package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/context-labs/whip/internal/protocol"
)

func (s *Server) contentHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v4/content/{reference_id}", s.upload)
	mux.HandleFunc("GET /api/v4/content/{reference_id}", s.download)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.acquire(s.transfers) {
			http.Error(w, "transfer limit reached", http.StatusServiceUnavailable)
			return
		}
		defer s.release(s.transfers)
		if r.URL.Query().Get("runtime_id") != string(s.options.RuntimeID) {
			http.Error(w, "runtime identity mismatch", http.StatusConflict)
			return
		}
		if len(r.URL.Query()["runtime_id"]) != 1 || len(r.URL.Query()["session_id"]) != 1 {
			http.Error(w, "ambiguous content scope", http.StatusBadRequest)
			return
		}
		if epochs := r.URL.Query()["expected_process_epoch"]; len(epochs) > 1 || len(epochs) == 1 && epochs[0] != string(s.options.ProcessEpoch) {
			http.Error(w, "runtime process generation mismatch", http.StatusConflict)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) contentCall(ctx context.Context, method string, params, result any) error {
	stream, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = stream.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = stream.SetDeadline(deadline)
	}
	return invoke(stream, method, params, result)
}

func contentParams(r *http.Request) (protocol.ReadContentParams, error) {
	params := protocol.ReadContentParams{SessionID: protocol.ID(r.URL.Query().Get("session_id")), ReferenceID: protocol.ID(r.PathValue("reference_id"))}
	raw, err := json.Marshal(params)
	if err != nil {
		return params, err
	}
	return params, protocol.Validate("ReadContentParams", raw)
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength < 0 || r.ContentLength > maxContentBytes {
		http.Error(w, "content requires a size of at most 4 MiB", http.StatusRequestEntityTooLarge)
		return
	}
	params, err := contentParams(r)
	if err != nil {
		http.Error(w, "invalid content scope", http.StatusBadRequest)
		return
	}
	expected := r.Header.Get("X-Content-Sha256")
	if len(r.Header.Values("X-Content-Sha256")) != 1 || !validDigest(expected) {
		http.Error(w, "invalid content digest", http.StatusBadRequest)
		return
	}
	body := http.MaxBytesReader(w, r.Body, r.ContentLength)
	defer func() { _ = body.Close() }()
	stop := context.AfterFunc(r.Context(), func() { _ = body.Close() })
	defer stop()
	data, err := io.ReadAll(body)
	if err != nil || int64(len(data)) != r.ContentLength || digest(data) != expected {
		http.Error(w, "content size or digest mismatch", http.StatusBadRequest)
		return
	}
	request := protocol.PutContentParams{SessionID: params.SessionID, ReferenceID: params.ReferenceID, MediaType: r.Header.Get("Content-Type"), DataBase64: base64.StdEncoding.EncodeToString(data)}
	raw, err := json.Marshal(request)
	if err != nil || protocol.Validate("PutContentParams", raw) != nil {
		http.Error(w, "invalid content metadata", http.StatusBadRequest)
		return
	}
	var reference protocol.ContentReference
	if err := s.contentCall(r.Context(), "content.put", request, &reference); err != nil {
		http.Error(w, "content was not acknowledged; inspect the same owner and reference before retrying", http.StatusBadGateway)
		return
	}
	if err := validateReference(reference, params, int64(len(data)), expected); err != nil || reference.MediaType != request.MediaType {
		http.Error(w, "invalid content response", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(reference)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	params, err := contentParams(r)
	if err != nil {
		http.Error(w, "invalid content scope", http.StatusBadRequest)
		return
	}
	var result protocol.ReadContentResult
	if err := s.contentCall(r.Context(), "content.read", params, &result); err != nil {
		http.Error(w, "scoped content is unavailable", http.StatusNotFound)
		return
	}
	if len(result.DataBase64) > base64.StdEncoding.EncodedLen(maxContentBytes) {
		http.Error(w, "invalid content size", http.StatusBadGateway)
		return
	}
	data, err := base64.StdEncoding.Strict().DecodeString(result.DataBase64)
	if err != nil || validateReference(result.Reference, params, int64(len(data)), digest(data)) != nil {
		http.Error(w, "invalid content response", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Content-Sha256", result.Reference.Digest)
	_, _ = w.Write(data)
}

func validateReference(reference protocol.ContentReference, params protocol.ReadContentParams, size int64, expected string) error {
	if reference.SessionID != params.SessionID || reference.ID != params.ReferenceID || int64(reference.Size) != size || size < 0 || size > maxContentBytes || reference.Digest != expected || !validDigest(expected) {
		return errors.New("content identity or body mismatch")
	}
	return nil
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func validDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}
