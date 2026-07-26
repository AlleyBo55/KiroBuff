package mode

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func layout(t *testing.T) Layout {
	t.Helper()
	root := t.TempDir()
	return Layout{
		Library:  filepath.Join(root, "kirobuff", "modes"),
		Steering: filepath.Join(root, "steering"),
	}
}

func TestCatalogueHasSpankAndCofounder(t *testing.T) {
	r := Registry()
	if m, ok := r["spank"]; !ok || m.Kind != System {
		t.Errorf("spank should exist as a system mode, got %+v", m)
	}
	if m, ok := r["tech-cofounder"]; !ok || m.Kind != Prompt {
		t.Errorf("tech-cofounder should exist as a prompt mode, got %+v", m)
	}
}

func TestMoreModesExistThanCanBeActive(t *testing.T) {
	// The cap only means something if it can actually be hit.
	prompts := 0
	for _, m := range all() {
		if m.Kind == Prompt {
			prompts++
		}
	}
	if prompts <= MaxActive {
		t.Errorf("only %d prompt modes for a cap of %d; the cap never bites", prompts, MaxActive)
	}
}

func TestPromptModesHaveBodiesAndSystemModesDoNot(t *testing.T) {
	for _, m := range all() {
		switch m.Kind {
		case Prompt:
			if strings.TrimSpace(m.Body) == "" {
				t.Errorf("%s: prompt mode needs a fragment", m.Name)
			}
			if len(m.Body) > 2000 {
				t.Errorf("%s: fragment is %d bytes and is re-sent every turn", m.Name, len(m.Body))
			}
		case System:
			if m.Body != "" {
				t.Errorf("%s: system mode should not carry a fragment", m.Name)
			}
		}
		if m.Summary == "" {
			t.Errorf("%s: needs a summary", m.Name)
		}
	}
}

func TestOnOffRoundTrip(t *testing.T) {
	l := layout(t)

	if err := On(l, "paranoid"); err != nil {
		t.Fatalf("On: %v", err)
	}
	if !IsActive(l, "paranoid") {
		t.Fatal("expected active")
	}
	if got := Active(l); len(got) != 1 || got[0] != "paranoid" {
		t.Errorf("Active: %v", got)
	}

	// The link must resolve to the library fragment.
	target, err := os.Readlink(l.activePath("paranoid"))
	if err != nil {
		t.Fatalf("not a symlink: %v", err)
	}
	if target != l.libraryPath("paranoid") {
		t.Errorf("link target: %s", target)
	}
	body, err := os.ReadFile(l.activePath("paranoid"))
	if err != nil {
		t.Fatalf("link does not resolve: %v", err)
	}
	if !strings.Contains(string(body), Marker) {
		t.Error("fragment missing marker")
	}

	if err := Off(l, "paranoid"); err != nil {
		t.Fatalf("Off: %v", err)
	}
	if IsActive(l, "paranoid") {
		t.Error("expected inactive")
	}
	// The library copy survives, so re-enabling is cheap.
	if _, err := os.Stat(l.libraryPath("paranoid")); err != nil {
		t.Error("library fragment should remain after Off")
	}
}

func TestModesCompose(t *testing.T) {
	// The whole point: several at once.
	l := layout(t)
	for _, n := range []string{"tech-cofounder", "paranoid", "perf"} {
		if err := On(l, n); err != nil {
			t.Fatalf("On %s: %v", n, err)
		}
	}
	if got := Active(l); len(got) != 3 {
		t.Fatalf("expected 3 active, got %v", got)
	}
	if Remaining(l) != MaxActive-3 {
		t.Errorf("Remaining: got %d", Remaining(l))
	}
}

func TestCapIsEnforcedWithAHelpfulError(t *testing.T) {
	l := layout(t)
	var on []string
	for _, m := range all() {
		if m.Kind != Prompt {
			continue
		}
		if len(on) == MaxActive {
			err := On(l, m.Name)
			if !errors.Is(err, ErrTooMany) {
				t.Fatalf("expected ErrTooMany, got %v", err)
			}
			// The message must say why, since the cap looks arbitrary otherwise.
			if !strings.Contains(err.Error(), "every turn") {
				t.Errorf("error should explain the context cost: %v", err)
			}
			for _, name := range on {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("error should list active modes, missing %s", name)
				}
			}
			return
		}
		if err := On(l, m.Name); err != nil {
			t.Fatalf("On %s: %v", m.Name, err)
		}
		on = append(on, m.Name)
	}
	t.Fatalf("never reached the cap with %d modes on", len(on))
}

func TestOnIsIdempotent(t *testing.T) {
	l := layout(t)
	if err := On(l, "perf"); err != nil {
		t.Fatal(err)
	}
	if err := On(l, "perf"); err != nil {
		t.Errorf("second On should be a no-op, got %v", err)
	}
	if got := Active(l); len(got) != 1 {
		t.Errorf("duplicate activation: %v", got)
	}
}

func TestSystemModeCannotBeToggled(t *testing.T) {
	l := layout(t)
	err := On(l, "spank")
	if !errors.Is(err, ErrSystem) {
		t.Fatalf("expected ErrSystem, got %v", err)
	}
	if !strings.Contains(err.Error(), "explain") {
		t.Errorf("error should point at the instructions command: %v", err)
	}
}

func TestUnknownModeAndOffWhenNotOn(t *testing.T) {
	l := layout(t)
	if err := On(l, "nope"); !errors.Is(err, ErrUnknown) {
		t.Errorf("expected ErrUnknown, got %v", err)
	}
	if err := Off(l, "perf"); !errors.Is(err, ErrNotOn) {
		t.Errorf("expected ErrNotOn, got %v", err)
	}
}

func TestRefusesToTouchAFileItDidNotCreate(t *testing.T) {
	l := layout(t)
	if err := os.MkdirAll(l.Steering, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := l.activePath("perf")
	if err := os.WriteFile(mine, []byte("# my own steering\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := On(l, "perf"); !errors.Is(err, ErrConflict) {
		t.Errorf("On should refuse a regular file, got %v", err)
	}
	if err := Off(l, "perf"); !errors.Is(err, ErrConflict) {
		t.Errorf("Off should refuse to delete a regular file, got %v", err)
	}
	body, _ := os.ReadFile(mine)
	if string(body) != "# my own steering\n" {
		t.Error("user file was modified")
	}
}

// ------------------------------------------------------------- per-agent

func TestAttachAndDetachPerAgent(t *testing.T) {
	l := layout(t)
	if err := Sync(l); err != nil {
		t.Fatal(err)
	}
	cfg := []byte(`{"name":"reviewer","tools":["read"],"resources":["file://README.md"]}`)

	patched, err := AttachToAgent(cfg, l, "paranoid")
	if err != nil {
		t.Fatalf("AttachToAgent: %v", err)
	}
	names, err := AgentModes(patched)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "paranoid" {
		t.Fatalf("AgentModes: %v", names)
	}
	// Unrelated resources must survive.
	if !strings.Contains(string(patched), "README.md") {
		t.Error("existing resource dropped")
	}

	back, err := DetachFromAgent(patched, "paranoid")
	if err != nil {
		t.Fatalf("DetachFromAgent: %v", err)
	}
	if names, _ := AgentModes(back); len(names) != 0 {
		t.Errorf("expected no modes after detach, got %v", names)
	}
	if !strings.Contains(string(back), "README.md") {
		t.Error("detach dropped an unrelated resource")
	}
}

func TestPerAgentModesAreIndependent(t *testing.T) {
	// Agent A in paranoid, agent B in perf, neither paying for the other.
	l := layout(t)
	_ = Sync(l)

	a, err := AttachToAgent([]byte(`{"name":"a"}`), l, "paranoid")
	if err != nil {
		t.Fatal(err)
	}
	b, err := AttachToAgent([]byte(`{"name":"b"}`), l, "perf")
	if err != nil {
		t.Fatal(err)
	}
	am, _ := AgentModes(a)
	bm, _ := AgentModes(b)
	if len(am) != 1 || am[0] != "paranoid" {
		t.Errorf("agent a: %v", am)
	}
	if len(bm) != 1 || bm[0] != "perf" {
		t.Errorf("agent b: %v", bm)
	}
	if strings.Contains(string(a), "perf.md") {
		t.Error("agent a should not carry agent b's mode")
	}
}

func TestAttachIsIdempotentAndCapped(t *testing.T) {
	l := layout(t)
	_ = Sync(l)

	cfg := []byte(`{"name":"a"}`)
	patched, err := AttachToAgent(cfg, l, "perf")
	if err != nil {
		t.Fatal(err)
	}
	// A second attach reports no change rather than duplicating.
	again, err := AttachToAgent(patched, l, "perf")
	if err != nil {
		t.Fatalf("second attach: %v", err)
	}
	if again != nil {
		t.Error("expected nil output signalling no change")
	}

	// Fill to the cap.
	cur := patched
	count := 1
	for _, m := range all() {
		if m.Kind != Prompt || m.Name == "perf" {
			continue
		}
		next, err := AttachToAgent(cur, l, m.Name)
		if count >= MaxActive {
			if !errors.Is(err, ErrTooMany) {
				t.Fatalf("expected ErrTooMany at %d, got %v", count, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("attach %s: %v", m.Name, err)
		}
		cur = next
		count++
	}
	t.Fatal("never hit the per-agent cap")
}

func TestSystemModeCannotAttachToAnAgent(t *testing.T) {
	l := layout(t)
	if _, err := AttachToAgent([]byte(`{"name":"a"}`), l, "spank"); !errors.Is(err, ErrSystem) {
		t.Errorf("expected ErrSystem, got %v", err)
	}
}

func TestModeNameFromResourceIgnoresUnrelatedPaths(t *testing.T) {
	cases := map[string]string{
		"file:///home/u/.kiro/kirobuff/modes/perf.md": "perf",
		"file://README.md":                            "",
		"file:///home/u/.kiro/steering/perf.md":       "",
		"skill://.kiro/skills/x/SKILL.md":             "",
		"file:///home/u/notes/perf.md":                "",
	}
	for in, want := range cases {
		if got := modeNameFromResource(in); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestAttachRejectsMalformedConfig(t *testing.T) {
	l := layout(t)
	if _, err := AttachToAgent([]byte(`{nope`), l, "perf"); err == nil {
		t.Error("expected a parse error")
	}
}

func TestFocusTargetsDriftNotVerbosity(t *testing.T) {
	// terse trims the output. focus trims the reasoning. Conflating them would
	// leave the actual problem - the model working on adjacent questions -
	// unaddressed.
	focus, err := Get("focus")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ToLower(focus.Body)
	// Each of these names a concrete mechanism by which drift happens, rather
	// than saying "stay on topic".
	for _, want := range []string{
		"do not read files you do not need",
		"adjacent question",
		"options you are not going to take",
		"unrelated problem",
		"generalise before the second case",
		"stopping condition",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("focus should address %q", want)
		}
	}

	terse, _ := Get("terse")
	if strings.Contains(strings.ToLower(terse.Body), "adjacent") {
		t.Error("terse should stay about output, not reasoning scope")
	}
}

func TestFocusAndTerseCanBothBeActive(t *testing.T) {
	// They address different layers, so combining them is the useful case.
	l := layout(t)
	for _, n := range []string{"focus", "terse"} {
		if err := On(l, n); err != nil {
			t.Fatalf("On %s: %v", n, err)
		}
	}
	if got := Active(l); len(got) != 2 {
		t.Errorf("expected both active, got %v", got)
	}
}

func TestCapHoldsUnderConcurrency(t *testing.T) {
	// Seven parallel invocations against a cap of six produced seven active
	// modes before the lock existed: every process read the count before any
	// wrote a link.
	l := layout(t)
	if err := Sync(l); err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, m := range all() {
		if m.Kind == Prompt {
			names = append(names, m.Name)
		}
	}
	if len(names) <= MaxActive {
		t.Skipf("need more than %d prompt modes to force the race", MaxActive)
	}

	var wg sync.WaitGroup
	for _, n := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_ = On(l, name) // over-cap errors are expected and ignored here
		}(n)
	}
	wg.Wait()

	if got := len(Active(l)); got > MaxActive {
		t.Errorf("cap exceeded under concurrency: %d active, cap is %d", got, MaxActive)
	}
}

func TestStaleLockIsBroken(t *testing.T) {
	// A process killed mid-operation must not wedge the command forever.
	l := layout(t)
	if err := os.MkdirAll(l.Library, 0o755); err != nil {
		t.Fatal(err)
	}
	lock := l.lockPath()
	if err := os.WriteFile(lock, []byte("99999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * lockStale)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}

	if err := On(l, "perf"); err != nil {
		t.Fatalf("a stale lock should be broken, got %v", err)
	}
	if !IsActive(l, "perf") {
		t.Error("the mode should be active after breaking a stale lock")
	}
}

func TestFreshLockBlocksAndExplains(t *testing.T) {
	l := layout(t)
	if err := os.MkdirAll(l.Library, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.lockPath(), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := On(l, "perf")
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
	if !strings.Contains(err.Error(), l.lockPath()) {
		t.Errorf("the error should name the lock file: %v", err)
	}
}

func TestLockIsNotMistakenForASteeringFile(t *testing.T) {
	// Kiro CLI loads every .md under the steering directory, so the lock must
	// not live there.
	l := layout(t)
	if err := On(l, "perf"); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(l.lockPath()) == l.Steering {
		t.Error("the lock must not sit in the steering directory")
	}
}
