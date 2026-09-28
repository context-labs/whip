package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProviderContractsPreserveUnknownZeroAndExactCounters(t *testing.T) {
	value := ProviderModelsResult{Items: []ProviderModel{{ID: "model", Prices: ModelPrices{Input: new(Counter(9007199254740993)), Output: new(Counter(0))}, ContextWindowTokens: new(Counter(0)), ReasoningEfforts: []string{}, SupportsTools: new(false), MetadataSource: "advertised"}}}
	raw, err := json.Marshal(value)
	if err != nil || Validate("ProviderModelsResult", raw) != nil {
		t.Fatal("valid exact metadata rejected", err)
	}
	text := string(raw)
	for _, expected := range []string{`"input":"9007199254740993"`, `"output":"0"`, `"cached_input":null`, `"context_window_tokens":"0"`, `"reasoning_efforts":[]`, `"input_modalities":null`, `"supports_tools":false`} {
		if !strings.Contains(text, expected) {
			t.Fatal("presence or exact integer changed", expected)
		}
	}
	for _, bad := range []string{`9007199254740993`, `"01"`, `"-1"`, `"9223372036854775808"`} {
		if Validate("ProviderModelsResult", []byte(strings.Replace(text, `"9007199254740993"`, bad, 1))) == nil {
			t.Fatal("invalid exact counter accepted", bad)
		}
	}
	value.Items = append(value.Items, make([]ProviderModel, 1024)...)
	raw, _ = json.Marshal(value)
	if Validate("ProviderModelsResult", raw) == nil {
		t.Fatal("unbounded model list accepted")
	}
	for _, name := range []string{"ProviderModelsResult", "ProviderPresetsResult"} {
		if Validate(name, []byte(`{"items":null}`)) == nil {
			t.Fatal("collection absence accepted", name)
		}
	}
}

func TestProviderPublicStatusRejectsSecretFields(t *testing.T) {
	value := ProviderInventory{Revision: strings.Repeat("a", 64), Routes: []ProviderRoute{{ID: "custom", Kind: "openai-chat", BaseURL: "https://example.test/v1", Credential: ProviderCredentialStatus{Source: "command", State: "unchecked"}, Models: map[string]ProviderModelSettings{}}}}
	raw, _ := json.Marshal(value)
	if err := Validate("ProviderInventory", raw); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{`"key":"private",`, `"command":{"executable":"/bin/echo","arguments":["private"]},`} {
		bad := strings.Replace(string(raw), `"source":"command",`, `"source":"command",`+secret, 1)
		if Validate("ProviderInventory", []byte(bad)) == nil {
			t.Fatal("secret accepted in public credential projection")
		}
	}
}
