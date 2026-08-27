package config

import "testing"

// samplingParams in a model entry parses into the struct, survives a
// round-trip, and stays nil (omitted) when absent — the wire depends on the
// nil-vs-set distinction to omit unset params from requests.
func TestSamplingParamsParse(t *testing.T) {
	var c Config
	err := parseJSONC([]byte(`{
		// a comment, because config is JSONC
		"providers": {"p": {"baseURL": "http://x"}},
		"models": {
			"with": {"providers": ["p"], "samplingParams": {"temperature": 0.2, "top_p": 0.9}},
			"without": {"providers": ["p"]}
		}
	}`), &c)
	if err != nil {
		t.Fatal(err)
	}
	sp := c.Models["with"].SamplingParams
	if sp == nil || sp.Temperature == nil || *sp.Temperature != 0.2 || sp.TopP == nil || *sp.TopP != 0.9 {
		t.Fatalf("samplingParams did not parse: %+v", sp)
	}
	if c.Models["without"].SamplingParams != nil {
		t.Fatalf("model without samplingParams should have nil, got %+v", c.Models["without"].SamplingParams)
	}
}
