package persona

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryHasTechCofounder(t *testing.T) {
	if _, err := Get("tech-cofounder"); err != nil {
		t.Fatalf("tech-cofounder should be built in: %v", err)
	}
	if _, err := Get("nope"); err == nil {
		t.Error("unknown persona should error")
	}
}

func TestRenderProducesValidAgentConfig(t *testing.T) {
	p, err := Get("tech-cofounder")
	if err != nil {
		t.Fatal(err)
	}
	body, err := p.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, body)
	}
	if cfg["name"] != "tech-cofounder" {
		t.Errorf("name: got %v", cfg["name"])
	}
}

func TestVisibilityFieldsArePresent(t *testing.T) {
	// welcomeMessage is the only user-visible mode indicator Kiro CLI offers,
	// and keyboardShortcut is what makes the mode a toggle rather than a
	// one-way switch. Both are load-bearing for the feature.
	p, _ := Get("tech-cofounder")
	body, _ := p.Render()

	var cfg map[string]any
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	welcome, ok := cfg["welcomeMessage"].(string)
	if !ok || welcome == "" {
		t.Fatal("welcomeMessage is required; without it the mode is invisible")
	}
	if !strings.Contains(strings.ToUpper(welcome), "TECH COFOUNDER MODE") {
		t.Errorf("welcomeMessage should name the mode, got %q", welcome)
	}
	if cfg["keyboardShortcut"] != "ctrl+shift+t" {
		t.Errorf("keyboardShortcut: got %v", cfg["keyboardShortcut"])
	}
}

func TestModelIsOmittedSoSessionModelIsInherited(t *testing.T) {
	// Switching persona must not silently switch models.
	p, _ := Get("tech-cofounder")
	body, _ := p.Render()

	var cfg map[string]any
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	if _, present := cfg["model"]; present {
		t.Error("model must be omitted so the persona inherits the session model")
	}
}

func TestPromptAddressesConcreteFailureModes(t *testing.T) {
	p, _ := Get("tech-cofounder")
	// A persona prompt that only says "be critical" produces hedging. These
	// are the specific behaviours the mode exists to produce.
	required := []string{
		"one-way door", // reversibility
		"tradeoff",     // naming cost rather than presenting choices as free
		"built at all", // the option to decline
		"guessing",     // calibration
		"not doing",    // explicit scope
	}
	lower := strings.ToLower(p.Prompt)
	for _, want := range required {
		if !strings.Contains(lower, want) {
			t.Errorf("prompt should cover %q", want)
		}
	}
}

func TestInstallWritesAndIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	p, _ := Get("tech-cofounder")

	path, err := p.Install(dir, false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if filepath.Base(path) != "tech-cofounder.json" {
		t.Errorf("path: got %s", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not written: %v", err)
	}

	if _, err := p.Install(dir, false); !errors.Is(err, ErrExists) {
		t.Errorf("second install should report ErrExists, got %v", err)
	}
	if _, err := p.Install(dir, true); err != nil {
		t.Errorf("force should overwrite: %v", err)
	}
}

func TestInstallDoesNotClobberUserEdits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	p, _ := Get("tech-cofounder")
	path, err := p.Install(dir, false)
	if err != nil {
		t.Fatal(err)
	}

	edited := `{"name":"tech-cofounder","prompt":"my own wording"}`
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Install(dir, false); !errors.Is(err, ErrExists) {
		t.Fatal("expected ErrExists")
	}
	got, _ := os.ReadFile(path)
	if string(got) != edited {
		t.Error("user edits were overwritten without -force")
	}
}

func TestDirResolvesScopes(t *testing.T) {
	t.Setenv("KIRO_HOME", "")
	got, err := Dir(Global, "/home/u", "/ws")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/home/u", ".kiro", "agents") {
		t.Errorf("global: got %s", got)
	}

	got, err = Dir(Workspace, "/home/u", "/ws")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/ws", ".kiro", "agents") {
		t.Errorf("workspace: got %s", got)
	}

	if _, err := Dir("bogus", "/home/u", "/ws"); err == nil {
		t.Error("unknown scope should error")
	}
}

func TestDirHonoursKiroHome(t *testing.T) {
	t.Setenv("KIRO_HOME", "/opt/team/kiro")
	got, err := Dir(Global, "/home/u", "/ws")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/opt/team/kiro", "agents") {
		t.Errorf("KIRO_HOME ignored: got %s", got)
	}
}

func TestPersonaShipsEnforcementHook(t *testing.T) {
	// Built-in agents are not files and cannot carry hooks, so a generated
	// agent is the only place enforcement can live. A persona without it is a
	// prompt asking nicely.
	p, _ := Get("tech-cofounder")
	body, err := p.Render()
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	hooks, ok := cfg["hooks"].(map[string]any)
	if !ok {
		t.Fatal("no hooks in the generated persona")
	}
	pre, ok := hooks["preToolUse"].([]any)
	if !ok || len(pre) != 1 {
		t.Fatalf("expected one preToolUse hook, got %v", hooks["preToolUse"])
	}
	if got := pre[0].(map[string]any)["command"]; got != "kirobuff enforce" {
		t.Errorf("command: got %v", got)
	}
}
