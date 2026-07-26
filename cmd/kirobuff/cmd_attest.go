package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/AlleyBo55/KiroBuff/internal/attest"
	"github.com/AlleyBo55/KiroBuff/internal/tune"
)

// AI-attribution trailers.

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
