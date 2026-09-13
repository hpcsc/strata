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

	t.Run("theme", func(t *testing.T) {
		t.Run("comes from the config file when --theme is not given", func(t *testing.T) {
			require.Equal(t, "github", themeWith(t, "github"))
		})

		t.Run("--theme overrides the config file", func(t *testing.T) {
			require.Equal(t, "dracula", themeWith(t, "github", "--theme", "dracula"))
		})
	})
}
