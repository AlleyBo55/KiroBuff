package guard

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const cmd = "kirobuff budget .kiro/agents/a.json --max 2000 --quiet"

func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, raw)
	}
	return m
}

func TestInstallIntoConfigWithNoHooks(t *testing.T) {
	in := []byte(`{"name":"a","tools":["read"],"resources":["file://README.md"]}`)

	out, err := Install(in, cmd, 3600)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	m := decode(t, out)

	hooks, ok := m["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("expected object-format hooks, got %T", m["hooks"])
	}
	spawn, ok := hooks[Trigger].([]any)
	if !ok || len(spawn) != 1 {
		t.Fatalf("expected one agentSpawn hook, got %v", hooks[Trigger])
	}
	h := spawn[0].(map[string]any)
	if h["command"] != cmd {
		t.Errorf("command: got %v", h["command"])
	}
	if h["cache_ttl_seconds"] != float64(3600) {
		t.Errorf("cache_ttl_seconds: got %v, want 3600", h["cache_ttl_seconds"])
	}

	// Unrelated fields must survive untouched.
	if m["name"] != "a" {
		t.Errorf("name lost: %v", m["name"])
	}
	if got := m["tools"].([]any); len(got) != 1 || got[0] != "read" {
		t.Errorf("tools lost: %v", m["tools"])
	}
	if got := m["resources"].([]any); len(got) != 1 || got[0] != "file://README.md" {
		t.Errorf("resources lost: %v", m["resources"])
	}
}

func TestInstallPreservesExistingObjectHooks(t *testing.T) {
	in := []byte(`{
	  "name":"a",
	  "hooks":{
	    "agentSpawn":[{"command":"git status"}],
	    "stop":[{"command":"go test ./..."}]
	  }
	}`)

	out, err := Install(in, cmd, 60)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	hooks := decode(t, out)["hooks"].(map[string]any)

	spawn := hooks[Trigger].([]any)
	if len(spawn) != 2 {
		t.Fatalf("expected the original hook plus ours, got %d", len(spawn))
	}
	if spawn[0].(map[string]any)["command"] != "git status" {
		t.Errorf("existing agentSpawn hook was displaced: %v", spawn[0])
	}
	// Other triggers must be untouched.
	stop, ok := hooks["stop"].([]any)
	if !ok || len(stop) != 1 || stop[0].(map[string]any)["command"] != "go test ./..." {
		t.Errorf("stop hook lost: %v", hooks["stop"])
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	in := []byte(`{"name":"a"}`)

	once, err := Install(in, cmd, 3600)
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	if _, err := Install(once, cmd, 3600); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("second install should report ErrAlreadyInstalled, got %v", err)
	}
}

func TestInstallPreservesArrayFormat(t *testing.T) {
	in := []byte(`{
	  "name":"a",
	  "hooks":[
	    {"name":"existing","trigger":"stop","action":{"type":"command","command":"date"}}
	  ]
	}`)

	out, err := Install(in, cmd, 3600)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	hooks, ok := decode(t, out)["hooks"].([]any)
	if !ok {
		t.Fatalf("array format must stay an array, got %T", decode(t, out)["hooks"])
	}
	if len(hooks) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(hooks))
	}
	added := hooks[1].(map[string]any)
	if added["trigger"] != Trigger {
		t.Errorf("trigger: got %v", added["trigger"])
	}
	action := added["action"].(map[string]any)
	if action["command"] != cmd || action["type"] != "command" {
		t.Errorf("action wrong: %v", action)
	}
	// The array format has no cache field; it must not be invented.
	if _, present := added["cache_ttl_seconds"]; present {
		t.Error("cache_ttl_seconds must not be written into array-format hooks")
	}
}

func TestInstallRejectsMalformedHooksField(t *testing.T) {
	in := []byte(`{"name":"a","hooks":"not-a-structure"}`)
	if _, err := Install(in, cmd, 0); err == nil {
		t.Fatal("expected an error for an unrecognised hooks shape")
	}
}

func TestInstallRejectsInvalidJSON(t *testing.T) {
	if _, err := Install([]byte(`{not json`), cmd, 0); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestZeroTTLIsOmitted(t *testing.T) {
	out, err := Install([]byte(`{"name":"a"}`), cmd, 0)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if strings.Contains(string(out), "cache_ttl_seconds") {
		t.Errorf("a zero TTL is the documented default and should be omitted:\n%s", out)
	}
}

func TestSupportsCaching(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"no hooks", `{"name":"a"}`, true},
		{"object hooks", `{"hooks":{"stop":[{"command":"date"}]}}`, true},
		{"array hooks", `{"hooks":[{"trigger":"stop","action":{"type":"command","command":"date"}}]}`, false},
		{"invalid json", `{nope`, false},
	}
	for _, c := range cases {
		if got := SupportsCaching([]byte(c.raw)); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestOutputIsIndentedAndNewlineTerminated(t *testing.T) {
	out, err := Install([]byte(`{"name":"a"}`), cmd, 60)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !strings.Contains(string(out), "\n  ") {
		t.Error("output should be indented for a readable diff")
	}
	if out[len(out)-1] != '\n' {
		t.Error("output should end with a newline")
	}
}
