// Package attest generates and validates AI-attribution trailers for commit
// messages.
//
// The Linux kernel codified a formal AI policy in April 2026 with three
// principles:
//
//  1. AI agents cannot add Signed-off-by. Only a human can certify the
//     Developer Certificate of Origin.
//  2. Contributions using AI tools must carry an Assisted-by trailer
//     identifying the model, agent, and auxiliary tools used.
//  3. The human submitter bears full responsibility for the result.
//
// This package implements 1 and 2 mechanically. It is the only part of
// kirobuff answering to an external mandatory standard rather than to taste,
// so it errs toward the documented format and refuses to invent structure.
//
// FORMAT CAVEAT: one example trailer has been published,
//
//	Assisted-by: Claude:claude-3-opus coccinelle sparse
//
// so the shape here is Vendor:model followed by space-separated tool names.
// Anything beyond that shape is inference, not specification.
package attest

import (
	"fmt"
	"strings"
)

// TrailerKey is the tag the kernel settled on. Co-developed-by and
// Generated-by were both considered and rejected: Assisted-by describes a tool
// rather than implying co-authorship or that the code is second-class.
const TrailerKey = "Assisted-by"

// DCOKey is the trailer only a human may add.
const DCOKey = "Signed-off-by"

// Vendor maps a model ID to the vendor label used in the trailer.
//
// Unknown models return the empty string and an error rather than a guess: a
// wrong vendor label in a legally-significant trailer is worse than refusing
// to emit one.
func Vendor(modelID string) (string, error) {
	id := strings.ToLower(modelID)
	switch {
	case strings.Contains(id, "claude"):
		return "Claude", nil
	case strings.Contains(id, "gpt"):
		return "OpenAI", nil
	case strings.Contains(id, "gemini"):
		return "Google", nil
	case strings.Contains(id, "grok"):
		return "xAI", nil
	case strings.Contains(id, "kimi"):
		return "Moonshot", nil
	case strings.Contains(id, "glm"):
		return "Zhipu", nil
	case strings.Contains(id, "minimax"):
		return "MiniMax", nil
	case strings.Contains(id, "nemotron"):
		return "NVIDIA", nil
	}
	return "", fmt.Errorf("unknown vendor for model %q: add a mapping rather "+
		"than guessing, the trailer is legally significant", modelID)
}

// Spec describes one attribution.
type Spec struct {
	Model string   // model ID, e.g. claude-opus-4.7
	Agent string   // harness/agent, e.g. kiro-cli/loop
	Tools []string // auxiliary tools, e.g. coccinelle, sparse
}

// Trailer renders the Assisted-by line without a trailing newline.
func (s Spec) Trailer() (string, error) {
	if strings.TrimSpace(s.Model) == "" {
		return "", fmt.Errorf("model is required")
	}
	vendor, err := Vendor(s.Model)
	if err != nil {
		return "", err
	}

	parts := []string{vendor + ":" + s.Model}
	if a := sanitizeToken(s.Agent); a != "" {
		parts = append(parts, a)
	}
	for _, t := range s.Tools {
		if tok := sanitizeToken(t); tok != "" {
			parts = append(parts, tok)
		}
	}
	return TrailerKey + ": " + strings.Join(parts, " "), nil
}

// sanitizeToken strips whitespace and anything that would break a trailer
// line. Tokens are space-separated, so an embedded space would be read as an
// extra tool.
func sanitizeToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r', ':':
			return '-'
		}
		return r
	}, s)
}

// Trailer is a parsed trailer line.
type Trailer struct {
	Key   string
	Value string
	Line  int // 0-based index into the message lines
}

// looksLikeTrailer reports whether a line is Key: value with a git-style key.
func looksLikeTrailer(line string) (key, value string, ok bool) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	key = line[:idx]
	for _, r := range key {
		isAlpha := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !isAlpha && r != '-' {
			return "", "", false
		}
	}
	return key, strings.TrimSpace(line[idx+1:]), true
}

// ParseTrailers returns the trailers in the message's final block.
//
// Only the last paragraph is considered, matching git's own interpretation. A
// "Note: see foo" line in the middle of a body is prose, not a trailer.
func ParseTrailers(message string) []Trailer {
	lines := strings.Split(strings.TrimRight(message, "\n"), "\n")

	// Walk back over the final block of non-blank lines.
	end := len(lines)
	start := end
	for start > 0 && strings.TrimSpace(lines[start-1]) != "" {
		start--
	}
	// A message with no blank line at all has no trailer block: the subject
	// line alone cannot be one.
	if start == 0 && end <= 1 {
		return nil
	}

	var out []Trailer
	for i := start; i < end; i++ {
		key, value, ok := looksLikeTrailer(lines[i])
		if !ok {
			// One non-trailer line disqualifies the whole block, as in git.
			return nil
		}
		out = append(out, Trailer{Key: key, Value: value, Line: i})
	}
	return out
}

// Has reports whether a trailer with the given key is present.
func Has(message, key string) bool {
	for _, t := range ParseTrailers(message) {
		if strings.EqualFold(t.Key, key) {
			return true
		}
	}
	return false
}

// Append adds the trailer to the message's trailer block.
//
// Idempotent: an identical trailer is not duplicated. A message whose final
// paragraph is prose gets a new blank line and a fresh trailer block.
func Append(message, trailer string) string {
	trailer = strings.TrimRight(trailer, "\n")
	body := strings.TrimRight(message, "\n")

	for _, t := range ParseTrailers(body) {
		if strings.TrimSpace(t.Key+": "+t.Value) == strings.TrimSpace(trailer) {
			return body + "\n"
		}
	}

	if len(ParseTrailers(body)) > 0 {
		return body + "\n" + trailer + "\n"
	}
	return body + "\n\n" + trailer + "\n"
}

// Problem is a policy violation found in a commit message.
type Problem struct {
	Rule   string
	Detail string
	Fix    string
}

// Policy configures validation.
type Policy struct {
	// AIAssisted is true when the change was produced with AI help. When true,
	// a missing Assisted-by trailer is a violation.
	AIAssisted bool
	// AgentMayNotSignOff enforces principle 1. Set when the caller knows the
	// commit is being authored by an agent rather than a human.
	AgentMayNotSignOff bool
}

// Validate checks a commit message against the policy.
func Validate(message string, p Policy) []Problem {
	var problems []Problem

	if p.AIAssisted && !Has(message, TrailerKey) {
		problems = append(problems, Problem{
			Rule:   "missing-attribution",
			Detail: "the change used AI assistance but carries no " + TrailerKey + " trailer",
			Fix:    "add a trailer naming the model, agent, and auxiliary tools",
		})
	}

	if p.AgentMayNotSignOff && Has(message, DCOKey) {
		problems = append(problems, Problem{
			Rule: "agent-signed-off",
			Detail: DCOKey + " was added by an agent; only a human can certify " +
				"the Developer Certificate of Origin",
			Fix: "remove the trailer and let the human submitter add it",
		})
	}

	// A malformed Assisted-by is worse than none: it looks compliant.
	for _, t := range ParseTrailers(message) {
		if !strings.EqualFold(t.Key, TrailerKey) {
			continue
		}
		if !strings.Contains(t.Value, ":") {
			problems = append(problems, Problem{
				Rule:   "malformed-attribution",
				Detail: fmt.Sprintf("%q is missing the Vendor:model form", t.Value),
				Fix:    "use e.g. Claude:claude-opus-4.7",
			})
		}
	}
	return problems
}
