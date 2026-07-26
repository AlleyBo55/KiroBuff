// Package guard installs the budget check as an agentSpawn hook so it runs
// automatically at the start of every session.
//
// agentSpawn is the correct trigger, and the exit code contract is the reason.
// Kiro CLI adds a hook's STDOUT to the model's context when the hook exits 0,
// and shows STDERR to the user as a warning on any other exit code. So a guard
// that stays silent on STDOUT and writes only to STDERR costs zero tokens: the
// warning reaches the human and never reaches the model.
//
// userPromptSubmit would run every turn and inject its output into context on
// success, which is the exact waste the budget check exists to find.
package guard

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Trigger is the only lifecycle event this package installs into.
const Trigger = "agentSpawn"

// ErrAlreadyInstalled means a guard hook is already present.
var ErrAlreadyInstalled = errors.New("guard hook already installed")

// objectHook is the object-format hook shape.
type objectHook struct {
	Command         string `json:"command"`
	CacheTTLSeconds int    `json:"cache_ttl_seconds,omitempty"`
}

// arrayHook is the flat array-format hook shape.
type arrayHook struct {
	Name    string `json:"name,omitempty"`
	Trigger string `json:"trigger"`
	Action  struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	} `json:"action"`
}

// Install adds an agentSpawn hook running command to an agent config.
//
// The config's existing hook format is preserved: an object stays an object, an
// array stays an array. All other fields pass through verbatim, though top-level
// key order is normalised because Go marshals JSON object keys sorted.
//
// Returns ErrAlreadyInstalled if a hook with the same command is present, so
// repeated installs are safe rather than duplicating.
func Install(raw []byte, command string, cacheTTLSeconds int) ([]byte, error) {
	return InstallOn(raw, Trigger, command, cacheTTLSeconds)
}

// InstallOn adds a hook on an arbitrary trigger. Prefer Install for the budget
// guard; this exists for hooks that genuinely need to run every turn, such as
// the status line, which writes to /dev/tty rather than stdout and so costs
// nothing per turn.
func InstallOn(raw []byte, trigger, command string, cacheTTLSeconds int) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("parse agent config: %w", err)
	}

	existing, hasHooks := top["hooks"]

	// No hooks at all: start with the object format, which is the documented
	// default and the only one carrying cache_ttl_seconds.
	if !hasHooks || len(existing) == 0 {
		return write(top, map[string][]objectHook{
			trigger: {{Command: command, CacheTTLSeconds: cacheTTLSeconds}},
		})
	}

	// Try object format first.
	var asObject map[string][]objectHook
	if err := json.Unmarshal(existing, &asObject); err == nil {
		for _, h := range asObject[trigger] {
			if h.Command == command {
				return nil, ErrAlreadyInstalled
			}
		}
		asObject[trigger] = append(asObject[trigger],
			objectHook{Command: command, CacheTTLSeconds: cacheTTLSeconds})
		return write(top, asObject)
	}

	// Fall back to array format. It has no cache field, so the TTL is dropped
	// rather than silently written somewhere it will be ignored.
	var asArray []arrayHook
	if err := json.Unmarshal(existing, &asArray); err == nil {
		for _, h := range asArray {
			if h.Action.Command == command {
				return nil, ErrAlreadyInstalled
			}
		}
		var h arrayHook
		h.Name = "kirobuff-" + trigger
		h.Trigger = trigger
		h.Action.Type = "command"
		h.Action.Command = command
		return write(top, append(asArray, h))
	}

	return nil, errors.New("hooks field is neither the object nor the array format")
}

// SupportsCaching reports whether the config's hook format can carry
// cache_ttl_seconds. The array format cannot.
func SupportsCaching(raw []byte) bool {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return false
	}
	existing, ok := top["hooks"]
	if !ok || len(existing) == 0 {
		return true // a fresh config gets the object format
	}
	var asObject map[string][]objectHook
	return json.Unmarshal(existing, &asObject) == nil
}

func write(top map[string]json.RawMessage, hooks any) ([]byte, error) {
	encoded, err := json.Marshal(hooks)
	if err != nil {
		return nil, err
	}
	top["hooks"] = encoded

	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
