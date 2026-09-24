package session

import (
	"cmp"
	"context"
	"database/sql"
	"slices"
	"time"
	"unicode/utf8"

	schedulepkg "github.com/context-labs/whip/internal/schedule"
)

const upcomingPromptBytes = 4096

func readSnapshotUpcomingSchedules(ctx context.Context, tx *sql.Tx, rootID string, snapshot *RootSnapshot) error {
	limit := 128
	if snapshot.view != nil {
		limit = snapshot.view.CollectionLimit
	}
	upcoming := make([]UpcomingSchedule, 0, limit+1)
	count := 0
	// Scan metadata independently of the historical ID page, retaining only the
	// earliest pending slots. Fetch bounded prompts after selection, not per row.
	rows, err := tx.QueryContext(ctx, `SELECT id,schedule,anchor,last_fire FROM schedules WHERE session_id=?`, rootID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int
		var expression, anchorText, lastFireText string
		if err := rows.Scan(&id, &expression, &anchorText, &lastFireText); err != nil {
			return err
		}
		parsed, err := schedulepkg.Parse(expression)
		if err != nil {
			continue
		}
		anchor, err := time.Parse(time.RFC3339, anchorText)
		if err != nil {
			continue
		}
		var lastFire time.Time
		if lastFireText != "" {
			lastFire, err = time.Parse(time.RFC3339, lastFireText)
			if err != nil {
				continue
			}
		}
		slot, ok := parsed.NextSlot(anchor, lastFire)
		if !ok {
			continue
		}
		count++
		item := UpcomingSchedule{ID: id, NextFire: formatStamp(slot)}
		index, _ := slices.BinarySearchFunc(upcoming, item, compareUpcomingSchedules)
		if index >= limit {
			continue
		}
		upcoming = slices.Insert(upcoming, index, item)
		if len(upcoming) > limit {
			upcoming = upcoming[:limit]
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for i := range upcoming {
		var size int
		var prompt []byte
		err := tx.QueryRowContext(ctx,
			`SELECT length(CAST(prompt AS BLOB)),substr(CAST(prompt AS BLOB),1,?) FROM schedules WHERE session_id=? AND id=?`,
			upcomingPromptBytes, rootID, upcoming[i].ID,
		).Scan(&size, &prompt)
		if err != nil {
			return err
		}
		// A byte-limited SQL read may cut the final UTF-8 rune.
		for len(prompt) > 0 && !utf8.Valid(prompt) {
			prompt = prompt[:len(prompt)-1]
		}
		upcoming[i].Prompt = string(prompt)
		upcoming[i].PromptTruncated = size > len(prompt)
	}
	snapshot.UpcomingSchedules = &upcoming
	snapshot.UpcomingScheduleCount = &count
	if count > len(upcoming) {
		snapshot.Omitted["upcoming_schedules"] = true
	}
	return nil
}

// Fixed-precision UTC slot strings sort chronologically without reparsing.
func compareUpcomingSchedules(a, b UpcomingSchedule) int {
	if order := cmp.Compare(a.NextFire, b.NextFire); order != 0 {
		return order
	}
	return cmp.Compare(a.ID, b.ID)
}

// trimUpcomingSchedule first sheds prompt text, then the latest occurrence.
// Keeping a chronological prefix preserves the earliest timestamp under pressure.
func trimUpcomingSchedule(snapshot *RootSnapshot) bool {
	if snapshot.UpcomingSchedules == nil {
		return false
	}
	upcoming := *snapshot.UpcomingSchedules
	for i := len(upcoming) - 1; i >= 0; i-- {
		if upcoming[i].Prompt != "" {
			upcoming[i].Prompt = ""
			upcoming[i].PromptTruncated = true
			return true
		}
	}
	if len(upcoming) == 0 {
		return false
	}
	*snapshot.UpcomingSchedules = upcoming[:len(upcoming)-1]
	snapshot.Omitted["upcoming_schedules"] = true
	return true
}
