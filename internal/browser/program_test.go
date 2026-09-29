package browser

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type programFake struct {
	Backend
	calls []string
	info  PageInfo
	fail  string
	image []byte
}

func (b *programFake) record(name string) error {
	b.calls = append(b.calls, name)
	if b.fail == name {
		return errors.New("fake observed failure")
	}
	return nil
}
func (b *programFake) Info(context.Context) (PageInfo, error) { return b.info, b.record("info") }
func (b *programFake) Navigate(_ context.Context, url string) error {
	b.info.URL = url
	return b.record("goto:" + url)
}

func (b *programFake) TypeText(_ context.Context, text string) error   { return b.record("type:" + text) }
func (b *programFake) ClickAt(context.Context, float64, float64) error { return b.record("click") }
func (b *programFake) Eval(context.Context, string) (string, error)    { return "result", b.record("js") }

func (b *programFake) Screenshot(context.Context, int) ([]byte, error) {
	return b.image, b.record("screenshot")
}
func (b *programFake) UseTab(_ context.Context, id string) error { return b.record("useTab:" + id) }
func TestProgramCompilesEntireBatchBeforeEffects(t *testing.T) {
	for _, code := range []string{`type("prefix"); unknown()`, `type("prefix"); click(1)`, `type("prefix"); waitFor("x",42)`, `type("prefix"); box(9007199254740993)`, `type("prefix"); box(1.5)`, `type("prefix"); upload("#file","/secret")`, `type("prefix"); goto("file:///secret")`, `type("prefix"); goto("http://169.254.169.254/latest/meta-data")`, `type("prefix"); press("not-a-key")`} {
		if _, e := CompileProgram(code); e == nil {
			t.Errorf("accepted invalid suffix: %s", code)
		}
	}
	p, e := CompileProgram(`print(print("retained literal")); type("café 👋"); click(1.5,2); print(js("document.title")); useTab("one"); screenshot()`)
	if e != nil {
		t.Fatal(e)
	}
	if e = p.ValidateTarget("other"); e == nil {
		t.Fatal("foreign tab survived static scope validation")
	}
	if e = p.ValidateTarget("one"); e != nil {
		t.Fatal(e)
	}
	if p.Screenshots() != 1 {
		t.Fatal("lost nested helper plan")
	}
	b := &programFake{image: []byte("image")}
	shots := 0
	output, e := p.Run(context.Background(), b, ProgramLimits{Images: 8, ImageBytes: 16 << 20}, func(context.Context, []byte) error { shots++; return nil })
	if e != nil {
		t.Fatal(e)
	}
	if shots != 1 || !strings.Contains(output, "retained literal\nresult\n") || strings.Join(b.calls, ",") != "type:café 👋,click,js,useTab:one,screenshot,info" {
		t.Fatalf("result %q, calls %v, shots%d", output, b.calls, shots)
	}
}

func TestProgramPreservesObservedScreenshotBeforeSuffixFailure(t *testing.T) {
	p, e := CompileProgram(`print("prefix"); screenshot(); type("fail"); screenshot()`)
	if e != nil {
		t.Fatal(e)
	}
	b := &programFake{image: []byte("image"), fail: "type:fail"}
	published := 0
	output, e := p.Run(context.Background(), b, ProgramLimits{Images: 8, ImageBytes: 16 << 20}, func(context.Context, []byte) error { published++; return nil })
	if e == nil || published != 1 || !strings.HasPrefix(output, "prefix\n") || strings.Join(b.calls, ",") != "screenshot,type:fail,info" {
		t.Fatalf("partial outcome %q %v, %v, images%d", output, e, b.calls, published)
	}
}

func TestProgramRemainingImageBudgetPreventsNativeSuffix(t *testing.T) {
	p, e := CompileProgram(`screenshot(); type("after first"); screenshot(); type("forbidden")`)
	if e != nil {
		t.Fatal(e)
	}
	b := &programFake{image: []byte("image")}
	published := 0
	_, e = p.Run(context.Background(), b, ProgramLimits{Images: 1, ImageBytes: 100}, func(context.Context, []byte) error { published++; return nil })
	if e == nil || published != 1 || strings.Join(b.calls, ",") != "screenshot,type:after first,info" {
		t.Fatalf("image budget %v, %v", e, b.calls)
	}
}

func TestProgramPostNavigationFloorUsesScopedNeutralization(t *testing.T) {
	p, e := CompileProgram(`js("page navigates")`)
	if e != nil {
		t.Fatal(e)
	}
	b := &programFake{info: PageInfo{URL: "http://169.254.169.254/"}}
	output, e := p.Run(context.Background(), b, ProgramLimits{}, nil)
	if e != nil || !strings.Contains(output, "neutralized") || strings.Join(b.calls, ",") != "js,info,goto:about:blank" {
		t.Fatalf("floor %q %v, %v", output, e, b.calls)
	}
	b = &programFake{info: PageInfo{URL: "http://169.254.169.254/"}, fail: "goto:about:blank"}
	output, e = p.Run(context.Background(), b, ProgramLimits{}, nil)
	if e == nil || strings.Contains(output, "neutralized") {
		t.Fatalf("unconfirmed cleanup claimed success: %q %v", output, e)
	}
}

func TestProgramOutputAndPublishingFailuresStopEffects(t *testing.T) {
	p, e := CompileProgram(`screenshot(); type("forbidden")`)
	if e != nil {
		t.Fatal(e)
	}
	b := &programFake{image: []byte("image")}
	if _, e = p.Run(context.Background(), b, ProgramLimits{Images: 1, ImageBytes: 100}, func(context.Context, []byte) error { return errors.New("local persistence failed") }); e == nil {
		t.Fatal("publication failure ignored")
	}
	if strings.Join(b.calls, ",") != "screenshot,info" {
		t.Fatal(b.calls)
	}
	_, e = CompileProgram(`print("` + strings.Repeat("x", MaxProgramOutput-20) + `"); print("this exceeds the remaining output limit"); type("forbidden")`)
	if e == nil {
		t.Fatal("source byte limit should reject oversized static program")
	}
}
