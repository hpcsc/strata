//go:build unit

package restack_test

import (
	"errors"
	"testing"

	"github.com/hpcsc/strata/internal/git"
	"github.com/hpcsc/strata/internal/restack"
	"github.com/stretchr/testify/require"
)

func TestAvailable(t *testing.T) {
	t.Run("git 2.44 can sync", func(t *testing.T) {
		require.NoError(t, restack.Available(git.Version{Major: 2, Minor: 44}, nil))
	})

	t.Run("git 2.43 cannot sync, and the error gives the version", func(t *testing.T) {
		err := restack.Available(git.Version{Major: 2, Minor: 43}, nil)

		require.EqualError(t, err, "sync needs git 2.44 or later, and this is git 2.43.0")
	})

	t.Run("a version that strata cannot read stops the sync, and the error gives the reason", func(t *testing.T) {
		err := restack.Available(git.Version{}, errors.New(`"zsh: command not found: git" is not a git version`))

		require.EqualError(t, err, `sync needs git 2.44 or later, and strata cannot read the git version: "zsh: command not found: git" is not a git version`)
	})
}
