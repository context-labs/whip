package daemon

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/context-labs/whip/internal/session"
)

// newContentHTTPHandler uses the same content grants and upload validation as
// socket clients. Upload state is scoped to this HTTP request, never a client ID.
func newContentHTTPHandler(uploads *uploadManager) http.Handler {
	slots := make(chan struct{}, 16)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v3/content/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength < 0 || r.ContentLength > MaxUploadSize {
			http.Error(w, "a bounded Content-Length is required", http.StatusRequestEntityTooLarge)
			return
		}
		transferID := "http-" + rand.Text()
		begin := UploadBeginParams{
			UploadID: transferID, RootID: r.URL.Query().Get("root_id"), AgentID: r.URL.Query().Get("agent_id"), Size: r.ContentLength,
			ExpectedDigest: r.Header.Get("X-Content-Sha256"),
			MediaType:      r.Header.Get("Content-Type"), Source: "http-upload",
		}
		if err := uploads.begin(transferID, begin); err != nil {
			http.Error(w, "invalid upload metadata", http.StatusBadRequest)
			return
		}
		defer uploads.abortClient(transferID)
		body := http.MaxBytesReader(w, r.Body, MaxUploadSize)
		defer func() { _ = body.Close() }()
		buffer := make([]byte, MaxContentChunk)
		var offset int64
		for {
			count, err := body.Read(buffer)
			if count > 0 {
				if chunkErr := uploads.chunk(transferID, UploadChunkParams{
					UploadID: transferID, Offset: offset, Data: buffer[:count],
				}); chunkErr != nil {
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
		handle, err := uploads.finish(r.Context(), transferID, transferID)
		if err != nil {
			http.Error(w, "upload could not be committed", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(handle)
	})
	mux.HandleFunc("GET /api/v3/content/{reference_id}", func(w http.ResponseWriter, r *http.Request) {
		referenceID := r.PathValue("reference_id")
		rootID, agentID := r.URL.Query().Get("root_id"), r.URL.Query().Get("agent_id")
		data, meta, err := uploads.store.ReadContent(r.Context(), referenceID, rootID, agentID, 0, MaxContentChunk)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, session.ErrContentAccess) {
				status = http.StatusForbidden
			}
			http.Error(w, "content is unavailable", status)
			return
		}
		// Never serve arbitrary uploaded HTML as active content on the daemon origin.
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
		for offset := int64(0); offset < meta.Size; {
			if len(data) == 0 {
				return
			}
			count, err := w.Write(data) //nolint:gosec // G705: forced octet-stream attachment plus nosniff prevents uploaded bytes from executing as HTML
			if err != nil || count != len(data) {
				return
			}
			offset += int64(count)
			if offset >= meta.Size {
				return
			}
			// Recheck the grant for every bounded read, including after revocation.
			data, _, err = uploads.store.ReadContent(r.Context(), referenceID, rootID, agentID, offset, MaxContentChunk)
			if err != nil {
				return
			}
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "transfer limit reached", http.StatusServiceUnavailable)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
