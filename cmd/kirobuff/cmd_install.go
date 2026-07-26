package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/AlleyBo55/KiroBuff/internal/guard"
	"github.com/AlleyBo55/KiroBuff/internal/persona"
	"github.com/AlleyBo55/KiroBuff/internal/statusline"
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
	var agentPaths []string
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
		agentPaths = append(agentPaths, path)
	}

	// 3. Statusline: HUD in the tab title, installed on each persona.
	for _, agentPath := range agentPaths {
		raw, readErr := os.ReadFile(agentPath)
		if readErr != nil {
			return readErr
		}
		command := statusline.HookCommand(agentPath)
		patched, hookErr := guard.InstallOn(raw, "userPromptSubmit", command, 0)
		switch {
		case hookErr == nil:
			mode := os.FileMode(0o644)
			if fi, statErr := os.Stat(agentPath); statErr == nil {
				mode = fi.Mode().Perm()
			}
			if err := os.WriteFile(agentPath, patched, mode); err != nil {
				return err
			}
			fmt.Printf("  statusline   installed  %s\n", display(agentPath, home))
		case errors.Is(hookErr, guard.ErrAlreadyInstalled):
			fmt.Printf("  statusline   exists     %s\n", display(agentPath, home))
		default:
			return hookErr
		}
	}

	// 4. Effort defaults.
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
  statusline  mode and token cost shown in your tab title
  effort      raise per session with /effort high when a task warrants it

Per-project extras, run inside a repo:

  kirobuff loop init                          verifier, ledger, stop condition
  kirobuff guard install .kiro/agents/x.json  warn when an agent gets expensive
`)

	for _, p := range steering.Verify(tune.SettingsPath(home)) {
		fmt.Printf("\nWARNING: %s\n", p)
	}
	return nil
}
