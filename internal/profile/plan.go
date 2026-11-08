package profile

import (
	"encoding/json"
	"math/big"
	"sort"
)

func compatible(a, b any) bool {
	switch a.(type) {
	case nil:
		// Preserve null in snapshots without guessing a type for PATCH requests.
		return b == nil
	case bool:
		_, ok := b.(bool)
		return ok
	case string:
		_, ok := b.(string)
		return ok
	case json.Number:
		_, ok := b.(json.Number)
		return ok
	}
	return false
}

// SameValue compares validated scalar values, treating 10 and 10.0 as equal.
func SameValue(a, b any) bool {
	// LCU may serialize the same number as 10 or 10.0.
	if x, ok := a.(json.Number); ok {
		y, ok := b.(json.Number)
		if !ok || !validNumber(x) || !validNumber(y) {
			return false
		}
		xr, xOK := new(big.Rat).SetString(string(x))
		yr, yOK := new(big.Rat).SetString(string(y))
		return xOK && yOK && xr.Cmp(yr) == 0
	}
	return a == b
}

// Plan contains only changed settings supported by the current client.
type Plan struct {
	Game, Input Settings
	Changed     int
	Skipped     []string
}

// MakePlan excludes missing keys and incompatible value types.
func MakePlan(source, current *Profile) *Plan {
	p := &Plan{}
	merge := func(label string, src, dst Settings, input bool) Settings {
		out := Settings{}
		for section, entries := range src {
			for key, v := range entries {
				old, ok := dst[section][key]
				if input {
					v = InputValue(section, v)
					old = InputValue(section, old)
				}
				if !ok || !compatible(v, old) {
					p.Skipped = append(p.Skipped, label+" / "+section+"."+key)
					continue
				}
				if SameValue(v, old) {
					continue
				}
				if out[section] == nil {
					out[section] = map[string]any{}
				}
				out[section][key] = v
				p.Changed++
			}
		}
		return out
	}
	p.Game = merge("ゲーム", source.Game, current.Game, false)
	p.Input = merge("キー", source.Input, current.Input, true)
	sort.Strings(p.Skipped)
	return p
}

// Matches reports whether actual contains every wanted setting with the same value.
func Matches(wanted, actual Settings) bool {
	for section, entries := range wanted {
		for key, v := range entries {
			a, ok := actual[section][key]
			if !ok || !compatible(v, a) || !SameValue(v, a) {
				return false
			}
		}
	}
	return true
}
