// Command kirobuff applies guardrails, tuning, and instrumentation to Kiro CLI.
//
// The organising idea is that most of what people want from an agent harness is
// not more capability but fewer ways to be surprised. Everything here is a
// config file or hook Kiro CLI already knows how to read, with one exception
// that matters: the enforcement hook exits 2 on a preToolUse event, which
// blocks the tool call outright rather than asking the model to reconsider.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"kirobuff/internal/attest"
	"kirobuff/internal/budget"
	"kirobuff/internal/discover"
	"kirobuff/internal/enforce"
	"kirobuff/internal/guard"
	"kirobuff/internal/loop"
	"kirobuff/internal/mode"
	"kirobuff/internal/persona"
	"kirobuff/internal/power"
	"kirobuff/internal/statusline"
	"kirobuff/internal/steering"
	"kirobuff/internal/tune"
)

const usage = `kirobuff - a buff for Kiro CLI

Your agent already has the stats. This is the status effect.

Quick start:
  kirobuff install                    guardrails, personas, effort defaults

Usage:
  kirobuff status [-C dir]
        Show what each harness currently has on disk

  kirobuff budget <agent.json> [-C dir] [-max N] [-quiet]
        Estimate recurring per-turn token cost.
        With -max, exit 1 when the estimate exceeds N tokens per turn.
        With -quiet, print nothing on stdout; warn on stderr only when over.

  kirobuff loop init [-C dir] [-goal "..."] [-editable glob] [-max-attempts N]
        Scaffold a Karpathy loop: verifier, state ledger, stop condition.

  kirobuff mode list
        List available opt-in personas

  kirobuff mode install <name> [-scope global|workspace] [-shortcut KEY]
        Install a persona you switch into with /agent <name>

  kirobuff guard install <agent.json> [-max N] [-ttl N] [-dry-run]
        Install the budget check as an agentSpawn hook so it runs every
        session. Costs no tokens: the warning goes to you, not the model.

Flags:
  -C dir     Workspace root (default: current directory)
  -max N     Token-per-turn budget (default 2000 for guard, 0 = no limit)
  -quiet     Suppress stdout; stderr warning only
  -ttl N     Hook cache_ttl_seconds (default 3600)
  -dry-run   Print the patched config instead of writing it
`

// valueFlags are the flags that consume the argument after them. permute needs
// to know these to move a flag and its value together.
var valueFlags = map[string]bool{
	"-C": true, "-max": true, "-ttl": true, "-goal": true,
	"-editable": true, "-max-attempts": true, "-scope": true, "-shortcut": true, "-model": true, "-effort": true, "-metric": true, "-direction": true,
	"-agent": true, "-tools": true, "-f": true,
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

// ---------------------------------------------------------------- status

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	wsFlag := fs.String("C", "", "workspace root")
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}
	ws, err := resolveWorkspace(*wsFlag)
	if err != nil {
		return err
	}

	layout, err := discover.DefaultLayout(ws)
	if err != nil {
		return err
	}
	artifacts, err := discover.Scan(layout)
	if err != nil {
		return err
	}

	fmt.Printf("workspace   %s\n", layout.Workspace)
	fmt.Printf("shared root %s\n", layout.SharedRoot)
	fmt.Printf("kiro home   %s\n\n", layout.KiroHome)

	if len(artifacts) == 0 {
		fmt.Println("No agent configuration found in either harness.")
		return nil
	}

	byKind := map[discover.Kind][]discover.Artifact{}
	for _, a := range artifacts {
		byKind[a.Kind] = append(byKind[a.Kind], a)
	}

	order := []discover.Kind{
		discover.KindMemory, discover.KindCommand, discover.KindAgent,
		discover.KindSkill, discover.KindSettings, discover.KindMCP,
	}

	var shared, unshared int
	for _, kind := range order {
		group := byKind[kind]
		if len(group) == 0 {
			continue
		}
		sort.Slice(group, func(i, j int) bool {
			if group[i].Harness != group[j].Harness {
				return group[i].Harness < group[j].Harness
			}
			return group[i].Path < group[j].Path
		})

		fmt.Printf("%s\n", strings.ToUpper(string(kind)))
		for _, a := range group {
			note := "authored here"
			switch {
			case a.SharedLink(layout.SharedRoot):
				note = "-> shared"
				shared++
			case a.IsSymlink:
				note = "-> " + a.LinkTarget
			default:
				if a.Harness != discover.Shared {
					unshared++
				}
			}
			fmt.Printf("  %-12s %-10s %-14s %s\n",
				a.Harness, a.Scope, note, display(a.Path, layout.Home))
		}
		fmt.Println()
	}

	fmt.Printf("%d artifact(s) already shared, %d still harness-specific\n", shared, unshared)
	return nil
}

// ---------------------------------------------------------------- budget

func cmdBudget(args []string) error {
	fs := flag.NewFlagSet("budget", flag.ExitOnError)
	wsFlag := fs.String("C", "", "workspace root")
	max := fs.Int("max", 0, "token-per-turn budget; 0 disables the check")
	quiet := fs.Bool("quiet", false, "stderr warning only")
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("budget: need a path to an agent config")
	}
	configPath := fs.Arg(0)

	ws, err := resolveWorkspace(*wsFlag)
	if err != nil {
		return err
	}
	agent, err := budget.Load(configPath)
	if err != nil {
		return err
	}
	findings := budget.Analyze(agent, ws)
	total := budget.Total(findings)

	// Quiet mode keeps stdout empty on purpose. Kiro CLI feeds a hook's stdout
	// into the model's context when the hook exits 0, so anything printed here
	// would be the very waste this command reports.
	if !*quiet {
		printReport(agent.Name, ws, findings, total)
	}

	if *max > 0 && total > *max {
		return &overBudgetError{msg: fmt.Sprintf(
			"kirobuff: agent %q costs ~%s tokens/turn, over the %s budget - run `kirobuff budget %s` for details",
			agent.Name, withCommas(total), withCommas(*max), configPath)}
	}
	return nil
}

func printReport(name, ws string, findings []budget.Finding, total int) {
	fmt.Printf("agent     %s\n", name)
	fmt.Printf("workspace %s\n\n", ws)

	if len(findings) == 0 {
		fmt.Println("No recurring context cost found. Nothing to trim.")
		return
	}
	for _, f := range findings {
		cost := "     -"
		if f.TokensPerTurn > 0 {
			cost = fmt.Sprintf("%6s", withCommas(f.TokensPerTurn))
		}
		fmt.Printf("[%-6s] %s tok/turn  %s\n", f.Severity, cost, f.Subject)
		fmt.Printf("           %s\n", f.Detail)
		fmt.Printf("           fix: %s\n\n", f.Fix)
	}
	fmt.Printf("~%s tokens per turn recoverable\n", withCommas(total))
	fmt.Printf("over a 50-turn session that is ~%s tokens\n", withCommas(total*50))
	fmt.Println("\nEstimates use bytes/4, the same approximation Kiro CLI uses for /context.")
}

// ---------------------------------------------------------------- guard

func cmdGuard(args []string) error {
	if len(args) == 0 || args[0] != "install" {
		return errors.New("guard: only `guard install <agent.json>` is supported")
	}
	fs := flag.NewFlagSet("guard install", flag.ExitOnError)
	max := fs.Int("max", 2000, "token-per-turn budget to enforce")
	ttl := fs.Int("ttl", 3600, "hook cache_ttl_seconds")
	dryRun := fs.Bool("dry-run", false, "print the patched config instead of writing")
	if err := fs.Parse(permute(args[1:])); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("guard install: need a path to an agent config")
	}
	configPath := fs.Arg(0)

	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	// The hook re-invokes this binary, so it must be on PATH at session start.
	command := fmt.Sprintf("kirobuff budget %s -max %d -quiet", configPath, *max)

	effectiveTTL := *ttl
	if !guard.SupportsCaching(raw) {
		effectiveTTL = 0
	}

	patched, err := guard.Install(raw, command, effectiveTTL)
	if err != nil {
		if errors.Is(err, guard.ErrAlreadyInstalled) {
			fmt.Printf("Already installed in %s - nothing to do.\n", configPath)
			return nil
		}
		return err
	}

	if *dryRun {
		fmt.Print(string(patched))
		return nil
	}

	// Preserve the original file mode rather than assuming 0644.
	mode := os.FileMode(0o644)
	if fi, statErr := os.Stat(configPath); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(configPath, patched, mode); err != nil {
		return err
	}

	fmt.Printf("Installed agentSpawn guard in %s\n\n", configPath)
	fmt.Printf("  command  %s\n", command)
	if effectiveTTL > 0 {
		fmt.Printf("  cached   %ds\n", effectiveTTL)
	} else if *ttl > 0 {
		fmt.Printf("  cached   no (array-format hooks cannot carry cache_ttl_seconds)\n")
	}
	fmt.Print(`
It runs once per session and writes nothing to stdout, so the model never
sees it and you pay no tokens for it. If the agent goes over budget you get
a warning on stderr at session start.

Two things to check:
  - kirobuff must be on PATH when kiro-cli starts the session
  - top-level JSON keys were re-sorted; the values are unchanged
`)
	return nil
}

// ---------------------------------------------------------------- loop

func cmdLoop(args []string) error {
	if len(args) == 0 || args[0] != "init" {
		return errors.New("loop: only `loop init` is supported")
	}
	fs := flag.NewFlagSet("loop init", flag.ExitOnError)
	wsFlag := fs.String("C", "", "workspace root")
	goal := fs.String("goal", "", "the measurable outcome the loop pursues")
	editable := fs.String("editable", "", "glob the agent may modify (default src/**)")
	maxAttempts := fs.Int("max-attempts", 10, "stop condition")
	metric := fs.String("metric", "", "command printing one number; makes the loop a search")
	direction := fs.String("direction", "lower", "lower or higher is better")
	force := fs.Bool("force", false, "overwrite existing loop files")
	if err := fs.Parse(permute(args[1:])); err != nil {
		return err
	}
	ws, err := resolveWorkspace(*wsFlag)
	if err != nil {
		return err
	}

	tc := loop.DetectToolchain(ws)
	scaffold := loop.New(*goal, *editable, *maxAttempts, tc)
	if *metric != "" {
		scaffold = scaffold.WithMetric(*metric, *direction)
	}

	written, skipped, err := scaffold.Write(ws, *force)
	if err != nil {
		return err
	}

	fmt.Printf("workspace  %s\n", ws)
	fmt.Printf("toolchain  %s\n", tc.Name)
	fmt.Printf("verifier   %s\n", tc.Verify)
	fmt.Printf("editable   %s\n", scaffold.Editable)
	if scaffold.Scored() {
		fmt.Printf("metric     %s (%s is better)\n", *metric, scaffold.Direction)
	} else {
		fmt.Printf("metric     none - this is a gate, not a search\n")
	}
	fmt.Printf("stop after %d attempts\n\n", scaffold.MaxAttempts)

	for _, p := range written {
		fmt.Printf("  created  %s\n", p)
	}
	for _, p := range skipped {
		fmt.Printf("  kept     %s (already exists)\n", p)
	}

	if tc.Name == "unknown" {
		fmt.Print(`
No build system detected, so the verifier is a failing placeholder. Edit
.kiro/loop/verify.sh before running the loop - a verifier that always passes
is worse than no loop, because the agent will grade its own homework.
`)
	}

	fmt.Printf(`
Start it with:  kiro-cli chat --agent %s

The verifier runs on the stop hook, so it fires after every turn whether or
not the agent asks for it. The ledger is injected once at session start by
agentSpawn rather than declared as a resource, which would re-send it every
turn.

Set the goal in .kiro/loop/program.md before the first run. The four-point
test still applies: only build a loop when the task repeats, verification is
automated, you have budget for wasted retries, and the agent has real tools.
`, scaffold.AgentName)
	return nil
}

// ---------------------------------------------------------------- mode

func cmdMode(args []string) error {
	if len(args) == 0 {
		return errors.New("mode: expected list, status, on, off, or explain")
	}
	sub, rest := args[0], args[1:]

	fs := flag.NewFlagSet("mode "+sub, flag.ExitOnError)
	agentPath := fs.String("agent", "",
		"scope the mode to one agent config instead of globally")
	scope := fs.String("scope", "ac", "for spank: ac or all")
	if err := fs.Parse(permute(rest)); err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	l := mode.DefaultLayout(home)

	switch sub {
	case "list":
		return modeList(l, home)
	case "status":
		return modeStatus(l, home)
	case "explain":
		if fs.NArg() < 1 {
			return errors.New("mode explain: need a mode name")
		}
		return modeExplain(fs.Arg(0), *scope)
	case "on", "off":
		if fs.NArg() < 1 {
			return fmt.Errorf("mode %s: need a mode name", sub)
		}
		return modeToggle(l, home, sub, fs.Arg(0), *agentPath)
	}
	return fmt.Errorf("mode: unknown subcommand %q", sub)
}

func modeList(l mode.Layout, home string) error {
	fmt.Printf("Modes compose. Up to %d active at once, because each one is\n", mode.MaxActive)
	fmt.Printf("re-sent on every turn.\n\n")
	for _, name := range mode.Names() {
		m, _ := mode.Get(name)
		marker := " "
		if mode.IsActive(l, name) {
			marker = "*"
		}
		kind := ""
		if m.Kind == mode.System {
			kind = "  (system)"
		}
		fmt.Printf(" %s %-16s %s%s\n", marker, name, m.Summary, kind)
	}
	fmt.Printf("\n* = active globally.  %d of %d slots free.\n", mode.Remaining(l), mode.MaxActive)
	fmt.Print(`
  kirobuff mode on <name>                  every agent
  kirobuff mode on <name> -agent a.json    that agent only
`)
	return nil
}

func modeStatus(l mode.Layout, home string) error {
	active := mode.Active(l)
	fmt.Printf("global   %s\n", orNone(active))
	fmt.Printf("slots    %d of %d free\n\n", mode.Remaining(l), mode.MaxActive)

	st := power.Detect()
	switch {
	case !st.Known:
		fmt.Printf("spank    unknown - %s\n", st.Detail)
	case st.LidSleepOff:
		fmt.Printf("spank    ON - lid close will not sleep this machine\n")
	default:
		fmt.Printf("spank    off - closing the lid suspends the agent (%s)\n", st.Detail)
	}
	return nil
}

func orNone(s []string) string {
	if len(s) == 0 {
		return "(none)"
	}
	return strings.Join(s, ", ")
}

func modeExplain(name, scope string) error {
	m, err := mode.Get(name)
	if err != nil {
		return err
	}
	if m.Kind != mode.System {
		fmt.Printf("%s - %s\n\nTurn it on with: kirobuff mode on %s\n", name, m.Summary, name)
		return nil
	}

	in, err := power.For(power.Current(), power.Scope(scope))
	if err != nil {
		return err
	}

	fmt.Printf("spank mode - %s\n\n", m.Summary)
	fmt.Printf("Platform: %s", in.Platform)
	if in.Scoped {
		fmt.Print("  (scoped to one command, nothing to undo)")
	}
	fmt.Print("\n\nEnable:\n")
	for _, c := range in.Enable {
		fmt.Printf("  %s\n", c)
	}
	fmt.Print("\nRevert:\n")
	for _, c := range in.Disable {
		fmt.Printf("  %s\n", c)
	}
	fmt.Print("\nNotes:\n")
	for _, n := range in.Notes {
		fmt.Printf("  - %s\n", n)
	}
	fmt.Print(`
kirobuff does not run these for you. Disabling lid sleep is a persistent,
system-wide change with thermal and battery consequences, and that is not a
tool's call to make on someone's laptop.

One thing unrelated to power will bite an unattended run: if the agent hits a
tool-approval prompt with the lid shut, it waits forever. Configure trust up
front, and rely on the enforce hook for the calls that actually matter:

  kiro-cli chat --trust-tools=read,grep,glob,code "your task"
`)
	return nil
}

func modeToggle(l mode.Layout, home, action, name, agentPath string) error {
	if agentPath == "" {
		if action == "on" {
			if err := mode.On(l, name); err != nil {
				return err
			}
			fmt.Printf("%s is on for every agent.\n", name)
		} else {
			if err := mode.Off(l, name); err != nil {
				return err
			}
			fmt.Printf("%s is off.\n", name)
		}
		fmt.Printf("active: %s  (%d of %d slots free)\n",
			orNone(mode.Active(l)), mode.Remaining(l), mode.MaxActive)
		fmt.Println("\nSteering loads at session start, so restart the session to pick this up.")
		return nil
	}

	// Per-agent scope: only this agent pays the context cost, which is what
	// makes several specialised agents on one project practical.
	if err := mode.Sync(l); err != nil {
		return err
	}
	raw, err := os.ReadFile(agentPath)
	if err != nil {
		return err
	}

	var patched []byte
	if action == "on" {
		patched, err = mode.AttachToAgent(raw, l, name)
		if err != nil {
			return err
		}
		if patched == nil {
			fmt.Printf("%s is already attached to %s.\n", name, agentPath)
			return nil
		}
	} else {
		patched, err = mode.DetachFromAgent(raw, name)
		if err != nil {
			return err
		}
	}
	if err := mode.WriteAgent(agentPath, patched); err != nil {
		return err
	}

	current, _ := mode.AgentModes(patched)
	fmt.Printf("%s: %s\n", agentPath, orNone(current))
	fmt.Println("\nOnly this agent carries these fragments. Another agent on the same")
	fmt.Println("project can run a different set without paying for these.")
	return nil
}

func modeInstall(args []string) error {
	fs := flag.NewFlagSet("mode install", flag.ExitOnError)
	wsFlag := fs.String("C", "", "workspace root")
	scope := fs.String("scope", "global", "global or workspace")
	shortcut := fs.String("shortcut", "", "override the keyboard shortcut")
	force := fs.Bool("force", false, "overwrite an existing persona")
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("mode install: need a persona name (see `kirobuff mode list`)")
	}

	p, err := persona.Get(fs.Arg(0))
	if err != nil {
		return err
	}
	if *shortcut != "" {
		p.Shortcut = *shortcut
	}

	ws, err := resolveWorkspace(*wsFlag)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir, err := persona.Dir(persona.Scope(*scope), home, ws)
	if err != nil {
		return err
	}

	path, err := p.Install(dir, *force)
	if err != nil {
		if errors.Is(err, persona.ErrExists) {
			fmt.Printf("Already installed at %s\n", display(path, home))
			fmt.Println("Use -force to overwrite, which discards any edits you made to the prompt.")
			return nil
		}
		return err
	}

	fmt.Printf("Installed %s at %s\n\n", p.Name, display(path, home))
	fmt.Printf("  turn on   /agent %s\n", p.Name)
	fmt.Printf("  toggle    %s (press again to switch back)\n", p.Shortcut)
	fmt.Printf("  confirm   /agent lists it with an arrow when active\n\n")
	fmt.Print(`It is off until you switch to it. On switch, its welcomeMessage prints so you
can see the mode is on.

Kiro CLI has no status line, so there is no permanent on-screen badge. The
mode announces itself when you enter it and shows in /agent on demand.

Two things that change when you switch: tool permissions reset to this
agent's own settings, and context files from the previous agent stay loaded
as temporary context. The model does not change - that is deliberate.
`)
	return nil
}

// ---------------------------------------------------------------- tune

func cmdTune(args []string) error {
	fs := flag.NewFlagSet("tune", flag.ExitOnError)
	model := fs.String("model", "claude-opus-4.7", "model ID to set a default for")
	effort := fs.String("effort", tune.Recommended, "low, medium, high, xhigh, max")
	show := fs.Bool("show", false, "print current defaults and exit")
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := tune.SettingsPath(home)

	if *show {
		raw, _ := os.ReadFile(path)
		models := tune.ConfiguredModels(raw)
		if len(models) == 0 {
			fmt.Printf("No per-model effort defaults set in %s\n", display(path, home))
			return nil
		}
		fmt.Printf("%s\n\n", display(path, home))
		for _, m := range models {
			if e, ok := tune.Current(raw, m); ok {
				fmt.Printf("  %-28s %s\n", m, e)
			}
		}
		return nil
	}

	if err := tune.Write(path, *model, *effort); err != nil {
		return err
	}
	fmt.Printf("Set %s effort to %s in %s\n\n", *model, *effort, display(path, home))
	fmt.Printf(`Kiro CLI's built-in default for the Opus family is xhigh, which is more
deliberation than a rename or a test run needs. %s is enough for real work
and noticeably faster.

This is a floor, not a ceiling. Raise it for one session with /effort high
when the task actually warrants it.
`, *effort)
	return nil
}

// ------------------------------------------------------------ guardrails

func cmdGuardrails(args []string) error {
	if len(args) == 0 || args[0] != "install" {
		return errors.New("guardrails: only `guardrails install` is supported")
	}
	fs := flag.NewFlagSet("guardrails install", flag.ExitOnError)
	wsFlag := fs.String("C", "", "workspace root")
	scope := fs.String("scope", "global", "global or workspace")
	force := fs.Bool("force", false, "overwrite a hand-written file")
	if err := fs.Parse(permute(args[1:])); err != nil {
		return err
	}
	ws, err := resolveWorkspace(*wsFlag)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir, err := steering.Dir(steering.Scope(*scope), home, ws)
	if err != nil {
		return err
	}

	path, updated, err := steering.Install(dir, *force)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			fmt.Printf("%s exists and was not written by kirobuff.\n", display(path, home))
			fmt.Println("Left alone. Use -force to replace it.")
			return nil
		}
		return err
	}

	verb := "Installed"
	if updated {
		verb = "Updated"
	}
	fmt.Printf("%s guardrails at %s\n\n", verb, display(path, home))
	fmt.Print(`Always on. Steering files are inherited by every agent by default, so this
applies to agents you create later too - nothing opts in.

It classifies every change before editing: additive and behaviour-preserving
work proceeds without asking, behaviour-changing and subtractive work stops
and asks. Asking about safe work is treated as its own failure.
`)

	for _, p := range steering.Verify(tune.SettingsPath(home)) {
		fmt.Printf("\nWARNING: %s\n", p)
	}
	return nil
}

// ------------------------------------------------------------ statusline

func cmdStatusline(args []string) error {
	if len(args) == 0 {
		return errors.New("statusline: expected `emit <agent.json>` or `install <agent.json>`")
	}
	sub, rest := args[0], args[1:]

	fs := flag.NewFlagSet("statusline "+sub, flag.ExitOnError)
	wsFlag := fs.String("C", "", "workspace root")
	max := fs.Int("max", 0, "budget shown alongside the measured cost")
	if err := fs.Parse(permute(rest)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("statusline: need a path to an agent config")
	}
	configPath := fs.Arg(0)

	ws, err := resolveWorkspace(*wsFlag)
	if err != nil {
		return err
	}

	switch sub {
	case "emit":
		// Called from a hook every turn. Never fail and never write to stdout:
		// stdout is fed to the model on exit 0.
		agent, err := budget.Load(configPath)
		if err != nil {
			return nil
		}
		st := statusline.Status{
			Mode:          agent.Name,
			Workspace:     filepath.Base(ws),
			TokensPerTurn: budget.Total(budget.Analyze(agent, ws)),
			Budget:        *max,
		}
		_ = statusline.WriteTTY(st)
		return nil

	case "install":
		raw, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		command := statusline.HookCommand(configPath)
		if *max > 0 {
			command = fmt.Sprintf("kirobuff statusline emit %s -max %d || true", configPath, *max)
		}
		patched, err := guard.InstallOn(raw, "userPromptSubmit", command, 0)
		if err != nil {
			if errors.Is(err, guard.ErrAlreadyInstalled) {
				fmt.Printf("Already installed in %s - nothing to do.\n", configPath)
				return nil
			}
			return err
		}
		mode := os.FileMode(0o644)
		if fi, statErr := os.Stat(configPath); statErr == nil {
			mode = fi.Mode().Perm()
		}
		if err := os.WriteFile(configPath, patched, mode); err != nil {
			return err
		}
		fmt.Printf("Installed statusline hook in %s\n\n  %s\n\n", configPath, command)
		if statusline.Available() {
			fmt.Println("A controlling terminal is available here, so the title should update.")
		} else {
			fmt.Println("No controlling terminal in this shell, so it cannot be tested from here.")
		}
		fmt.Print(`
It writes an OSC 0 escape sequence straight to /dev/tty, which bypasses the
hook's captured stdout. Nothing is printed to stdout, so it costs no tokens.

Kiro CLI has no statusLine setting, so the terminal window or tab title is
the only surface available. Credits are not shown: account usage is reachable
only through the in-session /usage command.

UNVERIFIED: whether a Kiro CLI hook subprocess inherits a controlling
terminal. Start a session and check your tab title. If it does not update,
remove the userPromptSubmit entry from the config by hand - there is no
uninstall command yet.
`)
		return nil
	}
	return fmt.Errorf("statusline: unknown subcommand %q", sub)
}

// ---------------------------------------------------------------- install

func cmdInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	wsFlag := fs.String("C", "", "workspace root")
	effort := fs.String("effort", tune.Recommended, "effort default for Opus")
	skipTune := fs.Bool("no-tune", false, "leave model effort defaults alone")
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}
	ws, err := resolveWorkspace(*wsFlag)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	fmt.Println("kirobuff install")
	fmt.Println()

	// 1. Guardrails: always on, every agent.
	dir, err := steering.Dir(steering.Global, home, ws)
	if err != nil {
		return err
	}
	if path, updated, err := steering.Install(dir, false); err == nil {
		verb := "installed"
		if updated {
			verb = "updated"
		}
		fmt.Printf("  guardrails   %s  %s\n", verb, display(path, home))
	} else if errors.Is(err, os.ErrExist) {
		fmt.Printf("  guardrails   skipped    %s is hand-written\n", display(path, home))
	} else {
		return err
	}

	// 2. Personas: available, off until switched to.
	pdir, err := persona.Dir(persona.Global, home, ws)
	if err != nil {
		return err
	}
	for name := range persona.Registry() {
		p, _ := persona.Get(name)
		path, err := p.Install(pdir, false)
		switch {
		case err == nil:
			fmt.Printf("  mode         installed  %s (off until /agent %s)\n", display(path, home), name)
		case errors.Is(err, persona.ErrExists):
			fmt.Printf("  mode         exists     %s\n", display(path, home))
		default:
			return err
		}
	}

	// 3. Effort defaults.
	if *skipTune {
		fmt.Println("  effort       skipped    -no-tune")
	} else {
		if err := tune.Write(tune.SettingsPath(home), "claude-opus-4.7", *effort); err != nil {
			return err
		}
		fmt.Printf("  effort       set        claude-opus-4.7 -> %s\n", *effort)
	}

	fmt.Print(`
Done. Nothing else is required.

  guardrails  active in every session, including agents you create later
  cofounder   /agent tech-cofounder, or ctrl+shift+t to toggle
  effort      raise per session with /effort high when a task warrants it

Per-project extras, run inside a repo:

  kirobuff loop init                          verifier, ledger, stop condition
  kirobuff guard install .kiro/agents/x.json  warn when an agent gets expensive
  kirobuff statusline install .kiro/agents/x.json
`)

	for _, p := range steering.Verify(tune.SettingsPath(home)) {
		fmt.Printf("\nWARNING: %s\n", p)
	}
	return nil
}

// ---------------------------------------------------------------- enforce

func cmdEnforce(args []string) error {
	if len(args) > 0 && args[0] == "install" {
		return enforceInstall(args[1:])
	}

	// Hook mode: a preToolUse event arrives on stdin. Exit 2 blocks the call and
	// returns stderr to the model; any other non-zero code only warns the user
	// and lets the call through, so the distinction matters.
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil // fail open: a broken pipe must not block every tool call
	}
	var event enforce.Event
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil // fail open on an unrecognised payload shape
	}

	d := enforce.Evaluate(event)
	if !d.Blocked {
		return nil
	}
	fmt.Fprintf(os.Stderr, "%s [%s]\n", d.Reason, d.Rule)
	os.Exit(2)
	return nil
}

func enforceInstall(args []string) error {
	fs := flag.NewFlagSet("enforce install", flag.ExitOnError)
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("enforce install: need a path to an agent config")
	}
	configPath := fs.Arg(0)

	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	// No matcher: the rules decide which tools they apply to, and a matcher here
	// would silently exempt any tool alias not listed.
	patched, err := guard.InstallOn(raw, "preToolUse", "kirobuff enforce", 0)
	if err != nil {
		if errors.Is(err, guard.ErrAlreadyInstalled) {
			fmt.Printf("Already installed in %s - nothing to do.\n", configPath)
			return nil
		}
		return err
	}
	mode := os.FileMode(0o644)
	if fi, statErr := os.Stat(configPath); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(configPath, patched, mode); err != nil {
		return err
	}

	fmt.Printf("Installed enforcement hook in %s\n\n", configPath)
	fmt.Print(`These are no longer requests. The hook exits 2, which blocks the tool call
and returns the reason to the model:

  no-agent-signoff        git commit -s, or a Signed-off-by written by hand
  no-test-deletion        rm on a test file or test directory
  no-assertion-weakening  an edit that reduces a test's assertion count
  protect-verifier        writes to .kiro/loop/verify.sh, program.md, best
  no-destructive-git      reset --hard, push --force, clean -fd, branch -D

Everything else passes. Unknown tools and unparseable input fail open on
purpose: a hook that blocks a shape it does not recognise would break every
session after a schema change.

Judgment stays in steering. Only rules decidable from the tool input are here.
`)
	return nil
}

// ---------------------------------------------------------------- attest

func cmdAttest(args []string) error {
	fs := flag.NewFlagSet("attest", flag.ExitOnError)
	model := fs.String("model", "", "model ID used (required unless -check)")
	agent := fs.String("agent", "", "agent or harness name")
	tools := fs.String("tools", "", "comma-separated auxiliary tools")
	msgFile := fs.String("f", "", "commit message file to read, and write when -w is set")
	write := fs.Bool("w", false, "write the trailer back into -f")
	check := fs.Bool("check", false, "validate instead of generating")
	asAgent := fs.Bool("as-agent", false,
		"check as an agent: also reject any Signed-off-by trailer")
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}

	if *check {
		if *msgFile == "" {
			return errors.New("attest -check: need -f <commit message file>")
		}
		body, err := os.ReadFile(*msgFile)
		if err != nil {
			return err
		}
		// AgentMayNotSignOff is opt-in. A human's Signed-off-by is required by
		// the DCO, not a violation; only an agent adding one is. Enabling it
		// unconditionally would reject correct patches.
		problems := attest.Validate(string(body), attest.Policy{
			AIAssisted:         true,
			AgentMayNotSignOff: *asAgent,
		})
		if len(problems) == 0 {
			fmt.Println("Compliant with the kernel AI policy: attribution present, no agent sign-off.")
			return nil
		}
		for _, p := range problems {
			fmt.Fprintf(os.Stderr, "[%s] %s\n  fix: %s\n", p.Rule, p.Detail, p.Fix)
		}
		os.Exit(1)
	}

	if *model == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			raw, _ := os.ReadFile(tune.SettingsPath(home))
			if models := tune.ConfiguredModels(raw); len(models) == 1 {
				*model = models[0]
			}
		}
	}
	if *model == "" {
		return errors.New("attest: -model is required (it could not be inferred from cli.json)")
	}

	spec := attest.Spec{Model: *model, Agent: *agent}
	for _, t := range strings.Split(*tools, ",") {
		if t = strings.TrimSpace(t); t != "" {
			spec.Tools = append(spec.Tools, t)
		}
	}
	trailer, err := spec.Trailer()
	if err != nil {
		return err
	}

	if *msgFile == "" {
		fmt.Println(trailer)
		return nil
	}
	body, err := os.ReadFile(*msgFile)
	if err != nil {
		return err
	}
	out := attest.Append(string(body), trailer)
	if !*write {
		fmt.Print(out)
		return nil
	}
	mode := os.FileMode(0o644)
	if fi, statErr := os.Stat(*msgFile); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(*msgFile, []byte(out), mode); err != nil {
		return err
	}
	fmt.Printf("Added to %s:\n  %s\n", *msgFile, trailer)
	return nil
}

// ---------------------------------------------------------------- helpers

func resolveWorkspace(flagValue string) (string, error) {
	if flagValue == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		flagValue = wd
	}
	return filepath.Abs(flagValue)
}

// permute moves flags ahead of positional arguments.
//
// Go's flag package stops parsing at the first non-flag argument, so
// `budget path -max 2000` would silently discard the threshold. Reordering
// makes flag placement irrelevant, matching what users expect from most CLIs.
func permute(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			positional = append(positional, args[i+1:]...)
			return append(flags, positional...)
		case valueFlags[a] && i+1 < len(args):
			flags = append(flags, a, args[i+1])
			i++
		case strings.HasPrefix(a, "-") && a != "-":
			flags = append(flags, a)
		default:
			positional = append(positional, a)
		}
	}
	return append(flags, positional...)
}

// display shortens a path to ~/... for readability.
func display(path, home string) string {
	if home != "" && strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}

// withCommas groups thousands for readability.
func withCommas(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
	}
	for i := lead; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
