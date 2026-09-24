package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestActorFailureAnswersCurrentAndRemainingBatch(t *testing.T) {
	failure := errors.New("worker failed")
	for _, kind := range []string{"stream", "control error", "client error", "control panic", "client preflight", "client panic"} {
		t.Run(kind, func(t *testing.T) {
			root := &Session{supervisor: newSupervisor()}
			t.Cleanup(root.supervisor.stop)
			currentControl := make(chan error, 1)
			currentClient := make(chan clientCommandReply, 1)
			current := workerEnvelope{}
			switch kind {
			case "stream":
				current = workerEnvelope{kind: workerStream, stream: &streamEnvelope{}}
			case "control error":
				current = workerEnvelope{kind: workerControl, err: failure, reply: currentControl}
			case "client error":
				current = workerEnvelope{kind: workerClientCommand, err: failure, client: &clientCommandCompletion{reply: currentClient}}
			case "control panic":
				current = workerEnvelope{kind: workerControl, reply: currentControl, control: func(context.Context) error { panic("control panic") }}
			case "client preflight":
				current = workerEnvelope{kind: workerClientCommand, client: &clientCommandCompletion{reply: currentClient}}
			case "client panic":
				root.clientBusy = true
				// A panic while applying completion state must still answer it.
				current = workerEnvelope{kind: workerClientCommand, client: &clientCommandCompletion{reply: currentClient, goal: &clientGoal{text: "goal"}}}
			}
			remainingControl := make(chan error, 1)
			remainingClient := make(chan clientCommandReply, 1)
			controlRan := false
			err := root.processWorkerBatch([]workerEnvelope{
				current,
				{kind: workerControl, reply: remainingControl, control: func(context.Context) error { controlRan = true; return nil }},
				{kind: workerClientCommand, client: &clientCommandCompletion{reply: remainingClient}},
			})
			if err == nil {
				t.Fatal("batch unexpectedly succeeded")
			}
			if strings.Contains(kind, "panic") && !strings.Contains(err.Error(), "panic") {
				t.Fatalf("panic lost from actor error: %v", err)
			}
			if current.kind == workerControl {
				if got := receiveActorValue(t, currentControl); got == nil {
					t.Fatal("failing control received success")
				}
			}
			if current.kind == workerClientCommand {
				if got := receiveActorValue(t, currentClient); got.err == nil {
					t.Fatal("failing client completion received success")
				}
			}
			if got := receiveActorValue(t, remainingControl); !errors.Is(got, ErrStopped) {
				t.Fatalf("remaining control = %v, want ErrStopped", got)
			}
			if got := receiveActorValue(t, remainingClient); !errors.Is(got.err, ErrStopped) {
				t.Fatalf("remaining client = %v, want ErrStopped", got.err)
			}
			if controlRan {
				t.Fatal("control behind the failing event ran")
			}
			if err := root.flushPendingEvents(); err != nil {
				t.Fatal(err)
			}
			if len(currentControl)+len(currentClient)+len(remainingControl)+len(remainingClient) != 0 {
				t.Fatal("one or more envelopes received a second reply")
			}
		})
	}
}

func TestActorClientCommitFailureRepliesExactlyOnce(t *testing.T) {
	root := newIdleActorSession(t, &fakeRunner{})
	if err := root.store.Close(); err != nil {
		t.Fatal(err)
	}
	root.clientBusy = true
	reply := make(chan clientCommandReply, 1)
	if err := root.processWorkerBatch([]workerEnvelope{{
		kind:   workerClientCommand,
		client: &clientCommandCompletion{clientID: "client", commandID: "command", operation: "shell.run", reply: reply},
	}}); err == nil {
		t.Fatal("closed store accepted command completion")
	}
	if got := receiveActorValue(t, reply); got.err == nil {
		t.Fatal("commit failure was not returned")
	}
	if err := root.flushPendingEvents(); err != nil {
		t.Fatal(err)
	}
	if len(reply) != 0 {
		t.Fatal("commit failure was answered twice")
	}
}

func TestActorControlErrorDoesNotFailBatch(t *testing.T) {
	root := &Session{supervisor: newSupervisor()}
	t.Cleanup(root.supervisor.stop)
	first, second := make(chan error, 1), make(chan error, 1)
	failure := errors.New("invalid control")
	if err := root.processWorkerBatch([]workerEnvelope{
		{kind: workerControl, reply: first, control: func(context.Context) error { return failure }},
		{kind: workerControl, reply: second, control: func(context.Context) error { return nil }},
	}); err != nil {
		t.Fatal(err)
	}
	if got := receiveActorValue(t, first); !errors.Is(got, failure) {
		t.Fatalf("control error = %v", got)
	}
	if got := receiveActorValue(t, second); got != nil {
		t.Fatalf("next control = %v", got)
	}
}
