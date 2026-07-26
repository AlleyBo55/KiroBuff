// Package persona generates opt-in agent personas: modes you switch into
// deliberately rather than run all the time.
//
// An agent is the right primitive here because Kiro CLI already supplies every
// part of a "mode" natively:
//
//	turn on      /agent <name>, or the agent's keyboardShortcut
//	turn off     press the same shortcut again, which toggles back
//	visible      welcomeMessage prints on switch, and /agent marks the
//	             active agent with an arrow
//
// What Kiro CLI does not have is a status line, so there is no way to keep an
// indicator on screen permanently. The mode announces itself when you enter it
// and is visible on demand via /agent; it is not a persistent badge.
package persona

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Persona is a switchable agent mode.
type Persona struct {
	Name           string
	Description    string
	WelcomeMessage string
	Prompt         string
	Shortcut       string
	Tools          []string
	AllowedTools   []string
}

// ErrExists means the persona is already installed.
var ErrExists = errors.New("persona already installed")

// Registry of built-in personas, keyed by the name used on the command line.
func Registry() map[string]Persona {
	return map[string]Persona{
		"tech-cofounder": techCofounder(),
	}
}

// Get returns a built-in persona by name.
func Get(name string) (Persona, error) {
	p, ok := Registry()[name]
	if !ok {
		return Persona{}, fmt.Errorf("unknown persona %q", name)
	}
	return p, nil
}

// techCofounder is critical technical judgment applied to whatever you are
// already doing.
//
// The prompt is written against specific failure modes rather than as a
// personality sketch, because "be critical" produces agreeable hedging while
// "name the tradeoff you are making" produces an answer.
func techCofounder() Persona {
	return Persona{
		Name:        "tech-cofounder",
		Description: "Critical technical judgment: cost, reversibility, and whether to build at all",
		WelcomeMessage: "TECH COFOUNDER MODE is on. I will argue with the premise, not just the " +
			"implementation. Expect questions about cost, reversibility, and whether this " +
			"should be built at all. Press the shortcut again to switch back.",
		Shortcut: "ctrl+shift+t",
		Prompt: "You are the technical co-founder on this project, not an implementer taking " +
			"orders. You carry consequences: you will maintain this, pay for it, and answer " +
			"for it in six months.\n\n" +

			"Before writing code, resolve these in order and say so out loud:\n" +
			"1. What problem does this solve, and for whom? If you cannot name the user, say so.\n" +
			"2. Should this be built at all? Buying, deleting, or doing nothing are real answers. " +
			"Say them when they are right.\n" +
			"3. What is the smallest change that produces signal? Prefer the thing that tells us " +
			"we are wrong in a day over the thing that is complete in a month.\n" +
			"4. Is this a one-way door? Name reversible decisions and move fast on them. Slow " +
			"down and flag anything hard to undo: data models, public interfaces, auth, vendor " +
			"lock-in, anything touching money or user data.\n" +
			"5. What is the real cost? Infrastructure, token spend, maintenance burden, on-call " +
			"surface, and the cost of the person who inherits this.\n\n" +

			"How to disagree: when you think the request is wrong, say so plainly in the first " +
			"sentence, give the reason, then offer the alternative. Do not implement something " +
			"you think is a mistake and mention the concern afterwards. Do not soften a real " +
			"objection into a caveat.\n\n" +

			"Always name the tradeoff you are making rather than presenting a choice as free. " +
			"If you are guessing, say you are guessing. If a claim depends on something you " +
			"have not checked, check it or label it unverified.\n\n" +

			"Distinguish 'this is broken' from 'this is unfashionable'. Working code with an " +
			"unpopular architecture is not a problem. Do not propose rewrites for aesthetics.\n\n" +

			"State what you are deliberately not doing, so scope is a decision rather than a " +
			"drift.",
		Tools:        []string{"read", "write", "shell", "grep", "glob", "code", "subagent", "task"},
		AllowedTools: []string{"read", "grep", "glob", "code"},
	}
}

// Render produces the Kiro CLI agent config.
//
// model is deliberately omitted so the persona inherits whatever model the
// session is already using: switching mode should not silently switch models.
func (p Persona) Render() ([]byte, error) {
	cfg := map[string]any{
		"name":             p.Name,
		"description":      p.Description,
		"prompt":           p.Prompt,
		"welcomeMessage":   p.WelcomeMessage,
		"keyboardShortcut": p.Shortcut,
		"tools":            p.Tools,
		"allowedTools":     p.AllowedTools,
		// Enforcement lives in an agent config because that is the only place
		// hooks can live: built-in agents are not files and cannot carry them.
		// Shipping it here means the persona has teeth without a second step.
		"hooks": map[string]any{
			"preToolUse": []map[string]any{
				{"command": "kirobuff enforce"},
			},
		},
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// Scope selects where the persona is installed.
type Scope string

const (
	Global    Scope = "global"    // ~/.kiro/agents, available in every project
	Workspace Scope = "workspace" // .kiro/agents, shared with the repo
)

// Dir resolves the agents directory for a scope.
func Dir(scope Scope, home, workspace string) (string, error) {
	switch scope {
	case Global:
		kiro := os.Getenv("KIRO_HOME")
		if kiro == "" {
			kiro = filepath.Join(home, ".kiro")
		}
		return filepath.Join(kiro, "agents"), nil
	case Workspace:
		return filepath.Join(workspace, ".kiro", "agents"), nil
	}
	return "", fmt.Errorf("unknown scope %q", scope)
}

// Install writes the persona into dir. Returns the path written.
func (p Persona) Install(dir string, force bool) (string, error) {
	path := filepath.Join(dir, p.Name+".json")
	if _, err := os.Stat(path); err == nil && !force {
		return path, ErrExists
	}
	body, err := p.Render()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
