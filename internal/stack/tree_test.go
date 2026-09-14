//go:build unit

package stack_test

import (
	"testing"

	"github.com/hpcsc/strata/internal/stack"
	"github.com/stretchr/testify/require"
)

func TestTree(t *testing.T) {
	t.Run("check delete", func(t *testing.T) {
		tree := func() stack.Tree {
			return stack.Tree{Trunk: "origin/main", Branches: []stack.Branch{
				{Name: "events", Parent: "origin/main"},
				{Name: "handler", Parent: "events", Level: 1},
				{Name: "api", Parent: "origin/main", RebaseWorktree: "/work/api"},
			}}
		}

		t.Run("refuses a branch that another branch sits on, when that branch stays", func(t *testing.T) {
			err := tree().CheckDelete([]string{"events"})

			require.EqualError(t, err, "handler sits on events: mark handler too")
		})

		t.Run("refuses a branch that a rebase uses", func(t *testing.T) {
			err := tree().CheckDelete([]string{"api"})

			require.EqualError(t, err, "a rebase in /work/api uses api: finish or abort the rebase first")
		})
	})
}
