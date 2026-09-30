package daemon

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	providersvc "github.com/context-labs/whip/internal/provider"
)

func customProviderService(t *testing.T) *providersvc.ProviderService {
	t.Helper()
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	s := providersvc.NewProviderService(t.Context(), "provider-configuration")
	t.Cleanup(s.Close)
	return s
}

func customProviderParams(t *testing.T, s *providersvc.ProviderService) protocol.ProviderCreateParams {
	t.Helper()
	cfg, err := s.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	return protocol.ProviderCreateParams{
		Revision: cfg.Revision, Provider: "custom-test",
		Definition: protocol.ProviderDefinition{Name: "My endpoint", BaseURL: "https://example.test/v1", API: "openai-completions"},
		Credential: protocol.ProviderCredential{Mode: "api_key", Key: "private-fixture-key"},
	}
}

func TestProviderConfigurationHostRPCAndSecretContracts(t *testing.T) {
	s := customProviderService(t)
	server := &Server{providers: s}
	connection := &serverConn{ctx: t.Context()}
	p := customProviderParams(t, s)
	p.Definition.BaseURL = providerModelEndpoint(t, p.Credential.Key, []llm.ModelInfo{{ID: "fixture"}})
	body, _ := json.Marshal(p)
	result, rpcErr, handled := server.handleProvider(connection, rpcMessage{Method: "provider.create", Params: body})
	if rpcErr != nil || !handled {
		t.Fatalf("host-only create: %v, handled %v", rpcErr, handled)
	}
	created, ok := result.(protocol.ProviderConfiguration)
	if !ok || created.Provider != p.Provider {
		t.Fatalf("unexpected create result: %T", result)
	}
	for _, method := range []string{"provider.create", "provider.update"} {
		operation, ok := protocol.Lookup(method)
		if !ok || !operation.Sensitive || operation.Execution != protocol.Ephemeral {
			t.Fatalf("secret-bearing method can enter history: %+v", operation)
		}
	}
	body, _ = json.Marshal(protocol.ProviderNameParams{Provider: p.Provider})
	result, rpcErr, handled = server.handleProvider(connection, rpcMessage{Method: "provider.get", Params: body})
	if rpcErr != nil || !handled {
		t.Fatalf("host-only get: %v", rpcErr)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), p.Credential.Key) {
		t.Fatal("host get returned raw credentials")
	}
	_, rpcErr, _ = server.handleProvider(connection, rpcMessage{Method: "provider.update", Params: json.RawMessage(`{"provider":"custom-test","revision":"old-revision","credential":{"mode":"api_key","key":"private-key"}}`)})
	if rpcErr == nil || rpcErr.Code != -32009 {
		t.Fatalf("missing revision conflict contract: %+v", rpcErr)
	}
	_, rpcErr, _ = server.handleProvider(connection, rpcMessage{Method: "provider.update", Params: json.RawMessage(`{"provider":"custom-test","unexpected":"secret"}`)})
	if rpcErr == nil || strings.Contains(rpcErr.Message, "secret") {
		t.Fatalf("invalid request was not safely rejected: %+v", rpcErr)
	}
}
