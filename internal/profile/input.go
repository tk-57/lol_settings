package profile

import "strings"

const unboundKey = "[<Unbound>]"

func bindingSection(section string) bool {
	switch section {
	case "GameEvents", "HUDEvents", "ShopEvents", "WASD":
		return true
	}
	return false
}

// InputValue uses an explicit unbind marker for null or empty key bindings.
// Quickbinds and other non-binding settings keep their original value types.
func InputValue(section string, value any) any {
	if bindingSection(section) && value == nil {
		return unboundKey
	}
	s, ok := value.(string)
	if !ok || !bindingSection(section) {
		return value
	}
	for _, slot := range strings.Split(s, ",") {
		if slot != "" && slot != unboundKey {
			return value
		}
	}
	return unboundKey
}

// SameInputValue treats null, empty and explicitly unbound bindings as equivalent.
// Missing keys, active bindings and binding order remain distinct.
func SameInputValue(section string, a, b any) bool {
	return SameValue(InputValue(section, a), InputValue(section, b))
}

// InputUnbinds releases only changed bindings that currently have a key.
// Releasing them first avoids retaining old assignments during key swaps.
func InputUnbinds(changes, current Settings) Settings {
	out := Settings{}
	for section, entries := range changes {
		if !bindingSection(section) {
			continue
		}
		for key, value := range entries {
			if _, ok := value.(string); !ok {
				continue
			}
			old, ok := current[section][key].(string)
			if !ok || InputValue(section, old) == unboundKey {
				continue
			}
			if out[section] == nil {
				out[section] = map[string]any{}
			}
			out[section][key] = strings.Repeat(unboundKey+",", strings.Count(old, ",")) + unboundKey
		}
	}
	return out
}

// WithChanges builds an independent expected snapshot, retaining untouched keys.
func WithChanges(before, changes Settings) Settings {
	out := Settings{}
	for _, settings := range []Settings{before, changes} {
		for section, entries := range settings {
			if out[section] == nil {
				out[section] = map[string]any{}
			}
			for key, value := range entries {
				out[section][key] = value
			}
		}
	}
	return out
}
