package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/AlleyBo55/KiroBuff/internal/persona"
	"github.com/AlleyBo55/KiroBuff/internal/steering"
	"github.com/AlleyBo55/KiroBuff/internal/tune"
)

// The one-shot installer.

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
