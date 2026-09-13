package keymap

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

var modifiers = []string{"ctrl", "alt", "shift", "meta", "hyper", "super"}

var keyNames = func() []string {
	var names []string
	for _, code := range []rune{tea.KeyEnter, tea.KeyTab, tea.KeyBackspace, tea.KeyEscape, tea.KeySpace} {
		names = append(names, tea.Key{Code: code}.String())
	}
	// Bubble Tea numbers its other special keys from KeyUp to KeyIsoLevel5Shift.
	for code := tea.KeyUp; code <= tea.KeyIsoLevel5Shift; code++ {
		if name := (tea.Key{Code: code}).String(); utf8.RuneCountInString(name) > 1 && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}()

// canonical gives the name that tea.KeyPressMsg.String gives for the key.
func canonical(name string) (string, error) {
	rest, base := "", name
	if i := strings.LastIndex(name[:max(len(name)-1, 0)], "+"); i >= 0 {
		rest, base = name[:i], name[i+1:]
	}
	held := map[string]bool{}
	if rest != "" {
		for _, m := range strings.Split(strings.ToLower(rest), "+") {
			if !slices.Contains(modifiers, m) {
				return "", fmt.Errorf("%q is not a key name", name)
			}
			held[m] = true
		}
	}

	switch lower := strings.ToLower(base); {
	case lower == "escape":
		base = "esc"
	case base == " ":
		base = "space"
	case slices.Contains(keyNames, lower):
		base = lower
	case utf8.RuneCountInString(base) == 1 && unicode.IsPrint([]rune(base)[0]):
		r := []rune(base)[0]
		onlyShift := held["shift"] && len(held) == 1
		switch {
		case onlyShift && !unicode.IsLetter(r):
			return "", fmt.Errorf("%q is not a key name: give the character that shift makes", name)
		case onlyShift:
			return string(unicode.ToUpper(r)), nil
		case len(held) > 0 && unicode.IsUpper(r):
			held["shift"] = true
			base = string(unicode.ToLower(r))
		case held["shift"]:
			base = string(unicode.ToLower(r))
		}
	default:
		return "", fmt.Errorf("%q is not a key name", name)
	}

	var b strings.Builder
	for _, m := range modifiers {
		if held[m] {
			b.WriteString(m + "+")
		}
	}
	return b.String() + base, nil
}
