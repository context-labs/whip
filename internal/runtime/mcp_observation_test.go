package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/brandicon"
	"github.com/context-labs/whip/internal/model"
)

func TestMCPBrandLookupSurvivesObserverCancellationAndCloseJoins(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
		close(stopped)
	}))
	t.Cleanup(endpoint.Close)
	before := brandicon.Endpoint
	brandicon.Endpoint = endpoint.URL + "/%s.ico"
	t.Cleanup(func() { brandicon.Endpoint = before })
	r := openTest(t, t.TempDir(), model.Scripted{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := r.MCPBrandIcons(ctx, []string{"example.com"}); done <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("lookup never started")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("caller cancellation ended owned lookup")
	case <-time.After(20 * time.Millisecond):
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not join lookup")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("HTTP request remained alive")
	}
}
