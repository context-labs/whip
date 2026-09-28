package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestSamplingCapturedAcrossRetryConfigurationRestartAndChildren(t *testing.T) {
	requests := make(chan map[string]any, 8)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- body
		if calls.Add(1) == 1 {
			select {
			case <-release:
			case <-request.Context().Done():
				return
			}
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":"busy"}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	t.Cleanup(server.Close)
	provider := model.OpenAI{Resolve: func(context.Context, session.ModelSelection) (model.Route, error) {
		return model.Route{URL: server.URL, MaxOutputTokens: 100, TimeoutMillis: 30000, MaxAttempts: 2}, nil
	}}
	directory := t.TempDir()
	r := openTest(t, directory, provider)
	t.Cleanup(unblock)
	owner := createTest(t, r)
	oldModel := session.ModelSelection{Provider: "fixture", Name: "fixture", Temperature: new(0.0), TopP: new(0.25)}
	owner, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Model: &oldModel})
	if err != nil {
		t.Fatal(err)
	}
	capturedRevision := owner.ConfigRevision
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	next := func() map[string]any {
		t.Helper()
		select {
		case body := <-requests:
			return body
		case <-time.After(10 * time.Second):
			t.Fatal("provider request did not arrive")
			return nil
		}
	}
	assertSampling := func(body map[string]any, selection session.ModelSelection) {
		t.Helper()
		for key, value := range map[string]*float64{"temperature": selection.Temperature, "top_p": selection.TopP} {
			actual, exists := body[key]
			if value == nil {
				if exists {
					t.Fatalf("%s inherited after whole-model replacement: %v", key, actual)
				}
			} else if !exists || actual != *value {
				t.Fatalf("%s=%v, want %v", key, actual, *value)
			}
		}
	}
	submitTest(t, r, owner.ID, "captured")
	first := next()
	assertSampling(first, oldModel)
	newModel := session.ModelSelection{Provider: "fixture", Name: "fixture", Temperature: new(0.75), TopP: new(1.0)}
	owner, err = r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Model: &newModel})
	if err != nil {
		t.Fatal(err)
	}
	unblock()
	if second := next(); !reflect.DeepEqual(first, second) {
		t.Fatal("configuration edit changed a prepared retry")
	}
	done := waitTest(t, r, "captured", terminal)
	if done.Turn.State != session.Succeeded || done.Turn.ConfigRevision != capturedRevision {
		t.Fatalf("active turn lost captured configuration: %+v", done.Turn)
	}
	check := func(key string, selection session.ModelSelection) {
		t.Helper()
		assertSampling(next(), selection)
		if done := waitTest(t, r, key, terminal); done.Turn.State != session.Succeeded {
			t.Fatalf("%s failed: %+v", key, done.Turn)
		}
	}
	submitTest(t, r, owner.ID, "updated")
	check("updated", newModel)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, directory, provider)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, owner.ID, "restarted")
	check("restarted", newModel)
	for _, key := range []string{"inherited", "replaced"} {
		request := store.ChildRequest{ParentID: owner.ID, Parts: []session.Part{{Type: "text", Text: key}}}
		want := newModel
		if key == "replaced" {
			want = session.ModelSelection{Provider: "fixture", Name: "another-model"}
			request.Overrides.Model = &want
		}
		if _, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: key}, request); err != nil {
			t.Fatal(err)
		}
		check(key, want)
	}
}
