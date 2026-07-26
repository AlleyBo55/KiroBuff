package mode

// Per-agent mode assignment.
//
// Global steering applies to every agent, which is the wrong default when you
// want specialists: an agent reviewing security should not pay context for the
// performance lens, and vice versa. Assigning a mode to one agent adds the
// fragment to that agent's `resources` instead, so each agent carries only the
// lenses it needs.
//
// This is also what makes several agents working the same project viable: agent
// A in paranoid mode and agent B in perf mode, each with a small context, rather
// than one agent carrying six fragments at once.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AgentModes lists the kirobuff modes referenced by an agent's resources.
func AgentModes(agentConfig []byte) ([]string, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(agentConfig, &top); err != nil {
		return nil, fmt.Errorf("parse agent config: %w", err)
	}
	raw, ok := top["resources"]
	if !ok {
		return nil, nil
	}
	var resources []string
	if err := json.Unmarshal(raw, &resources); err != nil {
		// resources present but not a string array: no modes, not a failure.
		return nil, nil //nolint:nilerr
	}
	var out []string
	for _, r := range resources {
		if name := modeNameFromResource(r); name != "" {
			out = append(out, name)
		}
	}
	return out, nil
}

// modeNameFromResource extracts a mode name from a resource URI, or "".
func modeNameFromResource(resource string) string {
	base := filepath.Base(strings.TrimPrefix(resource, "file://"))
	if !strings.HasSuffix(base, ".md") {
		return ""
	}
	// Library fragments are <name>.md under .../kirobuff/modes/, and only count
	// when the path actually points into the library.
	if !strings.Contains(filepath.ToSlash(resource), "/kirobuff/modes/") {
		return ""
	}
	return strings.TrimSuffix(base, ".md")
}

// AttachToAgent adds a mode's fragment to an agent's resources.
//
// The absolute library path is used rather than a relative one, because an
// agent config may be loaded from a different working directory than the one it
// was written in.
func AttachToAgent(agentConfig []byte, l Layout, name string) ([]byte, error) {
	m, err := Get(name)
	if err != nil {
		return nil, err
	}
	if m.Kind == System {
		return nil, fmt.Errorf("%w: %s is not a prompt fragment and cannot be "+
			"attached to an agent", ErrSystem, name)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(agentConfig, &top); err != nil {
		return nil, fmt.Errorf("parse agent config: %w", err)
	}

	var resources []string
	if raw, ok := top["resources"]; ok {
		_ = json.Unmarshal(raw, &resources)
	}

	existing, _ := AgentModes(agentConfig)
	for _, e := range existing {
		if e == name {
			return nil, nil // already attached; nil output signals no change
		}
	}
	if len(existing) >= MaxActive {
		return nil, fmt.Errorf("%w (agent already has: %s)",
			ErrTooMany, strings.Join(existing, ", "))
	}

	abs, err := filepath.Abs(l.libraryPath(name))
	if err != nil {
		return nil, err
	}
	resources = append(resources, "file://"+abs)

	encoded, err := json.Marshal(resources)
	if err != nil {
		return nil, err
	}
	top["resources"] = encoded

	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// DetachFromAgent removes a mode's fragment from an agent's resources.
func DetachFromAgent(agentConfig []byte, name string) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(agentConfig, &top); err != nil {
		return nil, fmt.Errorf("parse agent config: %w", err)
	}
	raw, ok := top["resources"]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotOn, name)
	}
	var resources []string
	if err := json.Unmarshal(raw, &resources); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotOn, name)
	}

	var kept []string
	var removed bool
	for _, r := range resources {
		if modeNameFromResource(r) == name {
			removed = true
			continue
		}
		kept = append(kept, r)
	}
	if !removed {
		return nil, fmt.Errorf("%w: %s", ErrNotOn, name)
	}

	encoded, err := json.Marshal(kept)
	if err != nil {
		return nil, err
	}
	top["resources"] = encoded

	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// WriteAgent persists a patched agent config, preserving its file mode.
func WriteAgent(path string, body []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	return os.WriteFile(path, body, mode)
}
