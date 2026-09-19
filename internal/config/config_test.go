//go:build unit

package config_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/hpcsc/strata/internal/config"
	"github.com/hpcsc/strata/internal/keymap"
	"github.com/stretchr/testify/require"
)

func TestConfig(t *testing.T) {
	t.Run("parse", func(t *testing.T) {
		t.Run("an empty file gives the defaults", func(t *testing.T) {
			c, err := config.Parse("")

			require.NoError(t, err)
			require.Equal(t, config.Default(), c)
		})

		t.Run("auto refresh is off when the file does not turn it on", func(t *testing.T) {
			c, err := config.Parse("theme = \"github\"\n")

			require.NoError(t, err)
			require.False(t, c.AutoRefresh)
		})

		t.Run("takes the options from the file", func(t *testing.T) {
			c, err := config.Parse("theme = \"github\"\nsplit = false\nwhole_file = true\nauto_refresh = true\n")

			want := config.Default()
			want.Theme, want.Split, want.WholeFile, want.AutoRefresh = "github", false, true, true
			require.NoError(t, err)
			require.Equal(t, want, c)
		})

		t.Run("the keys of an action replace its default keys", func(t *testing.T) {
			c, err := config.Parse(`
[keys]
quit = ["Q", "ctrl+q"]

[keys.diff]
next_hunk = ["n", "ctrl+n"]
next_file = "L"
previous_file = "H"
`)

			require.NoError(t, err)
			keys := map[keymap.Action][]string{}
			for _, a := range []keymap.Action{keymap.Quit, keymap.NextHunk, keymap.PreviousHunk, keymap.NextFile, keymap.PreviousFile} {
				keys[a] = c.Keys.Keys(a)
			}
			require.Equal(t, map[keymap.Action][]string{
				keymap.Quit:         {"Q", "ctrl+q"},
				keymap.NextHunk:     {"n", "ctrl+n"},
				keymap.PreviousHunk: {"p"},
				keymap.NextFile:     {"L"},
				keymap.PreviousFile: {"H"},
			}, keys)
		})

		t.Run("an empty list leaves the action with no key", func(t *testing.T) {
			c, err := config.Parse("[keys.diff]\nprevious_hunk = []\n")

			require.NoError(t, err)
			require.Empty(t, c.Keys.Keys(keymap.PreviousHunk))
		})

		t.Run("reads back the file that TOML writes", func(t *testing.T) {
			want := config.Default()
			want.Theme, want.Split, want.AutoRefresh = "github", false, true
			for a, keys := range map[keymap.Action][]string{
				keymap.Quit:         {"ctrl+q"},
				keymap.StackTop:     {"ctrl+g"},
				keymap.FilesFold:    {"f"},
				keymap.NextHunk:     {"ctrl+n", "n"},
				keymap.PreviousHunk: nil,
				keymap.ClearSearch:  {"ctrl+l"},
				keymap.ClosePlan:    {"x"},
			} {
				require.NoError(t, want.Keys.Set(a, keys))
			}

			c, err := config.Parse(want.TOML())

			require.NoError(t, err)
			require.Equal(t, want, c)
		})
	})

	t.Run("errors", func(t *testing.T) {
		t.Run("a file that is not TOML gives the line of the error", func(t *testing.T) {
			_, err := config.Parse("theme = \"github\"\nsplit = \n")

			require.ErrorContains(t, err, "toml: line 2")
		})

		t.Run("names each option, table and action that strata does not know", func(t *testing.T) {
			_, err := config.Parse(`
themee = "github"
kyes.quit = "Q"

[keys]
quitt = "q"

[keys.dif]
next_hunk = "n"

[keys.diff]
nxt_hunk = "n"

[key.diff]
next_hunk = "n"

[colours]
added_line = "#213528"
`)

			require.EqualError(t, err, "unknown option themee\n"+
				"unknown option kyes\n"+
				"unknown option key\n"+
				"unknown option colours\n"+
				"unknown table keys.dif\n"+
				"unknown action keys.diff.nxt_hunk\n"+
				"unknown action keys.quitt")
		})

		t.Run("a key name that strata does not know names its action", func(t *testing.T) {
			_, err := config.Parse("[keys.diff]\nnext_hunk = \"ctrl+enterr\"\n")

			require.EqualError(t, err, `keys.diff.next_hunk: "ctrl+enterr" is not a key name`)
		})

		t.Run("a value that is not a key name or a list of key names names its action", func(t *testing.T) {
			_, err := config.Parse("[keys]\nquit = 5\n")

			require.EqualError(t, err, "keys.quit: want a key name or a list of key names")
		})

		t.Run("a list with a value that is not a key name names its action", func(t *testing.T) {
			_, err := config.Parse("[keys]\nquit = [\"q\", 5]\n")

			require.EqualError(t, err, "keys.quit: want a key name or a list of key names")
		})

		t.Run("keys that is not a table is an error", func(t *testing.T) {
			_, err := config.Parse("keys = 5\n")

			require.EqualError(t, err, "keys: want a table")
		})

		t.Run("a key name that strata does not know hides the conflicts that its default keys give", func(t *testing.T) {
			_, err := config.Parse("[keys.diff]\nnext_file = \"n\"\nnext_hunk = \"ctrl+enterr\"\n")

			require.EqualError(t, err, `keys.diff.next_hunk: "ctrl+enterr" is not a key name`)
		})

		t.Run("a key that another action of the table has by default names the two actions", func(t *testing.T) {
			_, err := config.Parse("[keys.diff]\nnext_file = \"n\"\n")

			require.EqualError(t, err, `"n" is a key of keys.diff.next_hunk and of keys.diff.next_file`)
		})
	})

	t.Run("load", func(t *testing.T) {
		write := func(t *testing.T, text string) string {
			t.Helper()
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
			return path
		}

		t.Run("reads the options from the file", func(t *testing.T) {
			c, err := config.Load(write(t, "theme = \"github\"\n"))

			require.NoError(t, err)
			require.Equal(t, "github", c.Theme)
		})

		t.Run("a missing file gives an error that fs.ErrNotExist matches", func(t *testing.T) {
			_, err := config.Load(filepath.Join(t.TempDir(), "config.toml"))

			require.ErrorIs(t, err, fs.ErrNotExist)
		})

		t.Run("an error starts with the path of the file", func(t *testing.T) {
			path := write(t, "themee = \"github\"\n")

			_, err := config.Load(path)

			require.EqualError(t, err, path+": unknown option themee")
		})

		t.Run("puts each of two or more errors on its own line under the path", func(t *testing.T) {
			path := write(t, "themee = \"github\"\nsplitt = false\n")

			_, err := config.Load(path)

			require.EqualError(t, err, path+":\n  unknown option themee\n  unknown option splitt")
		})
	})

	t.Run("path", func(t *testing.T) {
		env := func(vars map[string]string) func(string) string {
			return func(name string) string { return vars[name] }
		}

		t.Run("is in XDG_CONFIG_HOME when it is set", func(t *testing.T) {
			path := config.Path(env(map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/home/ann"}))

			require.Equal(t, "/xdg/strata/config.toml", path)
		})

		t.Run("is in ~/.config when XDG_CONFIG_HOME is not set", func(t *testing.T) {
			path := config.Path(env(map[string]string{"HOME": "/home/ann"}))

			require.Equal(t, "/home/ann/.config/strata/config.toml", path)
		})

		t.Run("is in ~/.config when XDG_CONFIG_HOME is not an absolute path", func(t *testing.T) {
			path := config.Path(env(map[string]string{"XDG_CONFIG_HOME": "xdg", "HOME": "/home/ann"}))

			require.Equal(t, "/home/ann/.config/strata/config.toml", path)
		})
	})
}
