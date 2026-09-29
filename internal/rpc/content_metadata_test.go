package rpc_test

import (
	"encoding/base64"
	"errors"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestContentMetadataUsesOwnerAndReferenceIdentity(t *testing.T) {
	_, c := fixture(t)
	owner := create(t, c).Root
	other := create(t, c).Root
	put := func(id protocol.ID, text string) protocol.ContentReference {
		return call[protocol.ContentReference](t, c, "content.put", protocol.PutContentParams{SessionID: id, ReferenceID: "file", MediaType: "text/plain", DataBase64: base64.StdEncoding.EncodeToString([]byte(text))})
	}
	first, second := put(owner.ID, "First"), put(other.ID, "Different bytes")
	for _, want := range []protocol.ContentReference{first, second} {
		got := call[protocol.ContentReference](t, c, "content.get", protocol.ReadContentParams{SessionID: want.SessionID, ReferenceID: want.ID})
		if !reflect.DeepEqual(got, want) {
			t.Fatal(got, want)
		}
	}
	for _, id := range []protocol.ID{protocol.ID(first.Digest), "absent"} {
		var result protocol.ContentReference
		var wire *client.Error
		err := c.Call(t.Context(), "content.get", protocol.ReadContentParams{SessionID: owner.ID, ReferenceID: id}, &result)
		if !errors.As(err, &wire) || wire.Kind != "NOT_FOUND" {
			t.Fatal(err)
		}
	}
}
