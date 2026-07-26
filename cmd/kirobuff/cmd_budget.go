package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/AlleyBo55/KiroBuff/internal/budget"
	"github.com/AlleyBo55/KiroBuff/internal/guard"
)

// Budget and the automatic guard around it.

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
