package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/AlleyBo55/KiroBuff/internal/steering"
	"github.com/AlleyBo55/KiroBuff/internal/tune"
)

// Always-on guardrails.

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
