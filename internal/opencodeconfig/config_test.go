package opencodeconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestMergePreservesExistingKeysAndComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.jsonc")
	original := `{
  // keep me
  "$schema": "https://opencode.ai/config.json"
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	provider := Provider{
		Package:  "aisdk:@ai-sdk/openai-compatible",
		Name:     "llama-server (local)",
		Settings: map[string]interface{}{"baseURL": "http://127.0.0.1:8080/v1"},
		Models:   map[string]interface{}{"my-model": map[string]interface{}{"name": "my-model (local)"}},
	}

	if err := Merge(path, "llama-cpp", provider); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}

	if got["$schema"] != "https://opencode.ai/config.json" {
		t.Errorf("expected $schema preserved, got %v", got["$schema"])
	}

	providers, ok := got["providers"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected providers map, got %T", got["providers"])
	}
	block, ok := providers["llama-cpp"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected llama-cpp provider block, got %T", providers["llama-cpp"])
	}
	if block["package"] != "aisdk:@ai-sdk/openai-compatible" {
		t.Errorf("expected package field set, got %v", block["package"])
	}
	if block["settings"].(map[string]interface{})["baseURL"] != "http://127.0.0.1:8080/v1" {
		t.Errorf("expected settings.baseURL set, got %v", block["settings"])
	}
}

func TestMergeCreatesMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "opencode.jsonc")

	provider := Provider{Package: "aisdk:@ai-sdk/openai-compatible"}
	if err := Merge(path, "llama-cpp", provider); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to be created: %v", err)
	}
}

func TestMergeIsIdempotentAndKeepsOtherProviders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.jsonc")
	original := `{"provider": {"anthropic": {"options": {"apiKey": "x"}}}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	provider := Provider{Package: "aisdk:@ai-sdk/openai-compatible"}
	if err := Merge(path, "llama-cpp", provider); err != nil {
		t.Fatalf("first merge failed: %v", err)
	}
	if err := Merge(path, "llama-cpp", provider); err != nil {
		t.Fatalf("second merge failed: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	providers := got["providers"].(map[string]interface{})
	if _, ok := providers["llama-cpp"]; !ok {
		t.Error("expected llama-cpp provider to be present")
	}
	legacy := got["provider"].(map[string]interface{})
	if _, ok := legacy["anthropic"]; !ok {
		t.Error("expected the legacy anthropic provider block to be preserved")
	}
}

func TestMergeLeavesALegacyProviderBlockAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.jsonc")
	// llama-cpp as an older rnes (or v1-era opencode) wrote it, plus an
	// unrelated v2 provider. The stale legacy entry stays where it is:
	// opencode resolves a provider/providers conflict in favor of the
	// native providers block, so the entry is inert, and rnes doesn't
	// rewrite config it doesn't manage.
	original := `{"provider": {"llama-cpp": {"npm": "@ai-sdk/openai-compatible", "options": {"baseURL": "http://old:1/v1"}}}, "providers": {"other": {"name": "x"}}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	provider := Provider{Package: "aisdk:@ai-sdk/openai-compatible"}
	if err := Merge(path, "llama-cpp", provider); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	legacy := got["provider"].(map[string]interface{})
	if _, ok := legacy["llama-cpp"]; !ok {
		t.Error("expected the stale legacy llama-cpp block to be left alone")
	}
	providers := got["providers"].(map[string]interface{})
	if _, ok := providers["other"]; !ok {
		t.Error("expected the unrelated v2 provider to be preserved")
	}
	if _, ok := providers["llama-cpp"]; !ok {
		t.Error("expected llama-cpp under providers")
	}
}

func TestMergeConcurrentWritesDontDropEachOther(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.jsonc")
	if err := os.WriteFile(path, []byte(`{"$schema": "https://opencode.ai/config.json"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("provider-%d", i)
			provider := Provider{Package: "aisdk:@ai-sdk/openai-compatible"}
			if err := Merge(path, key, provider); err != nil {
				t.Errorf("Merge %d failed: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	providers, ok := got["providers"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected providers map, got %T", got["providers"])
	}
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("provider-%d", i)
		if _, ok := providers[key]; !ok {
			t.Errorf("expected %s to survive concurrent merges, got keys %v", key, providers)
		}
	}
}

func TestStripJSONComments(t *testing.T) {
	in := `{
  // line comment
  "a": "value with // not a comment",
  /* block
     comment */
  "b": "value with /* not a comment */ still"
}`
	got := stripJSONComments([]byte(in))
	var m map[string]interface{}
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("stripped output not valid JSON: %v\n%s", err, got)
	}
	if m["a"] != "value with // not a comment" {
		t.Errorf("string content corrupted: %v", m["a"])
	}
	if m["b"] != "value with /* not a comment */ still" {
		t.Errorf("string content corrupted: %v", m["b"])
	}
}

func TestDropProviderAPIKeysStripsEveryProviderAndOnlyTheKeys(t *testing.T) {
	// The v2 shape (providers/<id>/settings) and the pre-migration shape
	// (provider/<id>/options) both carry apiKeys; every one must go, and
	// every other option must stay.
	in := []byte(`{"model":"m","providers":{"openrouter":{"settings":{"apiKey":"sk-or-REAL","x":1},"models":{"a":{}}},"mistral":{"settings":{"apiKey":"mis-REAL"}}},"provider":{"legacy":{"options":{"apiKey":"sk-old-REAL","y":2}}},"theme":"dark"}`)
	out, dropped, err := DropProviderAPIKeys(in)
	if err != nil {
		t.Fatalf("DropProviderAPIKeys: %v", err)
	}
	if got, want := strings.Join(dropped, ","), "legacy,mistral,openrouter"; got != want {
		t.Errorf("dropped = %q, want %q (sorted)", got, want)
	}
	for _, secret := range []string{"sk-or-REAL", "mis-REAL", "sk-old-REAL"} {
		if strings.Contains(string(out), secret) {
			t.Errorf("%s stayed in the config:\n%s", secret, out)
		}
	}
	for _, want := range []string{`"x": 1`, `"y": 2`, `"models"`, `"model"`, `"theme"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("out is missing %s:\n%s", want, out)
		}
	}
	// Idempotent: nothing left to drop, and the config comes back untouched.
	if _, dropped, err := DropProviderAPIKeys(out); err != nil || dropped != nil {
		t.Errorf("second pass: dropped = %v, err = %v", dropped, err)
	}
}

func TestSetModelSetsTheTopLevelDefault(t *testing.T) {
	in := []byte(`{"theme":"dark","providers":{}}`)
	out, err := SetModel(in, "llama-cpp/m1")
	if err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got["model"] != "llama-cpp/m1" {
		t.Errorf("model = %v, want llama-cpp/m1", got["model"])
	}
	if got["theme"] != "dark" {
		t.Errorf("theme not preserved: %v", got["theme"])
	}
	// An existing default is replaced, not kept.
	out, err = SetModel(out, "llama-cpp/m2")
	if err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got["model"] != "llama-cpp/m2" {
		t.Errorf("model = %v, want llama-cpp/m2", got["model"])
	}
}
