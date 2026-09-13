//go:build unit

package keymap_test

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hpcsc/strata/internal/keymap"
	"github.com/stretchr/testify/require"
)

func TestKeymap(t *testing.T) {
	t.Run("action", func(t *testing.T) {
		t.Run("a key gives its action only in the table of that action", func(t *testing.T) {
			keys := keymap.Default()

			found := []keymap.Action{keys.Action(keymap.InDiff, "n"), keys.Action(keymap.InStack, "n")}

			require.Equal(t, []keymap.Action{keymap.NextHunk, keymap.None}, found)
		})
	})

	t.Run("set", func(t *testing.T) {
		t.Run("the keys replace the default keys of the action", func(t *testing.T) {
			keys := keymap.Default()

			require.NoError(t, keys.Set(keymap.NextHunk, []string{"ctrl+n", "x"}))

			found := []keymap.Action{keys.Action(keymap.InDiff, "n"), keys.Action(keymap.InDiff, "ctrl+n"), keys.Action(keymap.InDiff, "x")}
			require.Equal(t, []keymap.Action{keymap.None, keymap.NextHunk, keymap.NextHunk}, found)
		})

		t.Run("an empty list leaves the action with no key", func(t *testing.T) {
			keys := keymap.Default()

			require.NoError(t, keys.Set(keymap.PreviousHunk, []string{}))

			require.Empty(t, keys.Keys(keymap.PreviousHunk))
		})

		t.Run("writes each key name the way Bubble Tea names the key press", func(t *testing.T) {
			keys := keymap.Default()

			require.NoError(t, keys.Set(keymap.NextHunk, []string{
				"escape", "Enter", "alt+ctrl+a", "shift+n", "ctrl+N", " ", "+", "ctrl++", "CTRL+x", "?",
			}))

			require.Equal(t, []string{
				"esc", "enter", "ctrl+alt+a", "N", "ctrl+shift+n", "space", "+", "ctrl++", "ctrl+x", "?",
			}, keys.Keys(keymap.NextHunk))
		})

		t.Run("a key name matches the name that Bubble Tea gives its key press", func(t *testing.T) {
			presses := []struct {
				name  string
				press tea.KeyPressMsg
			}{
				{"shift+n", tea.KeyPressMsg{Code: 'n', ShiftedCode: 'N', Text: "N", Mod: tea.ModShift}},
				{"ctrl+N", tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl | tea.ModShift}},
				{"alt+ctrl+a", tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl | tea.ModAlt}},
				{" ", tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}},
				{"escape", tea.KeyPressMsg{Code: tea.KeyEscape}},
				{"shift+tab", tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}},
				{"menu", tea.KeyPressMsg{Code: tea.KeyMenu}},
				{"capslock", tea.KeyPressMsg{Code: tea.KeyCapsLock}},
				{"plus", tea.KeyPressMsg{Code: tea.KeyKpPlus}},
				{"f13", tea.KeyPressMsg{Code: tea.KeyF13}},
			}
			var names, pressed []string
			for _, p := range presses {
				keys := keymap.Default()
				require.NoError(t, keys.Set(keymap.NextHunk, []string{p.name}))
				names = append(names, keys.Keys(keymap.NextHunk)...)
				pressed = append(pressed, p.press.String())
			}

			require.Equal(t, pressed, names)
		})

		t.Run("keeps a key one time when two names give the same key", func(t *testing.T) {
			keys := keymap.Default()

			require.NoError(t, keys.Set(keymap.NextHunk, []string{"shift+n", "N"}))

			require.Equal(t, []string{"N"}, keys.Keys(keymap.NextHunk))
		})

		t.Run("rejects a key name that Bubble Tea does not give", func(t *testing.T) {
			var problems []string
			for _, name := range []string{"ctrl+enterr", "cmd+a", "", "ctrl+", "shift+1"} {
				keys := keymap.Default()
				problems = append(problems, fmt.Sprint(keys.Set(keymap.NextHunk, []string{name})))
			}

			require.Equal(t, []string{
				`"ctrl+enterr" is not a key name`,
				`"cmd+a" is not a key name`,
				`"" is not a key name`,
				`"ctrl+" is not a key name`,
				`"shift+1" is not a key name: give the character that shift makes`,
			}, problems)
		})

		t.Run("rejects ctrl+c, which always quits", func(t *testing.T) {
			keys := keymap.Default()

			err := keys.Set(keymap.Quit, []string{"q", "ctrl+c"})

			require.EqualError(t, err, "ctrl+c always quits, so no action can have it")
		})
	})

	t.Run("check", func(t *testing.T) {
		t.Run("the default keys have no conflict", func(t *testing.T) {
			require.NoError(t, keymap.Default().Check())
		})

		t.Run("a key of two actions in one table is a conflict", func(t *testing.T) {
			keys := keymap.Default()
			require.NoError(t, keys.Set(keymap.NextFile, []string{"n"}))

			require.EqualError(t, keys.Check(), `"n" is a key of keys.diff.next_hunk and of keys.diff.next_file`)
		})

		t.Run("a key of an action in keys and of an action in a panel is a conflict", func(t *testing.T) {
			keys := keymap.Default()
			require.NoError(t, keys.Set(keymap.Quit, []string{"j"}))

			require.EqualError(t, keys.Check(), `"j" is a key of keys.quit and of keys.stack.down`+"\n"+
				`"j" is a key of keys.quit and of keys.files.down`+"\n"+
				`"j" is a key of keys.quit and of keys.diff.down`)
		})

		t.Run("a key of keys.search or keys.plan can also be a key of another table", func(t *testing.T) {
			keys := keymap.Default()
			require.NoError(t, keys.Set(keymap.NextMatch, []string{"j"}))
			require.NoError(t, keys.Set(keymap.ClosePlan, []string{"q"}))

			require.NoError(t, keys.Check())
		})
	})
}
