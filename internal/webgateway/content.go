package webgateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/context-labs/whip/internal/protocol"
)

// Each transfer owns its initialized client. Closing it aborts incomplete uploads
// at the daemon; no upload is shared by HTTP requests or client-supplied IDs.
func (s *Server) contentHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v3/content/upload", s.upload)
	mux.HandleFunc("GET /api/v3/content/{reference_id}", s.download)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.acquire(s.transfers) {
			http.Error(w, "transfer limit reached", http.StatusServiceUnavailable)
			return
		}
		defer s.release(s.transfers)
		// HTTP socket deadlines do not cancel RPCs when the daemon remains live
		// but a transfer stalls. This deadline also closes its dedicated client.
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) contentClient(ctx context.Context) (Client, func(), error) {
	client, err := openClient(ctx, s.options.Open)
	if err != nil {
		return nil, nil, err
	}
	if err := s.checkBackend(client.InitializeResult()); err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	return client, func() { stop(); _ = client.Close() }, nil
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength < 0 || r.ContentLength > maxUploadSize {
		http.Error(w, "a bounded Content-Length is required", http.StatusRequestEntityTooLarge)
		return
	}
	begin := protocol.UploadBeginParams{
		UploadID: "http-" + rand.Text(), RootID: r.URL.Query().Get("root_id"), AgentID: r.URL.Query().Get("agent_id"),
		Size: r.ContentLength, ExpectedDigest: r.Header.Get("X-Content-Sha256"),
		MediaType: r.Header.Get("Content-Type"), Source: "http-upload",
	}
	if begin.RootID == "" || !validDigest(begin.ExpectedDigest) {
		http.Error(w, "invalid upload metadata", http.StatusBadRequest)
		return
	}
	client, closeClient, err := s.contentClient(r.Context())
	if err != nil {
		http.Error(w, "daemon is unavailable", http.StatusBadGateway)
		return
	}
	defer closeClient()
	if err := client.Call(r.Context(), "upload.begin", begin, nil); err != nil {
		http.Error(w, "invalid upload metadata", http.StatusBadRequest)
		return
	}
	body := http.MaxBytesReader(w, r.Body, begin.Size)
	defer func() { _ = body.Close() }()
	buffer := make([]byte, maxContentChunk)
	hash := sha256.New()
	var offset int64
	for {
		count, err := body.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
			if chunkErr := client.Call(r.Context(), "upload.chunk", protocol.UploadChunkParams{
				UploadID: begin.UploadID, Offset: offset, Data: buffer[:count],
			}, nil); chunkErr != nil {
				http.Error(w, "invalid upload body", http.StatusBadRequest)
				return
			}
			offset += int64(count)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Error(w, "interrupted upload", http.StatusBadRequest)
			return
		}
	}
	if offset != begin.Size || hex.EncodeToString(hash.Sum(nil)) != begin.ExpectedDigest {
		http.Error(w, "upload size or digest mismatch", http.StatusBadRequest)
		return
	}
	var handle protocol.ContentHandle
	if err := client.Call(r.Context(), "upload.finish", protocol.UploadFinishParams{UploadID: begin.UploadID}, &handle); err != nil {
		http.Error(w, "upload could not be committed", http.StatusBadRequest)
		return
	}
	if handle.Size != begin.Size || handle.Digest != begin.ExpectedDigest || handle.ReferenceID == "" {
		http.Error(w, "invalid upload response", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(handle)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	client, closeClient, err := s.contentClient(r.Context())
	if err != nil {
		http.Error(w, "daemon is unavailable", http.StatusBadGateway)
		return
	}
	defer closeClient()
	params := protocol.ContentReadParams{
		ReferenceID: r.PathValue("reference_id"), RootID: r.URL.Query().Get("root_id"),
		AgentID: r.URL.Query().Get("agent_id"), Limit: maxContentChunk,
	}
	var chunk protocol.ContentReadResult
	if err := client.Call(r.Context(), "content.read", params, &chunk); err != nil {
		status := http.StatusInternalServerError
		if failure, ok := errors.AsType[*protocol.RPCError](err); ok && failure.Code == -32003 {
			status = http.StatusForbidden
		}
		http.Error(w, "content is unavailable", status)
		return
	}
	handle := chunk.Content
	if handle.ReferenceID != params.ReferenceID || handle.Size < 0 || !validDigest(handle.Digest) {
		http.Error(w, "invalid content metadata", http.StatusBadGateway)
		return
	}
	hash := sha256.New()
	for {
		if chunk.Content != handle || len(chunk.Data) > params.Limit || int64(len(chunk.Data)) > handle.Size-params.Offset ||
			(len(chunk.Data) == 0 && params.Offset < handle.Size) {
			abortDownload(w, params.Offset)
			return
		}
		_, _ = hash.Write(chunk.Data)
		final := params.Offset+int64(len(chunk.Data)) == handle.Size
		// Hold the last chunk until its digest is validated, so corrupt transfers
		// cannot look complete even after earlier bounded chunks have been sent.
		if final && hex.EncodeToString(hash.Sum(nil)) != handle.Digest {
			abortDownload(w, params.Offset)
			return
		}
		if params.Offset == 0 {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", "attachment")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Length", strconv.FormatInt(handle.Size, 10))
		}
		count, err := w.Write(chunk.Data)
		if err != nil || count != len(chunk.Data) {
			return
		}
		params.Offset += int64(count)
		if final {
			return
		}
		chunk = protocol.ContentReadResult{}
		// Every chunk carries the original root/agent grant. Never cache a grant.
		if err := client.Call(r.Context(), "content.read", params, &chunk); err != nil {
			panic(http.ErrAbortHandler)
		}
	}
}

func abortDownload(w http.ResponseWriter, offset int64) {
	if offset > 0 {
		panic(http.ErrAbortHandler)
	}
	http.Error(w, "invalid content response", http.StatusBadGateway)
}

func validDigest(digest string) bool {
	if len(digest) != sha256.Size*2 || digest != strings.ToLower(digest) {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}
