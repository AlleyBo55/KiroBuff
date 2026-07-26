// Package enforce blocks agent tool calls that violate change-safety rules,
// rather than asking the model not to make them.
//
// A steering file states a policy. A policy in a prompt is a hope. Kiro CLI's
// preToolUse hook contract makes a subset of it mechanical: exit code 2 blocks
// the tool call and returns stderr to the model, so the model learns why it was
// stopped and can choose differently.
//
// # Extending
//
// Rules are values, not branches in a switch. Implement [Rule] and pass it to
// [Evaluate] alongside [DefaultRules]:
//
//	type noVendorEdits struct{}
//
//	func (noVendorEdits) Name() string { return "no-vendor-edits" }
//	func (noVendorEdits) Check(e enforce.Event) enforce.Decision {
//	    in, ok := e.Write()
//	    if !ok || !strings.HasPrefix(in.Path, "vendor/") {
//	        return enforce.Allow
//	    }
//	    return enforce.Block("no-vendor-edits", "vendor/ is generated; edit go.mod instead")
//	}
//
//	d := enforce.Evaluate(event, append(enforce.DefaultRules(), noVendorEdits{})...)
//
// Only rules decidable from the tool input belong here. "Prefer the smallest
// change" is judgment and belongs in steering; "do not delete a test file" is a
// string match and belongs in a wall.
package enforce

import (
	"encoding/json"
	"strings"
)

// Event is the preToolUse payload delivered on stdin.
type Event struct {
	HookEventName string          `json:"hook_event_name"`
	CWD           string          `json:"cwd"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
}

// WriteInput is the write tool's parameters.
type WriteInput struct {
	Command string `json:"command"`
	Path    string `json:"path"`
	Content string `json:"content"`
	OldStr  string `json:"oldStr"`
	NewStr  string `json:"newStr"`
}

// ShellInput is the shell tool's parameters.
type ShellInput struct {
	Command    string `json:"command"`
	WorkingDir string `json:"working_dir"`
}

// Tool returns the canonical tool name, collapsing the documented aliases.
func (e Event) Tool() string {
	switch strings.ToLower(e.ToolName) {
	case "write", "fs_write", "fswrite":
		return "write"
	case "shell", "execute_bash", "executebash", "execute_cmd", "executecmd":
		return "shell"
	}
	return strings.ToLower(e.ToolName)
}

// Write decodes the event as a write call. The second result is false when this
// is not a write, or when the payload does not parse.
//
// Unparseable input yields false rather than an error on purpose: a hook that
// blocks a shape it does not recognise would break every session after a schema
// change.
func (e Event) Write() (WriteInput, bool) {
	if e.Tool() != "write" {
		return WriteInput{}, false
	}
	var in WriteInput
	if json.Unmarshal(e.ToolInput, &in) != nil {
		return WriteInput{}, false
	}
	return in, true
}

// Shell decodes the event as a shell call.
func (e Event) Shell() (ShellInput, bool) {
	if e.Tool() != "shell" {
		return ShellInput{}, false
	}
	var in ShellInput
	if json.Unmarshal(e.ToolInput, &in) != nil {
		return ShellInput{}, false
	}
	return in, true
}

// Decision is the outcome of evaluating an event.
type Decision struct {
	Blocked bool
	Rule    string
	Reason  string // returned to the model on a block
}

// Allow is the default outcome.
var Allow = Decision{}

// Block builds a refusal. The reason is read by the model, not a human, so it
// should say what to do instead.
func Block(rule, reason string) Decision {
	return Decision{Blocked: true, Rule: rule, Reason: reason}
}

// Rule decides whether one tool call may proceed.
//
// A Rule must be safe to call with any Event, including ones for tools it does
// not care about, and must return Allow in that case.
type Rule interface {
	// Name is the stable identifier reported with a block.
	Name() string
	// Check returns a blocked Decision when the call must not proceed.
	Check(Event) Decision
}

// DefaultRules returns the built-in rule set, in evaluation order.
func DefaultRules() []Rule {
	return []Rule{
		ProtectedPathRule{Paths: DefaultProtectedPaths()},
		SignOffRule{},
		AssertionWeakeningRule{},
		TestDeletionRule{},
		DestructiveGitRule{},
	}
}

// Evaluate applies rules in order and returns the first block.
//
// With no rules supplied it evaluates DefaultRules, so the zero-configuration
// call does the expected thing.
func Evaluate(e Event, rules ...Rule) Decision {
	if len(rules) == 0 {
		rules = DefaultRules()
	}
	for _, r := range rules {
		if d := r.Check(e); d.Blocked {
			// A rule that forgets to set Rule is still attributable.
			if d.Rule == "" {
				d.Rule = r.Name()
			}
			return d
		}
	}
	return Allow
}

// RuleNames lists the names of a rule set, for reporting.
func RuleNames(rules []Rule) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.Name())
	}
	return out
}
