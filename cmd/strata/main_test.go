//go:build unit

package main

import (
	"context"
	"testing"

	"github.com/hpcsc/strata/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestCommand(t *testing.T) {
	themeWith := func(t *testing.T, fromFile string, args ...string) string {
		t.Helper()
		var theme string
		cmd := newCommand(nil)
		cmd.Action = func(_ context.Context, cmd *cli.Command) error {
			theme = themeOf(cmd, config.Config{Theme: fromFile})
			return nil
		}
		require.NoError(t, cmd.Run(context.Background(), append([]string{"strata"}, args...)))
		return theme
	}

	autoRefreshWith := func(t *testing.T, fromFile bool, args ...string) bool {
		t.Helper()
		var autoRefresh bool
		cmd := newCommand(nil)
		cmd.Action = func(_ context.Context, cmd *cli.Command) error {
			autoRefresh = autoRefreshOf(cmd, config.Config{AutoRefresh: fromFile})
			return nil
		}
		require.NoError(t, cmd.Run(context.Background(), append([]string{"strata"}, args...)))
		return autoRefresh
	}

	t.Run("auto refresh", func(t *testing.T) {
		t.Run("is off when the config file does not turn it on", func(t *testing.T) {
			require.False(t, autoRefreshWith(t, false))
		})

		t.Run("is on when the config file turns it on", func(t *testing.T) {
			require.True(t, autoRefreshWith(t, true))
		})

		t.Run("is off with --remote, also when the config file turns it on", func(t *testing.T) {
			require.False(t, autoRefreshWith(t, true, "--remote"))
		})
	})

	t.Run("theme", func(t *testing.T) {
		t.Run("comes from the config file when --theme is not given", func(t *testing.T) {
			require.Equal(t, "github", themeWith(t, "github"))
		})

		t.Run("--theme overrides the config file", func(t *testing.T) {
			require.Equal(t, "dracula", themeWith(t, "github", "--theme", "dracula"))
		})
	})
}
