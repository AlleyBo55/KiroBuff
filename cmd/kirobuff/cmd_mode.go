package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/AlleyBo55/KiroBuff/internal/mode"
	"github.com/AlleyBo55/KiroBuff/internal/persona"
	"github.com/AlleyBo55/KiroBuff/internal/power"
)

// Modes: composable lenses, and the agent that carries enforcement.

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
