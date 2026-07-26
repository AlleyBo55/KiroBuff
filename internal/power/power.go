// Package power produces platform-correct instructions for keeping a machine
// awake with the lid closed, so an agent can keep working unattended.
//
// This package prints commands and reports state. It does not run privileged
// commands itself. Disabling lid sleep is a persistent, system-wide change with
// thermal and battery consequences, and it is not kirobuff's call to make on
// someone's laptop.
//
// The important asymmetry between platforms:
//
//	macOS   caffeinate cannot override lid close at all. Only
//	        `pmset disablesleep` can, and it needs sudo.
//	Linux   systemd-inhibit CAN inhibit the lid switch, scoped to one
//	        command, with no permanent change. Cleanest of the three.
//	Windows powercfg changes the lid action per power scheme; no scoped
//	        equivalent, so it is a persistent setting.
//
// VERIFIED ON macOS ONLY. The Windows and Linux command sequences follow the
// documented interfaces for those platforms but have not been executed here.
package power

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Platform is a supported operating system.
type Platform string

// The platforms with a documented lid-sleep mechanism.
const (
	MacOS   Platform = "darwin"
	Linux   Platform = "linux"
	Windows Platform = "windows"
)

// Current returns the running platform.
func Current() Platform { return Platform(runtime.GOOS) }

// Scope limits a change to one power source.
type Scope string

// Power-source scopes for a sleep change.
const (
	ACOnly Scope = "ac"  // recommended: battery runs flat and cannot vent heat
	Always Scope = "all" // AC and battery
)

// Instructions is a platform-specific recipe.
type Instructions struct {
	Platform Platform
	Scoped   bool     // true when the change lasts only for one command
	Enable   []string // commands to run
	Disable  []string // commands to undo
	Notes    []string
}

// For returns instructions for a platform and scope.
func For(p Platform, scope Scope) (Instructions, error) {
	switch p {
	case MacOS:
		return macOS(scope), nil
	case Linux:
		return linux(scope), nil
	case Windows:
		return windows(scope), nil
	}
	return Instructions{}, fmt.Errorf("unsupported platform %q", p)
}

func macOS(scope Scope) Instructions {
	flag := "-c" // AC only
	if scope == Always {
		flag = "-a"
	}
	in := Instructions{
		Platform: MacOS,
		Enable:   []string{fmt.Sprintf("sudo pmset %s disablesleep 1", flag)},
		Disable:  []string{fmt.Sprintf("sudo pmset %s disablesleep 0", flag)},
		Notes: []string{
			"caffeinate cannot do this. It holds a prevent-idle-sleep assertion " +
				"and has no effect on the lid-close path. caffeinate -s is also " +
				"void on battery by design.",
			"This setting persists across reboots until you set it back to 0.",
			"Check the current state with: pmset -g | grep -i disablesleep",
		},
	}
	if scope == Always {
		in.Notes = append(in.Notes,
			"-a covers battery too. A closed MacBook on battery will run to a "+
				"hard shutdown, and restricted airflow means sustained work heats "+
				"it with nowhere to vent. Prefer -c unless you have a reason.")
	} else {
		in.Notes = append(in.Notes,
			"Scoped to AC power, so unplugging restores normal sleep. This is the "+
				"safe default.")
	}
	in.Notes = append(in.Notes,
		"If you have an external display, clamshell mode does this with no sudo "+
			"and no system change: connect display, power, and a keyboard or mouse.")
	return in
}

func linux(scope Scope) Instructions {
	// systemd-inhibit is scoped to a single command, which makes it strictly
	// better than editing logind.conf: nothing to remember to undo.
	in := Instructions{
		Platform: Linux,
		Scoped:   true,
		Enable: []string{
			`systemd-inhibit --what=handle-lid-switch:sleep:idle \`,
			`  --who=kirobuff --why="agent run in progress" \`,
			`  kiro-cli chat --trust-tools=read,grep,glob,code "your task"`,
		},
		Disable: []string{"(nothing: the inhibit lock is released when the command exits)"},
		Notes: []string{
			"Linux is the only one of the three with a scoped mechanism. The lock " +
				"lives as long as the wrapped process, so there is no persistent " +
				"setting and nothing to revert.",
			"Verify an active lock with: systemd-inhibit --list",
		},
	}
	if scope == Always {
		in.Notes = append(in.Notes,
			"For a persistent change instead, create "+
				"/etc/systemd/logind.conf.d/99-kirobuff.conf containing "+
				"[Login] with HandleLidSwitch=ignore, "+
				"HandleLidSwitchExternalPower=ignore and "+
				"HandleLidSwitchDocked=ignore, then run "+
				"`sudo systemctl restart systemd-logind`. Restarting logind can "+
				"terminate active sessions on some configurations, so do it from a "+
				"console you can afford to lose.")
	}
	return in
}

func windows(scope Scope) Instructions {
	// LIDACTION 0 = do nothing. ac/dc value index selects the power source.
	var enable, disable []string
	if scope == Always || scope == ACOnly {
		enable = append(enable,
			`powercfg /setacvalueindex SCHEME_CURRENT SUB_BUTTONS LIDACTION 0`)
		disable = append(disable,
			`powercfg /setacvalueindex SCHEME_CURRENT SUB_BUTTONS LIDACTION 1`)
	}
	if scope == Always {
		enable = append(enable,
			`powercfg /setdcvalueindex SCHEME_CURRENT SUB_BUTTONS LIDACTION 0`)
		disable = append(disable,
			`powercfg /setdcvalueindex SCHEME_CURRENT SUB_BUTTONS LIDACTION 1`)
	}
	enable = append(enable, `powercfg /setactive SCHEME_CURRENT`)
	disable = append(disable, `powercfg /setactive SCHEME_CURRENT`)

	return Instructions{
		Platform: Windows,
		Enable:   enable,
		Disable:  disable,
		Notes: []string{
			"Run these in an Administrator prompt. LIDACTION 0 means do nothing; " +
				"1 restores sleep.",
			"Idle sleep is separate. Add " +
				"`powercfg /change standby-timeout-ac 0` to stop the machine " +
				"sleeping on inactivity as well.",
			"Inspect the current scheme with: powercfg /query SCHEME_CURRENT SUB_BUTTONS",
			"This is a persistent change to the active power scheme. There is no " +
				"per-command equivalent.",
		},
	}
}

// State is what could be detected about the current machine.
type State struct {
	Platform    Platform
	LidSleepOff bool // best-effort: true when lid sleep is known to be disabled
	Known       bool // false when detection is not implemented for this platform
	Detail      string
}

// Detect reports whether lid sleep is currently disabled.
//
// Only macOS is implemented, because pmset gives a reliable single answer.
// Windows and Linux report Known=false rather than guessing: claiming the lid
// is safe when it is not would cost someone an overnight run.
func Detect() State {
	p := Current()
	if p != MacOS {
		return State{Platform: p, Known: false,
			Detail: "detection not implemented for " + string(p) +
				"; follow the instructions and verify with the command shown"}
	}
	// Bounded: pmset has been known to block on a wedged power daemon, and
	// Detect is called from an interactive status command.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "pmset", "-g").Output()
	if err != nil {
		return State{Platform: p, Known: false, Detail: "could not run pmset: " + err.Error()}
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(strings.ToLower(line), "disablesleep") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[len(fields)-1] == "1" {
			return State{Platform: p, Known: true, LidSleepOff: true,
				Detail: "pmset reports disablesleep 1"}
		}
		return State{Platform: p, Known: true, LidSleepOff: false,
			Detail: "pmset reports disablesleep off"}
	}
	// pmset omits the key entirely when it has never been set.
	return State{Platform: p, Known: true, LidSleepOff: false,
		Detail: "pmset does not report disablesleep, so it is unset"}
}
