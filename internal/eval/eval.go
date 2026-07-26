// Package eval scores the guardrails against a labelled corpus.
//
// The unit tests answer "did this rule change behaviour". They cannot answer
// "how good is this", because a suite of passing tests reports the same result
// whether it covers three cases or three hundred. That gap was the fair
// criticism: there was no number.
//
// So the corpus is data, not code. Each case carries a label (harmful or
// legitimate) and an expectation (caught, allowed, or missed), and the runner
// reports a detection rate and a false-positive rate. Both can move, and CI can
// hold a floor under them.
//
// Cases expected to be "missed" are deliberate. A corpus that only contains
// wins is marketing. Recording the known holes means a future change that
// closes one shows up as an improvement, and a change that opens a new one shows
// up as a regression.
//
// # What this does not measure
//
// Whether an agent produces better code. That needs A/B runs against real
// models on real tasks, which this cannot do. What it measures is whether the
// mechanism catches what it claims to catch, and how often it gets in the way.
package eval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AlleyBo55/KiroBuff/enforce"
	"github.com/AlleyBo55/KiroBuff/internal/sentinel"
)

// Label marks whether a case is something that should be stopped.
type Label string

const (
	// Harmful actions should be caught.
	Harmful Label = "harmful"
	// Legitimate actions must be allowed.
	Legitimate Label = "legitimate"
)

// Expectation is the recorded outcome for a case.
type Expectation string

const (
	// Caught means the guardrails stop or report the action.
	Caught Expectation = "caught"
	// Allowed means the action proceeds untouched.
	Allowed Expectation = "allowed"
	// Missed records a known hole: harmful, and not caught.
	Missed Expectation = "missed"
)

// Mutation is a repository change, used by sentinel-layer cases.
type Mutation struct {
	Op   string `json:"op"`
	Path string `json:"path"`
}

// Case is one scenario.
type Case struct {
	Name     string          `json:"name"`
	Label    Label           `json:"label"`
	Layer    string          `json:"layer"` // "enforce" or "sentinel"
	Tool     string          `json:"tool"`
	Input    json.RawMessage `json:"input"`
	Mutation *Mutation       `json:"mutation"`
	Expect   Expectation     `json:"expect"`
	Rule     string          `json:"rule"`
	Note     string          `json:"note"`
}

// Result is one case's outcome.
type Result struct {
	Case     Case
	Actual   Expectation
	RuleHit  string
	Agrees   bool // actual matched the recorded expectation
	Surprise string
}

// Score is the aggregate.
type Score struct {
	Results []Result

	Harmful       int
	Caught        int
	KnownHoles    int // harmful, recorded as missed, still missed
	NewHoles      int // harmful, expected caught, not caught
	Legitimate    int
	FalsePositive int
	Fixed         int // recorded as missed but now caught
}

// DetectionRate is the fraction of harmful cases the guardrails catch.
func (s Score) DetectionRate() float64 {
	if s.Harmful == 0 {
		return 0
	}
	return float64(s.Caught) / float64(s.Harmful) * 100
}

// FalsePositiveRate is the fraction of legitimate cases wrongly stopped.
func (s Score) FalsePositiveRate() float64 {
	if s.Legitimate == 0 {
		return 0
	}
	return float64(s.FalsePositive) / float64(s.Legitimate) * 100
}

// Regressed reports whether anything got worse: a new hole, or a legitimate
// action newly blocked.
func (s Score) Regressed() bool { return s.NewHoles > 0 || s.FalsePositive > 0 }

// Load reads a JSONL corpus, skipping blank lines and # comments.
func Load(path string) ([]Case, error) {
	f, err := os.Open(path) //nolint:gosec // the corpus path is a CLI argument
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var cases []Case
	scanner := bufio.NewScanner(f)
	// Corpus lines carry embedded JSON and can exceed the default 64KB.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		var c Case
		if err := json.Unmarshal([]byte(text), &c); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", filepath.Base(path), line, err)
		}
		if err := c.validate(); err != nil {
			return nil, fmt.Errorf("%s line %d (%s): %w", filepath.Base(path), line, c.Name, err)
		}
		cases = append(cases, c)
	}
	return cases, scanner.Err()
}

func (c Case) validate() error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if c.Label != Harmful && c.Label != Legitimate {
		return fmt.Errorf("label must be harmful or legitimate, got %q", c.Label)
	}
	switch c.Expect {
	case Caught, Allowed, Missed:
	default:
		return fmt.Errorf("expect must be caught, allowed or missed, got %q", c.Expect)
	}
	if c.Label == Legitimate && c.Expect != Allowed {
		return fmt.Errorf("a legitimate case must expect allowed")
	}
	if c.Expect == Missed && c.Note == "" {
		return fmt.Errorf("a known hole must carry a note explaining it")
	}
	switch c.Layer {
	case "enforce":
		if c.Tool == "" {
			return fmt.Errorf("enforce cases need a tool")
		}
	case "sentinel":
		if c.Mutation == nil {
			return fmt.Errorf("sentinel cases need a mutation")
		}
	default:
		return fmt.Errorf("layer must be enforce or sentinel, got %q", c.Layer)
	}
	return nil
}

// Run scores every case.
func Run(cases []Case) (Score, error) {
	var s Score
	for _, c := range cases {
		var actual Expectation
		var rule string
		var err error

		switch c.Layer {
		case "enforce":
			actual, rule = runEnforce(c)
		case "sentinel":
			actual, err = runSentinel(c)
			if err != nil {
				return s, fmt.Errorf("%s: %w", c.Name, err)
			}
		}

		r := Result{Case: c, Actual: actual, RuleHit: rule}
		r.Agrees = agrees(c.Expect, actual)

		switch c.Label {
		case Harmful:
			s.Harmful++
			if actual == Caught {
				s.Caught++
				if c.Expect == Missed {
					s.Fixed++
					r.Surprise = "known hole is now closed; update the corpus"
				}
			} else {
				if c.Expect == Missed {
					s.KnownHoles++
				} else {
					s.NewHoles++
					r.Surprise = "expected to be caught, was not"
				}
			}
		case Legitimate:
			s.Legitimate++
			if actual == Caught {
				s.FalsePositive++
				r.Surprise = "legitimate action was blocked"
			}
		}
		s.Results = append(s.Results, r)
	}
	return s, nil
}

func agrees(want, got Expectation) bool {
	if want == Missed {
		return got == Allowed
	}
	return want == got
}

func runEnforce(c Case) (Expectation, string) {
	d := enforce.Evaluate(enforce.Event{ToolName: c.Tool, ToolInput: c.Input})
	if d.Blocked {
		return Caught, d.Rule
	}
	return Allowed, ""
}

// runSentinel applies the mutation to a throwaway repository and reports whether
// the coverage check noticed.
func runSentinel(c Case) (Expectation, error) {
	dir, err := os.MkdirTemp("", "kirobuff-eval-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := seed(dir); err != nil {
		return "", err
	}
	if _, err := sentinel.Check(dir); err != nil { // baseline
		return "", err
	}
	if err := mutate(dir, *c.Mutation); err != nil {
		return "", err
	}
	v, err := sentinel.Check(dir)
	if err != nil {
		return "", err
	}
	if v.Regressed {
		return Caught, nil
	}
	return Allowed, nil
}

// seed builds a small repository with a known test surface.
func seed(dir string) error {
	files := map[string]string{
		"internal/a_test.go": "package internal\nfunc TestA(t *testing.T){ t.Errorf(\"x\"); t.Fatal(\"y\") }\n",
		"internal/b_test.go": "package internal\nfunc TestB(t *testing.T){ assert.Equal(1,2) }\n",
		"internal/prod.go":   "package internal\nfunc Prod() {}\n",
		"coverage.out":       "mode: set\n",
	}
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func mutate(dir string, m Mutation) error {
	target := filepath.Join(dir, m.Path)
	switch m.Op {
	case "delete":
		return os.Remove(target)
	case "delete-dir":
		return os.RemoveAll(target)
	case "move-out":
		outside, err := os.MkdirTemp("", "kirobuff-eval-out-*")
		if err != nil {
			return err
		}
		return os.Rename(target, filepath.Join(outside, filepath.Base(target)))
	case "rename-inside":
		return os.Rename(target, filepath.Join(filepath.Dir(target), "renamed_test.go"))
	case "truncate":
		return os.WriteFile(target, []byte("package internal\n"), 0o644)
	case "strip-assertions":
		return os.WriteFile(target, []byte("package internal\nfunc TestA(t *testing.T){}\n"), 0o644)
	case "add-test":
		return os.WriteFile(target, []byte("package internal\nfunc TestC(t *testing.T){ t.Errorf(\"z\") }\n"), 0o644)
	case "add-assertion":
		return os.WriteFile(target,
			[]byte("package internal\nfunc TestA(t *testing.T){ t.Errorf(\"x\"); t.Fatal(\"y\"); t.Errorf(\"z\") }\n"), 0o644)
	case "edit-prod":
		return os.WriteFile(target, []byte("package internal\nfunc Prod() int { return 1 }\n"), 0o644)
	}
	return fmt.Errorf("unknown mutation op %q", m.Op)
}

// ByRule counts caught cases per rule, for the report.
func (s Score) ByRule() []string {
	counts := map[string]int{}
	for _, r := range s.Results {
		if r.Actual == Caught && r.RuleHit != "" {
			counts[r.RuleHit]++
		}
	}
	out := make([]string, 0, len(counts))
	for rule, n := range counts {
		out = append(out, fmt.Sprintf("%-24s %d", rule, n))
	}
	sort.Strings(out)
	return out
}
