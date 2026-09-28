package session

import (
	"errors"
	"strings"
	"testing"
)

func TestForkRequestRequiresSnapshotAndWholeBoundaryCoordinates(t *testing.T) {
	valid := ForkRequest{ID: "fork", SessionID: "source", ExpectedHistoryRevision: 1, ExpectedConfigRevision: 2, ObservedThrough: 8, KeepThrough: 4}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*ForkRequest)
	}{
		{"identity", func(r *ForkRequest) { r.ID = "bad id" }},
		{"source", func(r *ForkRequest) { r.SessionID = "" }},
		{"history revision", func(r *ForkRequest) { r.ExpectedHistoryRevision = 0 }},
		{"config revision", func(r *ForkRequest) { r.ExpectedConfigRevision = 0 }},
		{"negative prefix", func(r *ForkRequest) { r.KeepThrough = -1 }},
		{"outside snapshot", func(r *ForkRequest) { r.KeepThrough = 9 }},
		{"title", func(r *ForkRequest) { r.Title = new(strings.Repeat("x", 1025)) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.change(&request)
			if err := request.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		})
	}
	valid.KeepThrough = 0
	if err := valid.Validate(); err != nil {
		t.Fatal("empty prefix rejected", err)
	}
}
