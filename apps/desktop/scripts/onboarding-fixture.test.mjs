import { test } from 'node:test';
import { execFile } from 'node:child_process';
import { copyFile, mkdtemp, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';
const exec = promisify(execFile);

test('disposable onboarding transport redirects only exact canonical provider paths', async () => {
  const directory = await mkdtemp('/tmp/whip-onboarding-transport-');
  try {
    const source = path.join(directory, 'transport.go'), tests = path.join(directory, 'transport_test.go');
    await copyFile(new URL('./fixtures/onboarding-transport.go.txt', import.meta.url), source);
    await writeFile(tests, `package main
import ("context"; "io"; "net/http"; "net/http/httptest"; "strings"; "sync/atomic"; "testing")
func TestOnlyCanonicalPaths(t *testing.T) {
 var calls atomic.Int64
 server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  calls.Add(1)
  if r.URL.Path != "/key" && r.URL.Path != "/models" && r.URL.Path != "/chat/completions" { t.Error(r.URL.Path) }
  if r.Header.Get("Authorization") != "Bearer synthetic" { t.Error("credential was lost") }
  _, _ = io.WriteString(w, "fixture")
 }))
 defer server.Close()
 t.Setenv("WHIP_ONBOARDING_PROVIDER", server.URL)
 for _, address := range []string{"http://openrouter.ai/api/v1/models", "https://elsewhere.test/api/v1/models", "https://openrouter.ai:443/api/v1/models", "https://user@openrouter.ai/api/v1/models", "https://openrouter.ai/api/v1/models?key=x", "https://openrouter.ai/api/v1/models#part", "https://openrouter.ai/api/v1/models/other", "https://openrouter.ai/api/v1/chat/completions-extra"} {
  t.Run(address, func(t *testing.T) { request, err := http.NewRequestWithContext(context.Background(), "GET", address, nil); if err != nil { t.Fatal(err) }; if _, err := http.DefaultTransport.RoundTrip(request); err == nil { t.Fatal("unexpected destination accepted") } })
 }
 if calls.Load() != 0 { t.Fatal("rejected request reached fixture") }
 for _, endpoint := range []string{"key", "models", "chat/completions"} {
  request, err := http.NewRequestWithContext(context.Background(), "GET", "https://openrouter.ai/api/v1/"+endpoint, nil); if err != nil { t.Fatal(err) }
  request.Header.Set("Authorization", "Bearer synthetic")
  response, err := http.DefaultTransport.RoundTrip(request); if err != nil { t.Fatal(err) }; response.Body.Close()
  if !strings.HasPrefix(request.URL.String(), "https://openrouter.ai/api/v1/") { t.Fatal("caller request mutated") }
 }
 if calls.Load() != 3 { t.Fatal(calls.Load()) }
 t.Setenv("WHIP_ONBOARDING_PROVIDER", "http://example.test:1234")
 request, _ := http.NewRequestWithContext(context.Background(), "GET", "https://openrouter.ai/api/v1/models", nil)
 if _, err := http.DefaultTransport.RoundTrip(request); err == nil { t.Fatal("nonlocal fixture destination accepted") }
}
`);
    await exec('go', ['test', '-race', source, tests], { timeout: 60000, maxBuffer: 1 << 20 });
  } finally { await rm(directory, { recursive: true, force: true }); }
});
