package shell

import (
	"bytes"
	"errors"
	"testing"
)

func TestInteractionBoundedOutputInputReceiptsAndGeneration(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	if value, err := m.Interaction("missing", 0); err != nil || value != nil || len(m.Owners()) != 0 {
		t.Fatal("observation created resources", value, err)
	}
	s := capture(t, m, "owner")
	r, err := s.Reserve(false)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	i, err := r.Interact("operation")
	if err != nil {
		t.Fatal(err)
	}
	defer i.Close()
	i.Output(string(bytes.Repeat([]byte("x"), InteractiveOutputBytes+100)))
	view, err := m.Interaction("owner", 0)
	if err != nil || view.From != 100 || view.Through != InteractiveOutputBytes+100 || len(view.Output) != InteractiveOutputBytes || view.NextInput != 1 {
		t.Fatal(view, err)
	}
	view.Output[0] = 'y'
	view, err = m.Interaction("owner", view.Through-2)
	if err != nil || string(view.Output) != "xx" {
		t.Fatal("read mutated output", view, err)
	}
	if _, err := m.Interaction("owner", view.Through+1); !errors.Is(err, ErrInputConflict) {
		t.Fatal(err)
	}
	data := []byte("secret")
	if err := m.Input("owner", "operation", 1, data); err != nil {
		t.Fatal(err)
	}
	data[0] = 'z'
	if string(<-i.Keys()) != "secret" {
		t.Fatal("input buffer ownership escaped")
	}
	if err := m.Input("owner", "operation", 1, []byte("secret")); err != nil {
		t.Fatal(err)
	}
	if len(i.keys) != 0 {
		t.Fatal("exact acknowledgement retry typed twice")
	}
	if err := m.Input("owner", "operation", 1, []byte("changed")); !errors.Is(err, ErrInputConflict) {
		t.Fatal(err)
	}
	if err := m.Input("foreign", "operation", 1, []byte("x")); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := m.Input("owner", "other-operation", 1, []byte("x")); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	for sequence := int64(2); sequence < 2+inputQueue; sequence++ {
		if err := m.Input("owner", "operation", sequence, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Input("owner", "operation", 2+inputQueue, []byte("x")); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	view, err = m.Interaction("owner", 0)
	if err != nil || view.NextInput != 2+inputQueue {
		t.Fatal("overflow consumed input sequence", view, err)
	}
	i.Close()
	if len(i.keys) != 0 {
		t.Fatal("closed command retained pending keystrokes")
	}
	if err := m.Input("owner", "operation", 2+inputQueue, []byte("x")); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if view, err := m.Interaction("owner", 0); err != nil || view != nil {
		t.Fatal("closed command remains visible", view, err)
	}
}
