package main

// Eval: score the guardrails against a labelled corpus.

import (
	"flag"
	"fmt"
	"os"

	"github.com/AlleyBo55/KiroBuff/internal/eval"
)

func cmdEval(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	corpus := fs.String("corpus", "evals/guardrails.jsonl", "path to the corpus")
	minDetect := fs.Float64("min-detection", 0, "fail below this detection rate (percent)")
	maxFP := fs.Float64("max-false-positive", 0, "fail above this false-positive rate (percent)")
	verbose := fs.Bool("v", false, "list every case")
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}

	cases, err := eval.Load(*corpus)
	if err != nil {
		return err
	}
	score, err := eval.Run(cases)
	if err != nil {
		return err
	}

	fmt.Printf("corpus   %s (%d cases)\n\n", *corpus, len(cases))
	fmt.Printf("  detection rate       %5.1f%%   (%d of %d harmful actions caught)\n",
		score.DetectionRate(), score.Caught, score.Harmful)
	fmt.Printf("  false positive rate  %5.1f%%   (%d of %d legitimate actions blocked)\n",
		score.FalsePositiveRate(), score.FalsePositive, score.Legitimate)
	fmt.Printf("  known holes          %5d     recorded and still open\n", score.KnownHoles)

	if len(score.ByRule()) > 0 {
		fmt.Printf("\n  caught by rule\n")
		for _, line := range score.ByRule() {
			fmt.Printf("    %s\n", line)
		}
	}

	// Known holes are printed every run. A hole nobody sees becomes a hole
	// nobody remembers.
	var holes []eval.Result
	for _, r := range score.Results {
		if r.Case.Expect == eval.Missed && r.Actual != eval.Caught {
			holes = append(holes, r)
		}
	}
	if len(holes) > 0 {
		fmt.Printf("\n  open holes, by design\n")
		for _, r := range holes {
			fmt.Printf("    %-44s %s\n", r.Case.Name, r.Case.Note)
		}
	}

	if *verbose {
		fmt.Printf("\n  cases\n")
		for _, r := range score.Results {
			mark := "ok  "
			if !r.Agrees {
				mark = "DIFF"
			}
			fmt.Printf("    %s %-9s %-9s %-46s %s\n",
				mark, r.Case.Label, r.Actual, r.Case.Name, r.RuleHit)
		}
	}

	var failed bool
	for _, r := range score.Results {
		if r.Surprise != "" {
			if !failed {
				fmt.Fprintf(os.Stderr, "\n")
			}
			fmt.Fprintf(os.Stderr, "  CHANGED  %-46s %s\n", r.Case.Name, r.Surprise)
			// A closed hole is good news, but the corpus is now stale and should
			// be updated, so it still needs attention.
			if r.Surprise != "known hole is now closed; update the corpus" {
				failed = true
			}
		}
	}

	if *minDetect > 0 && score.DetectionRate() < *minDetect {
		fmt.Fprintf(os.Stderr, "\n  detection rate %.1f%% is below the %.1f%% floor\n",
			score.DetectionRate(), *minDetect)
		failed = true
	}
	if score.FalsePositiveRate() > *maxFP {
		fmt.Fprintf(os.Stderr, "\n  false positive rate %.1f%% exceeds the %.1f%% ceiling\n",
			score.FalsePositiveRate(), *maxFP)
		failed = true
	}

	fmt.Println()
	if failed {
		os.Exit(1)
	}
	return nil
}
