// Package tune writes per-model reasoning-effort defaults into Kiro CLI's
// settings file, so a model stops spending more thought than a task deserves.
//
// Kiro CLI ships a built-in effort default per model, and for the Opus family
// that default is xhigh. On mechanical work — renames, small fixes, running
// tests — xhigh buys nothing and costs both latency and credits. Setting a
// lower default and escalating deliberately with /effort is faster and
// cheaper for the same result.
//
// The JSON shape is model-specific and unforgiving: Claude models read
// output_config.effort, GPT models read reasoning.effort. A value written at
// the wrong path is silently ignored at session bootstrap, which looks exactly
// like the setting having no effect.
package tune

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SettingsKey is where per-model defaults live in cli.json.
const SettingsKey = "chat.modelDefaults"

// Family groups models by the request schema they use.
type Family string

const (
	Claude  Family = "claude"
	GPT     Family = "gpt"
	Unknown Family = "unknown"
)

// DetectFamily classifies a model ID.
func DetectFamily(modelID string) Family {
	id := strings.ToLower(modelID)
	switch {
	case strings.Contains(id, "claude"):
		return Claude
	case strings.Contains(id, "gpt"):
		return GPT
	}
	return Unknown
}

// effortPath returns the nested JSON path a family reads effort from.
func effortPath(f Family) ([]string, error) {
	switch f {
	case Claude:
		return []string{"output_config", "effort"}, nil
	case GPT:
		return []string{"reasoning", "effort"}, nil
	}
	return nil, fmt.Errorf("no known effort path for this model; " +
		"not every model exposes an effort field")
}

// allowedEfforts per family. max is Claude-only.
func allowedEfforts(f Family) []string {
	switch f {
	case Claude:
		return []string{"low", "medium", "high", "xhigh", "max"}
	case GPT:
		return []string{"low", "medium", "high", "xhigh"}
	}
	return nil
}

// ValidateEffort checks a level against what the family accepts.
func ValidateEffort(f Family, effort string) error {
	allowed := allowedEfforts(f)
	for _, a := range allowed {
		if a == effort {
			return nil
		}
	}
	if len(allowed) == 0 {
		return fmt.Errorf("model family %q does not expose an effort field", f)
	}
	return fmt.Errorf("effort %q is not valid for %s; use one of: %s",
		effort, f, strings.Join(allowed, ", "))
}

// Recommended is the default kirobuff applies: enough reasoning for real work,
// not enough to deliberate over a rename. Escalate per-task with /effort.
const Recommended = "medium"

// SettingsPath resolves cli.json, honouring KIRO_HOME.
func SettingsPath(home string) string {
	kiro := os.Getenv("KIRO_HOME")
	if kiro == "" {
		kiro = filepath.Join(home, ".kiro")
	}
	return filepath.Join(kiro, "settings", "cli.json")
}

// Apply sets the effort default for modelID inside raw cli.json content.
//
// Every unrelated setting is preserved. An empty or absent file is treated as
// an empty object rather than an error, so first-time use works.
func Apply(raw []byte, modelID, effort string) ([]byte, error) {
	family := DetectFamily(modelID)
	path, err := effortPath(family)
	if err != nil {
		return nil, err
	}
	if err := ValidateEffort(family, effort); err != nil {
		return nil, err
	}

	settings := map[string]any{}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return nil, fmt.Errorf("parse cli.json: %w", err)
		}
	}

	defaults, _ := settings[SettingsKey].(map[string]any)
	if defaults == nil {
		defaults = map[string]any{}
	}
	model, _ := defaults[modelID].(map[string]any)
	if model == nil {
		model = map[string]any{}
	}

	// Walk the nested path, creating objects as needed, without disturbing
	// sibling keys at any level.
	cursor := model
	for _, key := range path[:len(path)-1] {
		next, _ := cursor[key].(map[string]any)
		if next == nil {
			next = map[string]any{}
		}
		cursor[key] = next
		cursor = next
	}
	cursor[path[len(path)-1]] = effort

	defaults[modelID] = model
	settings[SettingsKey] = defaults

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// Current reads back the effort configured for a model, if any.
func Current(raw []byte, modelID string) (string, bool) {
	var settings map[string]any
	if json.Unmarshal(raw, &settings) != nil {
		return "", false
	}
	defaults, ok := settings[SettingsKey].(map[string]any)
	if !ok {
		return "", false
	}
	model, ok := defaults[modelID].(map[string]any)
	if !ok {
		return "", false
	}
	path, err := effortPath(DetectFamily(modelID))
	if err != nil {
		return "", false
	}
	cursor := model
	for _, key := range path[:len(path)-1] {
		next, ok := cursor[key].(map[string]any)
		if !ok {
			return "", false
		}
		cursor = next
	}
	v, ok := cursor[path[len(path)-1]].(string)
	return v, ok
}

// ConfiguredModels lists models that have an effort default set.
func ConfiguredModels(raw []byte) []string {
	var settings map[string]any
	if json.Unmarshal(raw, &settings) != nil {
		return nil
	}
	defaults, ok := settings[SettingsKey].(map[string]any)
	if !ok {
		return nil
	}
	var out []string
	for k := range defaults {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Write applies the change to the settings file on disk, creating parent
// directories if needed.
func Write(path, modelID, effort string) error {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	patched, err := Apply(raw, modelID, effort)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, patched, 0o644)
}
