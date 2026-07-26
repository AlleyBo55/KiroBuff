package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AlleyBo55/KiroBuff/internal/budget"
	"github.com/AlleyBo55/KiroBuff/internal/guard"
	"github.com/AlleyBo55/KiroBuff/internal/statusline"
)

// Terminal-title status indicator.

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
