package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/session"
)

type BrowserDriverSelection struct {
	Revision, ConfiguredDriver, Driver string
	Pinned                             bool
}

func (r *Runtime) browserDriver(value config.Snapshot) BrowserDriverSelection {
	driver, _ := config.ResolveBrowserDriver(value.Host.BrowserDriver)
	result := BrowserDriverSelection{Revision: value.Revision, ConfiguredDriver: driver, Driver: driver, Pinned: r.browserDriverPin != ""}
	if result.Pinned {
		result.Driver = r.browserDriverPin
	}
	return result
}

func (r *Runtime) BrowserDriver(ctx context.Context) (BrowserDriverSelection, error) {
	value, err := r.configuration.Snapshot(ctx)
	return r.browserDriver(value), err
}

// SetBrowserDriver changes only future prepared batches. A process environment
// override is captured once at Open and cannot be defeated by a file edit.
func (r *Runtime) SetBrowserDriver(ctx context.Context, expected, driver string) (BrowserDriverSelection, error) {
	if driver != "rod" && driver != "chromedp" {
		return BrowserDriverSelection{}, session.ErrInvalid
	}
	value, err := r.configuration.Update(ctx, expected, func(host *config.Host) error {
		if r.browserDriverPin != "" && driver != r.browserDriverPin {
			return session.ErrInvalid
		}
		current, _ := config.ResolveBrowserDriver(host.BrowserDriver)
		if current != driver {
			host.BrowserDriver = driver
		}
		return nil
	})
	return r.browserDriver(value), err
}
