// Command kirobuff applies guardrails, tuning, and instrumentation to Kiro CLI.
//
// The organising idea is that most of what people want from an agent harness is
// not more capability but fewer ways to be surprised. Everything here is a
// config file or hook Kiro CLI already knows how to read, with one exception
// that matters: the enforcement hook exits 2 on a preToolUse event, which
// blocks the tool call outright rather than asking the model to reconsider.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/AlleyBo55/KiroBuff/semver"
)

const usage = `kirobuff - a buff for Kiro CLI

Your agent already has the stats. This is the status effect.

Quick start:
  kirobuff install                    guardrails, personas, effort defaults

Always on:
  kirobuff guardrails install [-scope global|workspace]
        Change-safety policy, inherited by every agent
  kirobuff enforce install <agent.json>
        Five rules that block the tool call instead of warning

Modes:
  kirobuff mode list | status
        List modes, or show what is active
  kirobuff mode on|off <name> [-agent path]
        Toggle a mode. Composable, up to 6 at once.
        With -agent, only that agent carries it.
  kirobuff mode explain spank [-scope ac|all]
        Platform instructions for working with the lid closed
  kirobuff agent install <name> [-scope global|workspace] [-shortcut KEY]
        Install an agent carrying the enforcement hook

Cost:
  kirobuff budget <agent.json> [-max N] [-quiet]
        Estimate recurring per-turn token cost.
        With -max, exit 1 when the estimate exceeds N.
  kirobuff guard install <agent.json> [-max N] [-ttl N] [-dry-run]
        Run the budget check every session. Costs no tokens:
        the warning goes to you, not the model.
  kirobuff tune [-model ID] [-effort LEVEL] [-show]
        Per-model reasoning effort defaults
  kirobuff statusline install <agent.json> [-max N]
        Active modes and live cost, in the terminal title

Work:
  kirobuff loop init [-goal "..."] [-editable glob] [-metric CMD]
                     [-direction lower|higher] [-max-attempts N]
        Scaffold a loop: verifier, scored ledger, stop condition
  kirobuff attest -model ID [-agent NAME] [-tools a,b] [-f FILE] [-w]
  kirobuff attest -check [-as-agent] -f FILE
        Assisted-by trailers and DCO validation

No conflicts:
  kirobuff preflight [-base REF] [-quiet]
        Check this branch against its base before pushing
  kirobuff preflight install [-force]
        Install it as a pre-push hook, so it always runs

Info:
  kirobuff status [-C dir]            what each harness has on disk
  kirobuff version [next]             build identity, or the next release

Flags:
  -C dir     Workspace root (default: current directory)
  -max N     Token-per-turn budget (default 2000 for guard, 0 = no limit)
  -quiet     Suppress stdout; stderr warning only
  -ttl N     Hook cache_ttl_seconds (default 3600)
  -dry-run   Print the patched config instead of writing it
  -force     Overwrite a file kirobuff did not create
`

// valueFlags are the flags that consume the argument after them. permute needs
// to know these to move a flag and its value together.
var valueFlags = map[string]bool{
	"-C": true, "-max": true, "-ttl": true, "-goal": true,
	"-editable": true, "-max-attempts": true, "-scope": true, "-shortcut": true, "-model": true, "-effort": true, "-metric": true, "-direction": true,
	"-agent": true, "-tools": true, "-f": true, "-base": true,
	"--C": true, "--max": true, "--ttl": true,
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "status":
		fail(cmdStatus(os.Args[2:]))
	case "budget":
		fail(cmdBudget(os.Args[2:]))
	case "guard":
		fail(cmdGuard(os.Args[2:]))
	case "loop":
		fail(cmdLoop(os.Args[2:]))
	case "mode":
		fail(cmdMode(os.Args[2:]))
	case "agent":
		if len(os.Args) < 3 || os.Args[2] != "install" {
			fail(errors.New("agent: only `agent install <name>` is supported"))
		}
		fail(modeInstall(os.Args[3:]))
	case "tune":
		fail(cmdTune(os.Args[2:]))
	case "guardrails":
		fail(cmdGuardrails(os.Args[2:]))
	case "statusline":
		fail(cmdStatusline(os.Args[2:]))
	case "install":
		fail(cmdInstall(os.Args[2:]))
	case "enforce":
		fail(cmdEnforce(os.Args[2:]))
	case "attest":
		fail(cmdAttest(os.Args[2:]))
	case "preflight":
		fail(cmdPreflight(os.Args[2:]))
	case "version", "-v", "--version":
		if len(os.Args) > 2 && os.Args[2] == "next" {
			fail(cmdVersionNext())
			return
		}
		fmt.Println(semver.Get())
		in := semver.Get()
		fmt.Printf("  version source  %s\n", in.Source)
		if in.Date != "" {
			fmt.Printf("  built           %s\n", in.Date)
		}
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "kirobuff: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

// overBudgetError signals a threshold breach rather than a malfunction. It
// exits 1 without the "kirobuff:" prefix, because the message is the warning.
type overBudgetError struct{ msg string }

func (e *overBudgetError) Error() string { return e.msg }

func fail(err error) {
	if err == nil {
		return
	}
	var ob *overBudgetError
	if errors.As(err, &ob) {
		fmt.Fprintln(os.Stderr, ob.msg)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "kirobuff: %v\n", err)
	os.Exit(1)
}
