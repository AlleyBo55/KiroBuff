package power

import (
	"runtime"
	"strings"
	"testing"
)

func TestForCoversAllThreePlatforms(t *testing.T) {
	for _, p := range []Platform{MacOS, Linux, Windows} {
		in, err := For(p, ACOnly)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if len(in.Enable) == 0 {
			t.Errorf("%s: no enable commands", p)
		}
		if len(in.Disable) == 0 {
			t.Errorf("%s: no disable commands", p)
		}
		if len(in.Notes) == 0 {
			t.Errorf("%s: no notes", p)
		}
	}
	if _, err := For("plan9", ACOnly); err == nil {
		t.Error("unsupported platform should error")
	}
}

func TestMacOSUsesPmsetNotCaffeinate(t *testing.T) {
	// caffeinate cannot override lid close. Suggesting it would send someone
	// away believing the problem is solved.
	in, _ := For(MacOS, ACOnly)
	joined := strings.Join(in.Enable, " ")
	if !strings.Contains(joined, "pmset") || !strings.Contains(joined, "disablesleep") {
		t.Errorf("expected pmset disablesleep, got %q", joined)
	}
	if strings.Contains(joined, "caffeinate") {
		t.Error("caffeinate must not be offered as the mechanism")
	}
	notes := strings.ToLower(strings.Join(in.Notes, " "))
	if !strings.Contains(notes, "caffeinate cannot") {
		t.Error("notes should say explicitly that caffeinate does not work here")
	}
}

func TestMacOSScopeSelectsFlag(t *testing.T) {
	ac, _ := For(MacOS, ACOnly)
	if !strings.Contains(ac.Enable[0], "-c ") {
		t.Errorf("AC-only should use -c, got %q", ac.Enable[0])
	}
	all, _ := For(MacOS, Always)
	if !strings.Contains(all.Enable[0], "-a ") {
		t.Errorf("Always should use -a, got %q", all.Enable[0])
	}
	// The battery hazard must be stated when the change covers battery.
	notes := strings.ToLower(strings.Join(all.Notes, " "))
	for _, want := range []string{"battery", "airflow"} {
		if !strings.Contains(notes, want) {
			t.Errorf("Always scope should warn about %q", want)
		}
	}
}

func TestMacOSMentionsClamshellAlternative(t *testing.T) {
	in, _ := For(MacOS, ACOnly)
	if !strings.Contains(strings.ToLower(strings.Join(in.Notes, " ")), "clamshell") {
		t.Error("the no-sudo alternative should be offered")
	}
}

func TestLinuxIsScopedAndUsesSystemdInhibit(t *testing.T) {
	in, _ := For(Linux, ACOnly)
	if !in.Scoped {
		t.Error("Linux has a per-command mechanism and should be marked scoped")
	}
	joined := strings.Join(in.Enable, " ")
	if !strings.Contains(joined, "systemd-inhibit") {
		t.Errorf("expected systemd-inhibit, got %q", joined)
	}
	if !strings.Contains(joined, "handle-lid-switch") {
		t.Error("the lid switch must be among the inhibited events")
	}
}

func TestLinuxPersistentPathWarnsAboutLogindRestart(t *testing.T) {
	in, _ := For(Linux, Always)
	notes := strings.ToLower(strings.Join(in.Notes, " "))
	if !strings.Contains(notes, "logind.conf.d") {
		t.Error("the persistent option should name the drop-in path")
	}
	if !strings.Contains(notes, "terminate active sessions") {
		t.Error("restarting logind can kill sessions; that must be stated")
	}
}

func TestWindowsUsesPowercfgLidAction(t *testing.T) {
	in, _ := For(Windows, ACOnly)
	joined := strings.Join(in.Enable, " ")
	if !strings.Contains(joined, "LIDACTION 0") {
		t.Errorf("expected LIDACTION 0, got %q", joined)
	}
	if !strings.Contains(joined, "/setactive") {
		t.Error("the scheme must be reactivated for the change to apply")
	}
	// Reverting must restore sleep, not repeat the disable.
	if !strings.Contains(strings.Join(in.Disable, " "), "LIDACTION 1") {
		t.Error("disable should restore LIDACTION 1")
	}
	if in.Scoped {
		t.Error("Windows has no per-command mechanism")
	}
}

func TestWindowsAlwaysCoversBothPowerSources(t *testing.T) {
	ac, _ := For(Windows, ACOnly)
	all, _ := For(Windows, Always)

	if strings.Contains(strings.Join(ac.Enable, " "), "setdcvalueindex") {
		t.Error("AC-only must not set the DC (battery) index")
	}
	if !strings.Contains(strings.Join(all.Enable, " "), "setdcvalueindex") {
		t.Error("Always must set the DC index too")
	}
}

func TestDetectDoesNotGuessOnUnimplementedPlatforms(t *testing.T) {
	s := Detect()
	if s.Platform != Platform(runtime.GOOS) {
		t.Errorf("platform: got %s want %s", s.Platform, runtime.GOOS)
	}
	if s.Detail == "" {
		t.Error("Detect should always explain itself")
	}
	if runtime.GOOS != "darwin" {
		// Reporting "lid is safe" when it is not would cost an overnight run.
		if s.Known {
			t.Errorf("detection is only implemented for macOS, got Known=true on %s", runtime.GOOS)
		}
		if s.LidSleepOff {
			t.Error("must not claim lid sleep is off without evidence")
		}
	}
}

func TestDetectOnMacOSReportsAConclusion(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	s := Detect()
	if !s.Known {
		t.Errorf("pmset should give a definite answer on macOS: %s", s.Detail)
	}
}
