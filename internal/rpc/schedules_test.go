package rpc_test

import (
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestSchedulesRPCExactSlotsAndBounds(t *testing.T) {
	_, client := fixture(t)
	tree := create(t, client)
	params := protocol.CreateScheduleParams{SessionID: tree.Root.ID, ScheduleID: "future", Expression: "@at 2500-01-02T03:04:05.123456789-07:00", Parts: []protocol.Part{{Type: "text", Text: "future"}}}
	value := call[protocol.ScheduleAdmission](t, client, "schedules.create", params)
	if value.Schedule == nil || *value.Schedule.NextDue != "2500-01-02T10:04:05.123456789Z" {
		t.Fatal(value)
	}
	listed := call[protocol.SchedulesResult](t, client, "schedules.list", protocol.ListSchedulesParams{SessionID: tree.Root.ID, Upcoming: true, Limit: 1})
	if len(listed.Items) != 1 || listed.NextCursor == nil || listed.NextCursor.Due != *value.Schedule.NextDue {
		t.Fatal(listed)
	}
	next := call[protocol.SchedulesResult](t, client, "schedules.list", protocol.ListSchedulesParams{SessionID: tree.Root.ID, Upcoming: true, Cursor: listed.NextCursor, Limit: 1})
	if len(next.Items) != 0 {
		t.Fatal(next)
	}
	for _, request := range []protocol.ListSchedulesParams{
		{SessionID: tree.Root.ID, Limit: 101},
		{SessionID: tree.Root.ID, Upcoming: true, Limit: 1, Cursor: &protocol.ScheduleCursor{ID: "future", Due: "2500-01-02T10:04:05.1234567891Z"}},
		{SessionID: tree.Root.ID, Limit: 1, Cursor: listed.NextCursor},
	} {
		var result protocol.SchedulesResult
		if err := client.Call(t.Context(), "schedules.list", request, &result); err == nil {
			t.Fatalf("accepted invalid cursor/page %+v", request)
		}
	}
	full := call[protocol.ScheduleResult](t, client, "schedules.get", protocol.ScheduleParams{SessionID: tree.Root.ID, ScheduleID: "future"})
	if len(full.Parts) != 1 || full.Parts[0].Text != "future" {
		t.Fatal(full)
	}
}
