//go:build unit

package git_test

import (
	"testing"

	"github.com/hpcsc/strata/internal/git"
	"github.com/stretchr/testify/require"
)

func TestVersion(t *testing.T) {
	t.Run("parse", func(t *testing.T) {
		t.Run("reads a release", func(t *testing.T) {
			v, err := git.ParseVersion("git version 2.47.1\n")

			require.NoError(t, err)
			require.Equal(t, git.Version{Major: 2, Minor: 47, Patch: 1}, v)
		})

		t.Run("reads an Apple build", func(t *testing.T) {
			v, err := git.ParseVersion("git version 2.39.5 (Apple Git-154)\n")

			require.NoError(t, err)
			require.Equal(t, git.Version{Major: 2, Minor: 39, Patch: 5}, v)
		})

		t.Run("reads a Windows build", func(t *testing.T) {
			v, err := git.ParseVersion("git version 2.44.0.windows.1\n")

			require.NoError(t, err)
			require.Equal(t, git.Version{Major: 2, Minor: 44, Patch: 0}, v)
		})

		t.Run("reads a release candidate", func(t *testing.T) {
			v, err := git.ParseVersion("git version 2.44.0.rc2\n")

			require.NoError(t, err)
			require.Equal(t, git.Version{Major: 2, Minor: 44, Patch: 0}, v)
		})

		t.Run("rejects text that is not a version", func(t *testing.T) {
			_, err := git.ParseVersion("zsh: command not found: git\n")

			require.ErrorContains(t, err, `"zsh: command not found: git" is not a git version`)
		})

		t.Run("rejects a version with no minor number", func(t *testing.T) {
			_, err := git.ParseVersion("git version 2\n")

			require.ErrorContains(t, err, "is not a git version")
		})
	})

	t.Run("compare", func(t *testing.T) {
		t.Run("2.43.9 is below 2.44", func(t *testing.T) {
			require.False(t, git.Version{Major: 2, Minor: 43, Patch: 9}.AtLeast(git.Version{Major: 2, Minor: 44}))
		})

		t.Run("2.44.0 is at 2.44", func(t *testing.T) {
			require.True(t, git.Version{Major: 2, Minor: 44}.AtLeast(git.Version{Major: 2, Minor: 44}))
		})

		t.Run("3.0.0 is above 2.44", func(t *testing.T) {
			require.True(t, git.Version{Major: 3}.AtLeast(git.Version{Major: 2, Minor: 44}))
		})
	})

	t.Run("print", func(t *testing.T) {
		t.Run("prints the three numbers", func(t *testing.T) {
			require.Equal(t, "2.43.0", git.Version{Major: 2, Minor: 43}.String())
		})
	})
}
