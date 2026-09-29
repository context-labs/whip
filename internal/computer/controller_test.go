package computer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/computerconfig"
)

func batchHelperBinary(t *testing.T) string {
	t.Helper()
	source := strings.Replace(ownedFakeHelper, `switch request.Method{`, `switch request.Method{
 case "apps":
  apps:=os.Getenv("WHIP_TEST_APPS");if apps==""{apps="[{\"name\":\"Test App\",\"bundleId\":\"com.test.App\",\"pid\":42,\"active\":true}]"}
  _=encoder.Encode(map[string]any{"jsonrpc":"2.0","id":request.ID,"result":json.RawMessage(apps)});continue
 case "state","ax","type","click","press":
  if request.Method=="click" {
   if path:=os.Getenv("WHIP_TEST_PARAMS");path!=""{data,_:=json.Marshal(request.Params);_=os.WriteFile(path,data,0600)}
   switch os.Getenv("WHIP_TEST_ACTION"){case "block":time.Sleep(time.Hour);case "crash":os.Exit(7);case "ack":_=encoder.Encode(map[string]any{"jsonrpc":"2.0","id":request.ID,"result":map[string]any{"action":"posted","stateUnavailable":"private failure"}});continue}
  }
  _=encoder.Encode(map[string]any{"jsonrpc":"2.0","id":request.ID,"result":map[string]any{"generation":7,"app":"Test App","elements":[]any{map[string]any{"index":0,"role":"AXButton","title":"test"}}}});continue
 `, 1)
	directory := t.TempDir()
	path := filepath.Join(directory, "main.go")
	binary := filepath.Join(directory, "helper")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.CommandContext(t.Context(), "go", "build", "-o", binary, path).CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	return binary
}

func newTestController(t *testing.T, binary string, environment map[string]string) *Controller {
	t.Helper()
	manager := capability.NewProcessManager()
	t.Cleanup(func() { _ = manager.Close() })
	c, err := NewController(ControllerOptions{Processes: manager, Owner: "computer-host", Directory: t.TempDir(), Environment: environment}, computerconfig.Config{Enabled: true, HelperExecutable: binary, DefaultDeny: true, Allow: []string{"test app"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func captureBatch(t *testing.T, c *Controller, code string) *Capture {
	t.Helper()
	b, err := CompileBatch(code)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := c.Capture(b)
	if err != nil {
		t.Fatal(err)
	}
	return capture
}

func executeBatch(t *testing.T, c *Controller, o *Observations, code string) (BatchResult, error) {
	t.Helper()
	capture := captureBatch(t, c, code)
	lease, err := capture.Acquire(t.Context())
	if err != nil {
		return BatchResult{}, err
	}
	defer lease.Close()
	return lease.Run(t.Context(), o, func(context.Context) error { return nil })
}

func TestComputerControllerPassiveStatusAndExactObservedAuthority(t *testing.T) {
	binary := batchHelperBinary(t)
	record := filepath.Join(t.TempDir(), "calls")
	params := filepath.Join(t.TempDir(), "params")
	c := newTestController(t, binary, map[string]string{"WHIP_TEST_RECORD": record, "WHIP_TEST_PARAMS": params})
	for range 3 {
		if c.Status().State != "available" {
			t.Fatal(c.Status())
		}
	}
	capture := captureBatch(t, c, `state("Test App")`)
	if capture.NeedsConsent() {
		t.Fatal("explicit availability not reflected")
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatal("passive capture started helper")
	}
	o := &Observations{}
	if _, err := executeBatch(t, c, o, `click("Test App",0)`); err == nil {
		t.Fatal("unobserved index executed")
	}
	raw, err := os.ReadFile(record)
	if err != nil || string(raw) != "apps\n" {
		t.Fatal("index validation forwarded action", string(raw), err)
	}
	result, err := executeBatch(t, c, o, `state("Test App"); click("Test App",0)`)
	if err != nil || strings.Count(result.Text, "generation") != 2 {
		t.Fatal(result, err)
	}
	payload, err := os.ReadFile(params)
	if err != nil {
		t.Fatal(err)
	}
	var args map[string]json.RawMessage
	if json.Unmarshal(payload, &args) != nil || string(args["gen"]) != "7" || string(args["app"]) != `"com.test.App"` {
		t.Fatal("did not pin exact identity and observed generation", string(payload))
	}
	if c.Status().State != "connected" {
		t.Fatal("lease release killed persistent helper", c.Status())
	}
	if _, err := executeBatch(t, c, &Observations{}, `click("Test App",0)`); err == nil {
		t.Fatal("another session inherited observation")
	}
	previous := capture.Resource()
	if err := c.Reconnect(); err != nil {
		t.Fatal(err)
	}
	if capture.Lifetime().Err() == nil || previous == captureBatch(t, c, `state("Test App")`).Resource() {
		t.Fatal("reconnect restored authority")
	}
	if _, err := executeBatch(t, c, o, `click("Test App",0)`); err == nil {
		t.Fatal("reconnect restored old index authority")
	}
}

func TestComputerExactAppsDenyAliasesAmbiguityAndBroadScript(t *testing.T) {
	binary := batchHelperBinary(t)
	for _, apps := range []string{`[{"name":"Test App Other","bundleId":"com.test.Other","pid":1}]`, `[{"name":"Test App","bundleId":"com.test.One","pid":1},{"name":"Test App","bundleId":"com.test.Two","pid":2}]`} {
		c := newTestController(t, binary, map[string]string{"WHIP_TEST_APPS": apps})
		if _, err := executeBatch(t, c, &Observations{}, `state("Test App")`); err == nil {
			t.Fatal("ambiguous/fuzzy app selection accepted")
		}
	}
	c := newTestController(t, binary, nil)
	settings := c.epoch.config
	settings.Deny = []string{"com.test.App"}
	if err := c.Update(settings); err != nil {
		t.Fatal(err)
	}
	if _, err := executeBatch(t, c, &Observations{}, `state("Test App")`); err == nil {
		t.Fatal("name bypassed bundle deny")
	}
	plan, _ := CompileBatch(`tell("Other", "activate")`)
	if _, err := c.Capture(plan); err == nil {
		t.Fatal("broad script claimed app confinement")
	}
	plan, _ = CompileBatch(`state("Other")`)
	capture, err := c.Capture(plan)
	if err != nil || !capture.NeedsConsent() {
		t.Fatal("unlisted app lost explicit consent path", err)
	}
}

func TestComputerWholeBatchQueueRechecksGenerationAndCancellation(t *testing.T) {
	c := newTestController(t, "", nil)
	capture := captureBatch(t, c, `print("ok")`)
	active, err := capture.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 4)
	for range 4 {
		go func() {
			lease, err := capture.Acquire(t.Context())
			if lease != nil {
				lease.Close()
			}
			results <- err
		}()
	}
	waitOwned(t, func() bool { return len(c.slots) == 5 })
	if _, err := capture.Acquire(t.Context()); !errors.Is(err, ErrBatchCapacity) {
		t.Fatal("queue not bounded", err)
	}
	changed := make(chan error, 1)
	go func() {
		settings := computerconfig.Config{Enabled: true, DefaultDeny: true}
		changed <- c.Update(settings)
	}()
	waitOwned(t, func() bool { return capture.Lifetime().Err() != nil })
	for range 4 {
		if err := <-results; err == nil {
			t.Fatal("stale waiter admitted")
		}
	}
	select {
	case <-changed:
		t.Fatal("policy update returned before active lease joined")
	default:
	}
	active.Close()
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	if _, err := capture.Acquire(t.Context()); !errors.Is(err, ErrGenerationRetired) {
		t.Fatal("stale capture accepted", err)
	}
	newer := captureBatch(t, c, `print("ok")`)
	active, err = newer.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := newer.Acquire(ctx); err == nil {
		t.Fatal("cancelled waiter accepted")
	}
	active.Close()
}

func TestComputerHelperFailureRetiresWithoutReplayAndExplicitReconnect(t *testing.T) {
	binary := batchHelperBinary(t)
	record := filepath.Join(t.TempDir(), "calls")
	c := newTestController(t, binary, map[string]string{"WHIP_TEST_RECORD": record, "WHIP_TEST_ACTION": "crash"})
	capture := captureBatch(t, c, `state("Test App"); click("Test App",0); type("Test App","never")`)
	lease, err := capture.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	result, err := lease.Run(t.Context(), &Observations{}, func(context.Context) error { return nil })
	if err == nil || !strings.Contains(result.Text, "generation") {
		t.Fatal("lost completed prefix", result, err)
	}
	if _, err := lease.Run(t.Context(), &Observations{}, func(context.Context) error { return nil }); err == nil {
		t.Fatal("replayed batch")
	}
	lease.Close()
	if c.Status().State != "retired" {
		t.Fatal(c.Status())
	}
	if err := c.Update(c.epoch.config); err != nil {
		t.Fatal(err)
	}
	plan, _ := CompileBatch(`state("Test App")`)
	if _, err := c.Capture(plan); !errors.Is(err, ErrGenerationRetired) {
		t.Fatal("equal policy reopened failed helper", err)
	}
	data, _ := os.ReadFile(record)
	if string(data) != "apps\nstate\napps\nclick\n" {
		t.Fatal("effect replay or suffix execution", string(data))
	}
}

func TestComputerCloseCancelsAndJoinsActiveBatch(t *testing.T) {
	binary := batchHelperBinary(t)
	record := filepath.Join(t.TempDir(), "calls")
	c := newTestController(t, binary, map[string]string{"WHIP_TEST_RECORD": record, "WHIP_TEST_ACTION": "block"})
	lease, err := captureBatch(t, c, `state("Test App"); click("Test App",0)`).Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := lease.Run(t.Context(), &Observations{}, func(context.Context) error { return nil })
		result <- err
	}()
	waitOwned(t, func() bool { data, _ := os.ReadFile(record); return strings.Contains(string(data), "click\n") })
	lease.Close()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled effect succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("lease close did not join batch")
	}
	if c.Status().State != "retired" {
		t.Fatal("cancelled transport reusable", c.Status())
	}
}

func TestComputerMutationAcknowledgementClearsIndexAuthority(t *testing.T) {
	binary := batchHelperBinary(t)
	c := newTestController(t, binary, map[string]string{"WHIP_TEST_ACTION": "ack"})
	o := &Observations{}
	result, err := executeBatch(t, c, o, `state("Test App"); click("Test App",0)`)
	if err != nil || !strings.Contains(result.Text, "action posted; state unavailable") || strings.Contains(result.Text, "private failure") {
		t.Fatal(result, err)
	}
	if _, err := executeBatch(t, c, o, `click("Test App",0)`); err == nil {
		t.Fatal("ack preserved stale index")
	}
}

func TestComputerBroadScriptsAreExplicitAndFailureNeverClaimsSuccess(t *testing.T) {
	c := newTestController(t, "", nil)
	calls := 0
	c.scriptCommand = func(_ context.Context, path string, args ...string) ([]byte, error) {
		calls++
		if path != "/usr/bin/osascript" || len(args) != 2 || args[0] != "-e" {
			t.Fatal("unexpected executable")
		}
		if calls == 1 {
			return []byte("known prefix"), nil
		}
		return []byte("private stderr"), errors.New("private failure")
	}
	capture := captureBatch(t, c, `tell("Test App","activate"); chrome_close(1,1); tell("Test App","never")`)
	if !capture.NeedsConsent() || calls != 0 {
		t.Fatal("broad execution did not require inert explicit consent")
	}
	lease, err := capture.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	result, err := lease.Run(t.Context(), &Observations{}, func(context.Context) error { return nil })
	if err == nil || calls != 2 || result.Text != "known prefix\n" || strings.Contains(err.Error(), "private") {
		t.Fatal("uncertain script replayed or misreported", result, err, calls)
	}
}

func TestComputerPolicyRecheckStopsLaterScriptAndClearsObservation(t *testing.T) {
	c := newTestController(t, "", nil)
	calls := 0
	c.scriptCommand = func(context.Context, string, ...string) ([]byte, error) { calls++; return []byte("done"), nil }
	lease, err := captureBatch(t, c, `tell("Test App","first"); tell("Test App","second")`).Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	observations := &Observations{}
	if err := observations.note("old", "app", 1, 2); err != nil {
		t.Fatal(err)
	}
	result, err := lease.Run(t.Context(), observations, func(context.Context) error {
		if calls > 0 {
			return errors.New("policy revoked")
		}
		return nil
	})
	if err == nil || calls != 1 || result.Text != "done\n" || observations.generation("old", "app", 1) != 0 {
		t.Fatal("changed policy admitted suffix", result, err)
	}
}
