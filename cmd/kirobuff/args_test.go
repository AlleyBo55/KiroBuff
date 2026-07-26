package main

import (
	"reflect"
	"strings"
	"testing"
)

// permute previously shipped a bug that silently discarded a flag placed after
// a positional argument, so `budget path -C dir` analysed the wrong workspace
// and reported plausible numbers for the wrong tree. It had no tests.

func TestPermuteMovesFlagsAheadOfPositionals(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "flag with value after a positional",
			in:   []string{"agent.json", "-C", "/ws"},
			want: []string{"-C", "/ws", "agent.json"},
		},
		{
			name: "already in order",
			in:   []string{"-C", "/ws", "agent.json"},
			want: []string{"-C", "/ws", "agent.json"},
		},
		{
			name: "boolean flag after a positional",
			in:   []string{"agent.json", "-quiet"},
			want: []string{"-quiet", "agent.json"},
		},
		{
			name: "several flags interleaved",
			in:   []string{"a.json", "-max", "2000", "-quiet", "-C", "/ws"},
			want: []string{"-max", "2000", "-quiet", "-C", "/ws", "a.json"},
		},
		{
			name: "two positionals keep their order",
			in:   []string{"on", "paranoid", "-agent", "a.json"},
			want: []string{"-agent", "a.json", "on", "paranoid"},
		},
		{
			name: "no arguments",
			in:   nil,
			want: nil,
		},
		{
			name: "only positionals",
			in:   []string{"list"},
			want: []string{"list"},
		},
	}
	for _, c := range cases {
		got := permute(c.in)
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n  in   %v\n  got  %v\n  want %v", c.name, c.in, got, c.want)
		}
	}
}

func TestPermuteTreatsEverythingAfterDoubleDashAsPositional(t *testing.T) {
	// A path that begins with a dash has to survive, which is the whole point of
	// the -- convention.
	got := permute([]string{"-C", "/ws", "--", "-weird-file.json", "-alsofile"})
	want := []string{"-C", "/ws", "-weird-file.json", "-alsofile"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestPermuteKeepsBareDashAsPositional(t *testing.T) {
	// A lone "-" conventionally means stdin, not a flag.
	got := permute([]string{"-", "-C", "/ws"})
	want := []string{"-C", "/ws", "-"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestPermuteDoesNotConsumeAMissingFlagValue(t *testing.T) {
	// A value flag at the very end has nothing to take; it must not read past
	// the slice.
	got := permute([]string{"agent.json", "-C"})
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if got[0] != "-C" {
		t.Errorf("flag should still move ahead, got %v", got)
	}
}

func TestEveryValueFlagIsRegistered(t *testing.T) {
	// A value flag missing from the map gets its value treated as a positional,
	// which is how the original bug behaved. Every flag the CLI declares with an
	// argument must be listed.
	for _, f := range []string{"-C", "-max", "-ttl", "-goal", "-editable",
		"-max-attempts", "-scope", "-shortcut", "-model", "-effort", "-metric",
		"-direction", "-agent", "-tools", "-f"} {
		if !valueFlags[f] {
			t.Errorf("%s takes a value but is not in valueFlags", f)
		}
	}
}

func TestWithCommas(t *testing.T) {
	cases := map[int]string{
		0: "0", 7: "7", 999: "999", 1000: "1,000",
		12345: "12,345", 999999: "999,999", 1234567: "1,234,567",
	}
	for in, want := range cases {
		if got := withCommas(in); got != want {
			t.Errorf("withCommas(%d): got %q want %q", in, got, want)
		}
	}
}

func TestDisplayShortensHomePaths(t *testing.T) {
	if got := display("/home/u/.kiro/agents/a.json", "/home/u"); got != "~/.kiro/agents/a.json" {
		t.Errorf("got %q", got)
	}
	// A path outside home is left alone, and a prefix that merely looks similar
	// must not be shortened.
	if got := display("/other/place", "/home/u"); got != "/other/place" {
		t.Errorf("got %q", got)
	}
	if got := display("/home/username/x", "/home/u"); got != "/home/username/x" {
		t.Errorf("a partial prefix match must not shorten: got %q", got)
	}
	if got := display("/home/u/x", ""); got != "/home/u/x" {
		t.Errorf("empty home should be a no-op: got %q", got)
	}
}

func TestUsageDocumentsEveryDispatchedCommand(t *testing.T) {
	// A command that dispatches but is undocumented is invisible.
	for _, cmd := range []string{"status", "budget", "guard", "loop", "mode",
		"agent", "tune", "guardrails", "statusline", "install", "enforce",
		"attest", "version"} {
		if !strings.Contains(usage, cmd) {
			t.Errorf("usage does not mention %q", cmd)
		}
	}
}
