package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hpcsc/strata/internal/keymap"
)

const header = `# strata reads this file from $XDG_CONFIG_HOME/strata/config.toml, or from
# ~/.config/strata/config.toml when XDG_CONFIG_HOME is not set. The --config
# flag names a different file.
#
# The keys of an action replace its default keys. A key name is the name that
# Bubble Tea gives the key, such as "n", "N", "ctrl+d", "enter" or "shift+tab".
# strata looks for a key in [keys.plan], then [keys.search], then [keys], and
# then in the table of the panel with the focus.

`

var tableNotes = map[keymap.Table]string{
	keymap.Anywhere: "in all panels",
	keymap.InStack:  "in the stack panel",
	keymap.InFiles:  "in the files panel",
	keymap.InDiff:   "in the diff panel",
	keymap.InSearch: "while the panel with the focus has a search",
	keymap.InPlan:   "while the sync plan is open",
}

type line struct {
	setting string
	note    string
}

func (c Config) TOML() string {
	var b strings.Builder
	b.WriteString(header)
	writeLines(&b, []line{
		{"theme = " + strconv.Quote(c.Theme), "the chroma style of the code and the diff"},
		{"split = " + strconv.FormatBool(c.Split), "start with the diff side by side"},
	})
	for _, t := range keymap.Tables() {
		fmt.Fprintf(&b, "\n[%s]  # %s\n", t, tableNotes[t])
		var lines []line
		for _, a := range t.Actions() {
			lines = append(lines, line{a.Name() + " = " + keyValue(c.Keys.Keys(a)), a.About()})
		}
		writeLines(&b, lines)
	}
	return b.String()
}

func writeLines(b *strings.Builder, lines []line) {
	width := 0
	for _, l := range lines {
		width = max(width, len(l.setting))
	}
	for _, l := range lines {
		fmt.Fprintf(b, "%-*s  # %s\n", width, l.setting, l.note)
	}
}

func keyValue(keys []string) string {
	quoted := make([]string, len(keys))
	for i, key := range keys {
		quoted[i] = strconv.Quote(key)
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
