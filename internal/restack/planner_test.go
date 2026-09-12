//go:build integration

package restack_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hpcsc/strata/internal/git"
	"github.com/hpcsc/strata/internal/gittest"
	"github.com/hpcsc/strata/internal/restack"
	"github.com/stretchr/testify/require"
)

func TestPlanner(t *testing.T) {
	plan := func(t *testing.T, repo *gittest.Repo, trunk string) restack.Plan {
		t.Helper()
		g := git.New(repo.Dir)
		p, err := restack.NewPlanner(g, g.Fetch, trunk, []string{"refs/heads/"}).Plan(context.Background())
		require.NoError(t, err)
		return p
	}
	outcomes := func(p restack.Plan) []string {
		var out []string
		for _, b := range p.Tree.Branches {
			out = append(out, b.Name+": "+p.Outcomes[b.Name].Text())
		}
		return out
	}
	heads := func(t *testing.T, repo *gittest.Repo) string {
		t.Helper()
		return repo.Git("for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/")
	}

	t.Run("plan", func(t *testing.T) {
		t.Run("a branch on the trunk moves onto the new trunk after the remote trunk gets a commit", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

			p := plan(t, repo, "origin/main")

			require.Equal(t, []string{"events: moves onto origin/main"}, outcomes(p))
		})

		t.Run("a branch on the tip of its parent is up to date when nothing moves", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")

			p := plan(t, repo, "origin/main")

			require.Equal(t, []string{"events: up to date", "handler: up to date"}, outcomes(p))
		})

		t.Run("a child behind its parent moves onto its parent while the trunk has no new commit", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.Switch("events")
			repo.Commit("events.go", "package orders\n\ntype Placed struct{}\n", "Add Placed")

			p := plan(t, repo, "origin/main")

			require.Equal(t, []string{"events: up to date", "handler: moves onto events"}, outcomes(p))
		})

		t.Run("the children of a branch that moves move too", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

			p := plan(t, repo, "origin/main")

			require.Equal(t, []string{"events: moves onto origin/main", "handler: moves onto events"}, outcomes(p))
		})

		t.Run("the fetch brings the commits of the remote trunk into the trunk", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all night\n", "Open all night")

			p := plan(t, repo, "origin/main")

			require.Equal(t, 2, p.NewCommits)
			require.Equal(t, "Open all night", strings.TrimSpace(repo.Git("log", "-1", "--format=%s", "origin/main")))
		})

		t.Run("a local trunk fetches nothing", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

			p := plan(t, repo, "main")

			require.Equal(t, 0, p.NewCommits)
			require.Equal(t, "Start the shop", strings.TrimSpace(repo.Git("log", "-1", "--format=%s", "origin/main")))
			require.Equal(t, []string{"events: up to date"}, outcomes(p))
		})

		t.Run("moves no branch", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
			before := heads(t, repo)

			p := plan(t, repo, "origin/main")
			require.Equal(t, "moves onto origin/main", p.Outcomes["events"].Text())

			require.Equal(t, before, heads(t, repo))
		})
	})
}
