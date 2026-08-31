package daemon

import (
	"errors"
	"time"

	"github.com/context-labs/whip/internal/schedule"
	"github.com/context-labs/whip/internal/session"
)

const schedulePollInterval = 5 * time.Second

func (s *Session) startScheduler() {
	s.supervisor.launchWorker("schedule ticker", func() {
		ticker := time.NewTicker(schedulePollInterval)
		defer ticker.Stop()
		for {
			s.supervisor.post(workerEnvelope{kind: workerScheduleTick, at: time.Now()})
			select {
			case <-s.supervisor.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
}

func (s *Session) fireDueSchedules(at time.Time) error {
	tasks, err := s.store.SchedulesContext(s.supervisor.ctx, s.meta.ID)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		parsed, err := schedule.Parse(task.Schedule)
		if err != nil {
			continue
		}
		slot, due := nextScheduleSlot(parsed, task, at)
		if !due {
			continue
		}
		_, err = s.store.ClaimScheduleFire(s.supervisor.ctx, session.ScheduleFireClaim{
			RootID: s.meta.ID, AgentID: s.authority.AgentID, ScheduleID: task.ID,
			ExpectedLastFire: task.LastFire, Slot: slot,
		})
		if err != nil && !errors.Is(err, session.ErrScheduleClaimed) {
			return err
		}
	}
	return nil
}

func nextScheduleSlot(parsed schedule.Schedule, task session.Schedule, at time.Time) (time.Time, bool) {
	if task.LastFire.IsZero() {
		slot := parsed.At
		if parsed.Every > 0 {
			slot, _ = parsed.NextAfter(task.Anchor, task.Anchor.Add(-time.Nanosecond))
		}
		return slot, !slot.Truncate(time.Second).After(at)
	}
	slot, ok := parsed.NextAfter(task.Anchor, task.LastFire)
	return slot, ok && !slot.Truncate(time.Second).After(at)
}
