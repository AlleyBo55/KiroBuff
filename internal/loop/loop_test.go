package loop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectToolchain(t *testing.T) {
	cases := []struct {
		marker string
		want   string
	}{
		{"go.mod", "go"},
		{"Cargo.toml", "cargo"},
		{"pyproject.toml", "python"},
		{"package.json", "node"},
		{"Makefile", "make"},
	}
	for _, c := range cases {
		ws := t.TempDir()
		if err := os.WriteFile(filepath.Join(ws, c.marker), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := DetectToolchain(ws).Name; got != c.want {
			t.Errorf("%s: got %q, want %q", c.marker, got, c.want)
		}
	}
}

func TestUnknownToolchainFailsClosed(t *testing.T) {
	tc := DetectToolchain(t.TempDir())
	if tc.Name != "unknown" {
		t.Fatalf("got %q", tc.Name)
	}
	// A verifier that always passes is worse than no loop at all.
	if !strings.Contains(tc.Verify, "exit 1") {
		t.Errorf("unknown toolchain must fail closed, got %q", tc.Verify)
	}
}

func TestNewFillsDefaults(t *testing.T) {
	s := New("", "", 0, Toolchain{Name: "go", Verify: "go test ./..."})
	if s.Editable != "src/**" {
		t.Errorf("editable default: got %q", s.Editable)
	}
	if s.MaxAttempts != 10 {
		t.Errorf("maxAttempts default: got %d", s.MaxAttempts)
	}
	if s.Goal == "" {
		t.Error("goal should get a placeholder, not stay empty")
	}
}

func TestGeneratedAgentIsValidJSONAndWiredCorrectly(t *testing.T) {
	s := New("Reduce p99 latency", "internal/**", 5, Toolchain{"go", "go test ./..."})

	var agentBody string
	for _, f := range s.Files() {
		if strings.HasSuffix(f.Path, "loop.json") {
			agentBody = f.Body
		}
	}
	if agentBody == "" {
		t.Fatal("no agent file generated")
	}

	var cfg map[string]any
	if err := json.Unmarshal([]byte(agentBody), &cfg); err != nil {
		t.Fatalf("agent config is not valid JSON: %v\n%s", err, agentBody)
	}

	hooks, ok := cfg["hooks"].(map[string]any)
	if !ok {
		t.Fatal("hooks missing")
	}
	// The verifier must be on stop, so the harness runs it rather than the agent
	// choosing to.
	stop, ok := hooks["stop"].([]any)
	if !ok || len(stop) != 1 {
		t.Fatalf("expected one stop hook, got %v", hooks["stop"])
	}
	if got := stop[0].(map[string]any)["command"]; got != ".kiro/loop/verify.sh" {
		t.Errorf("stop hook should run the verifier, got %v", got)
	}
	// State must arrive via agentSpawn (once) not resources (every turn).
	spawn, ok := hooks["agentSpawn"].([]any)
	if !ok || len(spawn) != 1 {
		t.Fatalf("expected one agentSpawn hook, got %v", hooks["agentSpawn"])
	}
	if got := spawn[0].(map[string]any)["command"]; !strings.Contains(got.(string), "state.json") {
		t.Errorf("agentSpawn should inject the ledger, got %v", got)
	}
	for _, r := range cfg["resources"].([]any) {
		if strings.Contains(r.(string), "state.json") {
			t.Error("state.json must not be a resource; that re-sends it every turn")
		}
	}

	// The verifier must be unwritable by the agent.
	ts := cfg["toolsSettings"].(map[string]any)
	denied := ts["write"].(map[string]any)["deniedPaths"].([]any)
	var found bool
	for _, d := range denied {
		if d == ".kiro/loop/verify.sh" {
			found = true
		}
	}
	if !found {
		t.Error("verify.sh must be in write deniedPaths, or the agent can grade itself")
	}
	allowed := ts["write"].(map[string]any)["allowedPaths"].([]any)
	if allowed[0] != "internal/**" {
		t.Errorf("editable glob not propagated: %v", allowed)
	}
}

func TestStateIsValidJSONWithGoalAndCap(t *testing.T) {
	s := New(`a "quoted" goal`, "src/**", 7, Toolchain{"go", "go test ./..."})

	var body string
	for _, f := range s.Files() {
		if strings.HasSuffix(f.Path, "state.json") {
			body = f.Body
		}
	}
	var st struct {
		Goal        string `json:"goal"`
		MaxAttempts int    `json:"max_attempts"`
		Attempts    []any  `json:"attempts"`
	}
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatalf("state.json invalid: %v\n%s", err, body)
	}
	if st.Goal != `a "quoted" goal` {
		t.Errorf("goal not escaped correctly: %q", st.Goal)
	}
	if st.MaxAttempts != 7 {
		t.Errorf("max_attempts: got %d", st.MaxAttempts)
	}
	if len(st.Attempts) != 0 {
		t.Errorf("attempts should start empty")
	}
}

func TestVerifierEmbedsToolchainAndCap(t *testing.T) {
	s := New("g", "src/**", 3, Toolchain{"cargo", "cargo test"})
	var body string
	for _, f := range s.Files() {
		if strings.HasSuffix(f.Path, "verify.sh") {
			body = f.Body
		}
	}
	if !strings.Contains(body, "cargo test") {
		t.Error("verifier should run the detected command")
	}
	if !strings.Contains(body, "max=${max:-3}") {
		t.Errorf("verifier should embed the attempt cap as a fallback:\n%s", body)
	}
	if !strings.HasPrefix(body, "#!/usr/bin/env bash") {
		t.Error("verifier needs a shebang")
	}
}

func TestWriteCreatesFilesWithCorrectModes(t *testing.T) {
	ws := t.TempDir()
	s := New("g", "src/**", 5, Toolchain{"go", "go test ./..."})

	written, skipped, err := s.Write(ws, false)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(written) != 4 {
		t.Errorf("expected 4 files written, got %v", written)
	}
	if len(skipped) != 0 {
		t.Errorf("nothing should be skipped on a clean tree, got %v", skipped)
	}

	fi, err := os.Stat(filepath.Join(ws, Dir, "verify.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("verify.sh must be executable, got %v", fi.Mode().Perm())
	}
}

func TestWriteDoesNotClobberExistingLedger(t *testing.T) {
	ws := t.TempDir()
	s := New("g", "src/**", 5, Toolchain{"go", "go test ./..."})
	if _, _, err := s.Write(ws, false); err != nil {
		t.Fatal(err)
	}

	// Simulate work in progress.
	ledger := filepath.Join(ws, Dir, "state.json")
	if err := os.WriteFile(ledger, []byte(`{"attempts":[{"hypothesis":"x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	written, skipped, err := s.Write(ws, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 0 {
		t.Errorf("re-running should write nothing, got %v", written)
	}
	if len(skipped) != 4 {
		t.Errorf("expected all 4 skipped, got %v", skipped)
	}
	got, _ := os.ReadFile(ledger)
	if !strings.Contains(string(got), `"hypothesis":"x"`) {
		t.Error("in-progress ledger was destroyed")
	}
}

func TestForceOverwrites(t *testing.T) {
	ws := t.TempDir()
	s := New("g", "src/**", 5, Toolchain{"go", "go test ./..."})
	if _, _, err := s.Write(ws, false); err != nil {
		t.Fatal(err)
	}
	written, _, err := s.Write(ws, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 4 {
		t.Errorf("force should rewrite all 4, got %v", written)
	}
}

func TestProgramDocumentsEditableGlob(t *testing.T) {
	s := New("g", "lib/**/*.rb", 5, Toolchain{"make", "make test"})
	var body string
	for _, f := range s.Files() {
		if strings.HasSuffix(f.Path, "program.md") {
			body = f.Body
		}
	}
	if !strings.Contains(body, "lib/**/*.rb") {
		t.Error("program.md should state what may be edited")
	}
	if !strings.Contains(body, "verify.sh") {
		t.Error("program.md should state that the verifier is off-limits")
	}
}

// --------------------------------------------------------------- scored loop

func TestUnscoredLoopIsLabelledAsAGate(t *testing.T) {
	s := New("g", "src/**", 5, Toolchain{"go", "true"})
	if s.Scored() {
		t.Fatal("no metric means unscored")
	}
	var verify, program string
	for _, f := range s.Files() {
		if strings.HasSuffix(f.Path, "verify.sh") {
			verify = f.Body
		}
		if strings.HasSuffix(f.Path, "program.md") {
			program = f.Body
		}
	}
	if !strings.Contains(verify, "gate rather than a search") {
		t.Error("verify.sh should say it cannot detect improvement")
	}
	if !strings.Contains(program, "cannot tell an improvement from a no-op") {
		t.Error("program.md should tell the agent not to claim unmeasured wins")
	}
}

func TestScoredLoopEmbedsMetricAndDirection(t *testing.T) {
	s := New("g", "src/**", 5, Toolchain{"go", "true"}).WithMetric("wc -l < out.txt", "higher")
	if !s.Scored() {
		t.Fatal("expected Scored")
	}
	if s.Direction != "higher" {
		t.Errorf("direction: %q", s.Direction)
	}
	var verify, state string
	for _, f := range s.Files() {
		if strings.HasSuffix(f.Path, "verify.sh") {
			verify = f.Body
		}
		if strings.HasSuffix(f.Path, "state.json") {
			state = f.Body
		}
	}
	if !strings.Contains(verify, "wc -l < out.txt") {
		t.Error("metric command not embedded")
	}
	if !strings.Contains(verify, `(s > b)`) {
		t.Errorf("higher-is-better must use >:\n%s", verify)
	}
	var st struct {
		Metric struct {
			Command   string `json:"command"`
			Direction string `json:"direction"`
		} `json:"metric"`
	}
	if err := json.Unmarshal([]byte(state), &st); err != nil {
		t.Fatalf("state.json invalid: %v", err)
	}
	if st.Metric.Direction != "higher" || st.Metric.Command != "wc -l < out.txt" {
		t.Errorf("metric metadata wrong: %+v", st.Metric)
	}
}

func TestLowerIsBetterUsesLessThan(t *testing.T) {
	s := New("g", "src/**", 5, Toolchain{"go", "true"}).WithMetric("echo 1", "lower")
	for _, f := range s.Files() {
		if strings.HasSuffix(f.Path, "verify.sh") && !strings.Contains(f.Body, `(s < b)`) {
			t.Errorf("lower-is-better must use <:\n%s", f.Body)
		}
	}
}

func TestScoreFilesAreDeniedToTheAgent(t *testing.T) {
	// Whoever owns the score can win by rewriting it.
	s := New("g", "src/**", 5, Toolchain{"go", "true"}).WithMetric("echo 1", "lower")
	var agent string
	for _, f := range s.Files() {
		if strings.HasSuffix(f.Path, "loop.json") {
			agent = f.Body
		}
	}
	for _, want := range []string{".kiro/loop/best", ".kiro/loop/score.log", ".kiro/loop/verify.sh"} {
		if !strings.Contains(agent, want) {
			t.Errorf("%s must be in deniedPaths", want)
		}
	}
}

func TestLoopAgentShipsEnforcementHook(t *testing.T) {
	s := New("g", "src/**", 5, Toolchain{"go", "true"})
	var agent string
	for _, f := range s.Files() {
		if strings.HasSuffix(f.Path, "loop.json") {
			agent = f.Body
		}
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(agent), &cfg); err != nil {
		t.Fatal(err)
	}
	pre, ok := cfg["hooks"].(map[string]any)["preToolUse"].([]any)
	if !ok || len(pre) != 1 {
		t.Fatalf("loop agent needs preToolUse enforcement, got %v", cfg["hooks"])
	}
	if got := pre[0].(map[string]any)["command"]; got != "kirobuff enforce" {
		t.Errorf("command: got %v", got)
	}
}
