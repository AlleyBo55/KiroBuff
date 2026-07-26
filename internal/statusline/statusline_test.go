package statusline

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRenderFields(t *testing.T) {
	cases := []struct {
		name   string
		s      Status
		want   []string
		absent []string
	}{
		{
			name: "minimal",
			s:    Status{},
			want: []string{"kirobuff"},
		},
		{
			name: "mode and workspace",
			s:    Status{Mode: "tech-cofounder", Workspace: "kirobuff"},
			want: []string{"kirobuff", "tech-cofounder"},
		},
		{
			name:   "tokens without budget",
			s:      Status{TokensPerTurn: 3000},
			want:   []string{"3.0k tok"},
			absent: []string{"/"},
		},
		{
			name:   "tokens under budget",
			s:      Status{TokensPerTurn: 1500, Budget: 2000},
			want:   []string{"1.5k/2.0k tok"},
			absent: []string{"!"},
		},
		{
			name: "over budget is marked",
			s:    Status{TokensPerTurn: 3000, Budget: 2000},
			want: []string{"3.0k/2.0k! tok"},
		},
		{
			name: "small counts are not abbreviated",
			s:    Status{TokensPerTurn: 400},
			want: []string{"400 tok"},
		},
	}
	for _, c := range cases {
		got := c.s.Render()
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q should contain %q", c.name, got, w)
			}
		}
		for _, a := range c.absent {
			if strings.Contains(got, a) {
				t.Errorf("%s: %q should not contain %q", c.name, got, a)
			}
		}
	}
}

func TestCreditsAreNotRendered(t *testing.T) {
	// Account usage is only reachable via the in-session /usage command, so any
	// credit figure here would be invented. Status has no field for it.
	s := Status{Mode: "m", Workspace: "w", TokensPerTurn: 1, Budget: 2}
	got := strings.ToLower(s.Render())
	for _, forbidden := range []string{"credit", "remaining", "%"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("render must not imply credit data: %q", got)
		}
	}
}

func TestOSCWrapsCorrectly(t *testing.T) {
	got := OSC("hello")
	if got != "\033]0;hello\007" {
		t.Errorf("got %q", got)
	}
}

func TestOSCStripsSequenceBreakingBytes(t *testing.T) {
	// A BEL or ESC inside the payload terminates the sequence early and leaks
	// the remainder onto the screen.
	got := OSC("a\abcd\033[31m\nnew\rx")
	if strings.Count(got, "\007") != 1 {
		t.Errorf("expected exactly one terminator, got %q", got)
	}
	if strings.Count(got, "\033") != 1 {
		t.Errorf("expected exactly one escape, got %q", got)
	}
	if strings.ContainsAny(got[4:len(got)-1], "\n\r") {
		t.Errorf("newlines must be stripped, got %q", got)
	}
}

func TestWriteToBuffer(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Status{Mode: "x"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "\033]0;") || !strings.HasSuffix(out, "\007") {
		t.Errorf("malformed sequence: %q", out)
	}
	if !strings.Contains(out, "x") {
		t.Errorf("mode missing: %q", out)
	}
}

func TestAsciiModeChangesSeparator(t *testing.T) {
	t.Setenv("KIRO_ASCII_MODE", "1")
	got := Status{Mode: "m"}.Render()
	if strings.Contains(got, "·") {
		t.Errorf("ASCII mode should not emit a middle dot: %q", got)
	}
	if !strings.Contains(got, ".") {
		t.Errorf("expected an ASCII separator: %q", got)
	}
}

func TestUnicodeSeparatorByDefault(t *testing.T) {
	t.Setenv("KIRO_ASCII_MODE", "")
	got := Status{Mode: "m"}.Render()
	if !strings.Contains(got, "·") {
		t.Errorf("expected a middle dot by default: %q", got)
	}
}

func TestWriteTTYDegradesGracefully(t *testing.T) {
	// This test environment has no controlling terminal. The contract that
	// matters is that the failure is the sentinel, never a panic, so the hook
	// can exit 0 and stay silent.
	err := WriteTTY(Status{Mode: "x"})
	if err != nil && !errors.Is(err, ErrNoTTY) {
		t.Errorf("unexpected error kind: %v", err)
	}
	// Available must agree with WriteTTY.
	if Available() != (err == nil) {
		t.Errorf("Available()=%v disagrees with WriteTTY err=%v", Available(), err)
	}
}

func TestHookCommandNeverFailsTheHook(t *testing.T) {
	got := HookCommand(".kiro/agents/a.json")
	if !strings.HasSuffix(got, "|| true") {
		t.Errorf("hook must not be able to fail, got %q", got)
	}
	if !strings.Contains(got, ".kiro/agents/a.json") {
		t.Errorf("config path missing from %q", got)
	}
}

func TestWriteTTYReturnsNoError(t *testing.T) {
	// On macOS in a terminal this should succeed; in CI it may return ErrNoTTY.
	// Either outcome is correct — the test just exercises the code path.
	s := Status{Mode: "test", Workspace: "proj", TokensPerTurn: 1500, Budget: 2000}
	err := WriteTTY(s)
	if err != nil && err != ErrNoTTY {
		t.Errorf("WriteTTY returned unexpected error: %v", err)
	}
}

func TestAvailableReturnsBoolean(t *testing.T) {
	// Just exercise the code path; result depends on environment.
	_ = Available()
}

func TestHookCommandFormat(t *testing.T) {
	cmd := HookCommand("/path/to/agent.json")
	if cmd != "kirobuff statusline emit /path/to/agent.json || true" {
		t.Errorf("unexpected command: %q", cmd)
	}
}
