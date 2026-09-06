// whip-contract generates the browser contract from Go wire types and registry.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/protocol"
)

type operationManifest struct {
	protocol.Operation
	ParamsType string `json:"params_type"`
	ResultType string `json:"result_type"`
}

type manifest struct {
	Major         int                 `json:"major"`
	Minor         int                 `json:"minor"`
	Operations    []operationManifest `json:"operations"`
	Events        map[string]string   `json:"events"`
	EventPayloads map[string]string   `json:"event_payloads"`
}

func main() {
	out := flag.String("out", "packages/protocol/schema", "schema directory")
	check := flag.Bool("check", false, "fail if generated files differ without writing")
	flag.Parse()
	if err := generate(*out, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(dir string, check bool) error {
	types := map[string]reflect.Type{}
	add := func(t reflect.Type) string { name := t.Name(); types[name] = t; return name }
	contract := manifest{Major: protocol.Major, Minor: protocol.Minor, Operations: []operationManifest{}, Events: map[string]string{}}
	for _, operation := range protocol.Operations() {
		contract.Operations = append(contract.Operations, operationManifest{Operation: operation, ParamsType: add(operation.Params), ResultType: add(operation.Result)})
	}
	add(reflect.TypeFor[protocol.ContentEventPayload]())
	contract.EventPayloads = map[string]string{}
	for name, event := range protocol.EventPayloads() {
		contract.EventPayloads[name] = add(event)
	}
	for name, event := range protocol.Events() {
		contract.Events[name] = add(event)
	}
	files := map[string][]byte{}
	encode := func(name string, value any) error {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		files[name] = append(data, '\n')
		return nil
	}
	if err := encode("manifest.json", contract); err != nil {
		return err
	}

	for name, t := range types {
		if name == "" {
			return fmt.Errorf("registry contains unnamed type %v", t)
		}
		schema, err := protocol.SchemaFor(t)
		if err != nil {
			return err
		}
		schema.Schema = "http://json-schema.org/draft-07/schema#"
		schema.ID = "https://whip.dev/protocol/v2/" + name
		schema.Title = name
		if err := encode(name+".json", schema); err != nil {
			return err
		}
	}
	// Actual Go marshaling is used, so browser validators exercise the wire bytes.
	fixture := []struct {
		Type  string `json:"type"`
		Value any    `json:"value"`
	}{
		{Type: "InitializeParams", Value: protocol.InitializeParams{ProtocolMajor: protocol.Major, BuildID: "fixture", ClientID: "browser-fixture", ClientKind: "human"}},
		{Type: "SubscribeParams", Value: protocol.SubscribeParams{RootID: "root-fixture", SubscriptionID: "view-fixture", Cursor: 9007199254740993}},
		{Type: "ContentHandle", Value: protocol.ContentHandle{ReferenceID: "ref-fixture", Digest: strings.Repeat("0", 64), Size: 9007199254740993}},
	}
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	nonce := bytes.Repeat([]byte{3}, 32)
	payload := `{"root_id":"root", "reason":"<tag> & café", "permission_id":"permission", "allow":true,"command_id":"fixture"}`
	message := protocol.ApprovalMessage("permission.decide", 9007199254740993, nonce, []byte(payload))
	signing := struct {
		Method     string `json:"method"`
		Generation int64  `json:"generation,string"`
		Nonce      []byte `json:"nonce"`
		Payload    string `json:"payload"`
		PublicKey  []byte `json:"public_key"`
		Seed       []byte `json:"seed"`
		Digest     []byte `json:"digest"`
		Signature  []byte `json:"signature"`
	}{Method: "permission.decide", Generation: 9007199254740993, Nonce: nonce, Payload: payload,
		PublicKey: private.Public().(ed25519.PublicKey), Seed: bytes.Repeat([]byte{7}, ed25519.SeedSize), Digest: message, Signature: ed25519.Sign(private, message)}
	if err := encode("signing-fixture.json", signing); err != nil {
		return err
	}
	if err := encode("fixtures.json", fixture); err != nil {
		return err
	}
	if !check {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		if check {
			existing, err := os.ReadFile(path)
			if err != nil || string(existing) != string(files[name]) {
				return fmt.Errorf("generated contract drift: %s", path)
			}
		} else if err := os.WriteFile(path, files[name], 0o644); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() && files[entry.Name()] == nil {
			if check {
				return fmt.Errorf("stale generated contract: %s", entry.Name())
			}
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
