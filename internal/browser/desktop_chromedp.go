package browser

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/chromedp/cdproto/page"
	"github.com/go-rod/rod"
)

// NewDesktopChromeDPBackend runs ChromeDP actions on the same authorized CDP
// batch as Rod. No allocator, endpoint discovery, socket, or browser is owned.
func NewDesktopChromeDPBackend(ctx context.Context, client rod.CDPClient, targetID, attachmentID string, closeTransport func() error) (Backend, error) {
	if client == nil || targetID == "" || attachmentID == "" || closeTransport == nil {
		return nil, errors.New("desktop browser transport is incomplete")
	}
	lifetime, cancel := context.WithCancel(ctx)
	b := &desktopChromeDP{ctx: lifetime, cancel: cancel, targetID: targetID, closeTransport: closeTransport}
	b.chromedpBackend = &chromedpBackend{mode: Mode("desktop"), executor: &desktopExecutor{ctx: lifetime, client: client, sessionID: attachmentID}}
	b.events.Go(func() {
		defer cancel()
		for {
			select {
			case <-lifetime.Done():
				return
			case event, ok := <-client.Event():
				if !ok {
					return
				}
				if event == nil || event.SessionID != attachmentID {
					continue
				}
				switch event.Method {
				case "Page.javascriptDialogOpening":
					var d struct{ Type, Message, DefaultPrompt string }
					if json.Unmarshal(event.Params, &d) != nil {
						return
					}
					b.dialogMu.Lock()
					b.dialog = &Dialog{Type: boundedDialog(d.Type), Message: boundedDialog(d.Message), DefaultPrompt: boundedDialog(d.DefaultPrompt)}
					b.dialogMu.Unlock()
				case "Page.javascriptDialogClosed":
					b.dialogMu.Lock()
					b.dialog = nil
					b.dialogMu.Unlock()
				}
			}
		}
	})
	if err := b.run(ctx, page.Enable()); err != nil {
		_ = b.Close()
		return nil, err
	}
	return b, nil
}

func boundedDialog(v string) string {
	if len(v) > 4096 {
		v = v[:4096]
		for !utf8.ValidString(v) {
			v = v[:len(v)-1]
		}
	}
	return strings.Clone(v)
}

type desktopExecutor struct {
	ctx       context.Context
	client    rod.CDPClient
	sessionID string
}

func (e *desktopExecutor) Execute(ctx context.Context, method string, params, result any) error {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(e.ctx, cancel)
	defer func() { stop(); cancel() }()
	if err := e.ctx.Err(); err != nil {
		return err
	}
	raw, err := e.client.Call(ctx, e.sessionID, method, params)
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(raw, result)
}

type desktopChromeDP struct {
	*chromedpBackend
	ctx            context.Context
	cancel         context.CancelFunc
	targetID       string
	closeTransport func() error
	once           sync.Once
	closeErr       error
	events         sync.WaitGroup
	dialogMu       sync.Mutex
	dialog         *Dialog
	media          desktopMedia
}

func (b *desktopChromeDP) Close() error {
	b.once.Do(func() { b.cancel(); b.closeErr = b.closeTransport(); b.events.Wait() })
	return b.closeErr
}

func (b *desktopChromeDP) Info(ctx context.Context) (PageInfo, error) {
	b.dialogMu.Lock()
	var dialog *Dialog
	if b.dialog != nil {
		value := *b.dialog
		dialog = &value
	}
	b.dialogMu.Unlock()
	if dialog != nil {
		return PageInfo{Dialog: dialog}, nil
	}
	return b.chromedpBackend.Info(ctx)
}

func (b *desktopChromeDP) Navigate(ctx context.Context, url string) error {
	err := b.navigate(ctx, url)
	if err != nil {
		return err
	}
	return b.WaitLoad(ctx)
}

func (b *desktopChromeDP) WaitLoad(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		value, err := b.Eval(ctx, "document.readyState")
		if err != nil {
			return err
		}
		if value == `"complete"` {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.ctx.Done():
			return b.ctx.Err()
		case <-ticker.C:
		}
	}
}

func (b *desktopChromeDP) UseTab(ctx context.Context, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if target != b.targetID {
		return &DesktopError{Kind: "permission_denied", Message: "only the attached desktop tab is available"}
	}
	return b.ctx.Err()
}

func (*desktopChromeDP) UploadFiles(context.Context, string, []string) error {
	return &DesktopError{Kind: "unsupported_operation", Message: "agent file upload requires authorized byte transfer; filesystem paths are not accepted"}
}

func (b *desktopChromeDP) Screenshot(ctx context.Context, maxDim int) ([]byte, error) {
	return b.media.capture(ctx, maxDim, b.chromedpBackend.Screenshot)
}

func (b *desktopChromeDP) HandleDialog(accept bool, text string) error {
	ctx, cancel := context.WithTimeout(b.ctx, 2*time.Second)
	defer cancel()
	return b.run(ctx, page.HandleJavaScriptDialog(accept).WithPromptText(text))
}
