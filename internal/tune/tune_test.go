package tune

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectFamily(t *testing.T) {
	cases := map[string]Family{
		"claude-opus-4.7":          Claude,
		"claude-opus-4.7-messages": Claude,
		"claude-sonnet-4.5":        Claude,
		"openai-gpt-5.4":           GPT,
		"openai-gpt-5.5-1p":        GPT,
		"gemini-2.5-pro":           Unknown,
		"kimi-k2.5":                Unknown,
	}
	for id, want := range cases {
		if got := DetectFamily(id); got != want {
			t.Errorf("%s: got %s, want %s", id, got, want)
		}
	}
}

// The path is the whole game: a value at the wrong path is silently ignored at
// session bootstrap, which is indistinguishable from the setting not working.
func TestClaudeUsesOutputConfigPath(t *testing.T) {
	out, err := Apply(nil, "claude-opus-4.7", "medium")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(out, &s); err != nil {
		t.Fatal(err)
	}
	model := s[SettingsKey].(map[string]any)["claude-opus-4.7"].(map[string]any)
	oc, ok := model["output_config"].(map[string]any)
	if !ok {
		t.Fatalf("claude must use output_config, got %v", model)
	}
	if oc["effort"] != "medium" {
		t.Errorf("effort: got %v", oc["effort"])
	}
	if _, wrong := model["reasoning"]; wrong {
		t.Error("claude must not get a reasoning key")
	}
}

func TestGPTUsesReasoningPath(t *testing.T) {
	out, err := Apply(nil, "openai-gpt-5.4", "high")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(out, &s); err != nil {
		t.Fatal(err)
	}
	model := s[SettingsKey].(map[string]any)["openai-gpt-5.4"].(map[string]any)
	r, ok := model["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("gpt must use reasoning, got %v", model)
	}
	if r["effort"] != "high" {
		t.Errorf("effort: got %v", r["effort"])
	}
	if _, wrong := model["output_config"]; wrong {
		t.Error("gpt must not get an output_config key")
	}
}

func TestMaxIsClaudeOnly(t *testing.T) {
	if err := ValidateEffort(Claude, "max"); err != nil {
		t.Errorf("max is valid for claude: %v", err)
	}
	if err := ValidateEffort(GPT, "max"); err == nil {
		t.Error("max must be rejected for gpt")
	}
}

func TestUnknownFamilyIsRejectedNotGuessed(t *testing.T) {
	// Guessing a path for gemini or kimi would write a setting that is silently
	// dropped, which is worse than refusing.
	if _, err := Apply(nil, "gemini-2.5-pro", "low"); err == nil {
		t.Fatal("expected a refusal for a model with no known effort path")
	}
}

func TestInvalidEffortIsRejected(t *testing.T) {
	if _, err := Apply(nil, "claude-opus-4.7", "ultra"); err == nil {
		t.Fatal("expected rejection of an unknown effort level")
	}
}

func TestUnrelatedSettingsArePreserved(t *testing.T) {
	existing := []byte(`{
	  "chat.defaultModel": "claude-opus-4.7",
	  "chat.disableInheritingDefaultResources": true,
	  "hooks.showStatus": false,
	  "chat.modelDefaults": {
	    "openai-gpt-5.4": {"reasoning": {"effort": "high"}}
	  }
	}`)

	out, err := Apply(existing, "claude-opus-4.7", "medium")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(out, &s); err != nil {
		t.Fatal(err)
	}
	if s["chat.defaultModel"] != "claude-opus-4.7" {
		t.Error("defaultModel lost")
	}
	if s["chat.disableInheritingDefaultResources"] != true {
		t.Error("inheritance setting lost")
	}
	if s["hooks.showStatus"] != false {
		t.Error("hooks.showStatus lost")
	}
	// The pre-existing model default must survive alongside the new one.
	defaults := s[SettingsKey].(map[string]any)
	gpt := defaults["openai-gpt-5.4"].(map[string]any)["reasoning"].(map[string]any)
	if gpt["effort"] != "high" {
		t.Error("existing gpt default was clobbered")
	}
	if len(defaults) != 2 {
		t.Errorf("expected 2 model defaults, got %d", len(defaults))
	}
}

func TestSiblingKeysInsideModelBlockSurvive(t *testing.T) {
	existing := []byte(`{"chat.modelDefaults":{"claude-opus-4.7":{"output_config":{"something_else":1}}}}`)
	out, err := Apply(existing, "claude-opus-4.7", "low")
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	json.Unmarshal(out, &s)
	oc := s[SettingsKey].(map[string]any)["claude-opus-4.7"].(map[string]any)["output_config"].(map[string]any)
	if oc["something_else"] != float64(1) {
		t.Error("sibling key inside output_config was dropped")
	}
	if oc["effort"] != "low" {
		t.Error("effort not set")
	}
}

func TestApplyOnEmptyAndWhitespaceInput(t *testing.T) {
	for _, in := range [][]byte{nil, {}, []byte("  \n ")} {
		if _, err := Apply(in, "claude-opus-4.7", "low"); err != nil {
			t.Errorf("empty input should be treated as {}, got %v", err)
		}
	}
}

func TestApplyRejectsMalformedSettings(t *testing.T) {
	if _, err := Apply([]byte(`{not json`), "claude-opus-4.7", "low"); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestCurrentReadsBack(t *testing.T) {
	out, _ := Apply(nil, "claude-opus-4.7", "medium")
	got, ok := Current(out, "claude-opus-4.7")
	if !ok || got != "medium" {
		t.Errorf("Current: got %q ok=%v", got, ok)
	}
	if _, ok := Current(out, "claude-sonnet-4.5"); ok {
		t.Error("unconfigured model should report absent")
	}
}

func TestConfiguredModelsIsSorted(t *testing.T) {
	out, _ := Apply(nil, "openai-gpt-5.4", "high")
	out, _ = Apply(out, "claude-opus-4.7", "medium")
	got := ConfiguredModels(out)
	if len(got) != 2 || got[0] != "claude-opus-4.7" || got[1] != "openai-gpt-5.4" {
		t.Errorf("got %v", got)
	}
}

func TestWriteCreatesFileAndParents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings", "cli.json")
	if err := Write(path, "claude-opus-4.7", "medium"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := Current(raw, "claude-opus-4.7"); got != "medium" {
		t.Errorf("round trip failed, got %q", got)
	}
}

func TestSettingsPathHonoursKiroHome(t *testing.T) {
	t.Setenv("KIRO_HOME", "/opt/team/kiro")
	if got := SettingsPath("/home/u"); got != filepath.Join("/opt/team/kiro", "settings", "cli.json") {
		t.Errorf("got %s", got)
	}
	t.Setenv("KIRO_HOME", "")
	if got := SettingsPath("/home/u"); !strings.HasSuffix(got, filepath.Join(".kiro", "settings", "cli.json")) {
		t.Errorf("got %s", got)
	}
}

func TestRecommendedIsBelowOpusBuiltInDefault(t *testing.T) {
	// Kiro CLI's built-in default for the Opus family is xhigh. The whole point
	// of this package is to land below that.
	if Recommended == "xhigh" || Recommended == "max" {
		t.Errorf("Recommended=%q does not reduce reasoning", Recommended)
	}
	if err := ValidateEffort(Claude, Recommended); err != nil {
		t.Errorf("Recommended must be a valid claude effort: %v", err)
	}
}
