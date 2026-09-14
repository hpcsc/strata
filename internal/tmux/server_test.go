//go:build integration

package tmux_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpcsc/strata/internal/tmux"
	"github.com/stretchr/testify/require"
)

func TestServer(t *testing.T) {
	ctx := context.Background()
	start := func(t *testing.T) string {
		t.Helper()
		// a socket path must be shorter than 104 bytes on macOS, and t.TempDir is longer there
		dir, err := os.MkdirTemp("", "tmux")
		require.NoError(t, err)
		socket := filepath.Join(dir, "socket")
		t.Cleanup(func() {
			_ = exec.Command("tmux", "-S", socket, "kill-server").Run()
			_ = os.RemoveAll(dir)
		})
		return socket
	}
	run := func(t *testing.T, socket string, args ...string) string {
		t.Helper()
		cmd := exec.Command("tmux", append([]string{"-S", socket, "-f", "/dev/null"}, args...)...)
		cmd.Env = append(os.Environ(), "SHELL=/bin/sh")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "tmux %s: %s", strings.Join(args, " "), out)
		return strings.TrimSpace(string(out))
	}
	open := func(t *testing.T, socket string, args ...string) string {
		t.Helper()
		return run(t, socket, append(args, "-P", "-F", "#{pane_id}", "cat")...)
	}
	folder := func(t *testing.T) string {
		t.Helper()
		dir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		return dir
	}
	env := func(tmuxVariable string) func(string) string {
		return func(name string) string {
			if name == "TMUX" {
				return tmuxVariable
			}
			return ""
		}
	}

	t.Run("panes", func(t *testing.T) {
		t.Run("lists each pane with its window and its folder", func(t *testing.T) {
			socket := start(t)
			orders, shop := folder(t), folder(t)
			first := open(t, socket, "new-session", "-d", "-s", "work", "-n", "orders", "-c", orders)
			second := open(t, socket, "new-window", "-t", "work:", "-n", "shop", "-c", shop)

			panes, err := tmux.New(env(socket + ",4242,0")).Panes(ctx)

			require.NoError(t, err)
			require.Equal(t, []tmux.Pane{
				{ID: first, Window: "work:orders", Path: orders},
				{ID: second, Window: "work:shop", Path: shop},
			}, panes)
		})

		t.Run("lists no panes outside tmux", func(t *testing.T) {
			panes, err := tmux.New(env("")).Panes(ctx)

			require.NoError(t, err)
			require.Empty(t, panes)
		})
	})

	t.Run("close", func(t *testing.T) {
		t.Run("closes the pane, and the window of its last pane", func(t *testing.T) {
			socket := start(t)
			orders := open(t, socket, "new-session", "-d", "-s", "work", "-n", "orders", "-c", folder(t))
			open(t, socket, "new-window", "-t", "work:", "-n", "shop", "-c", folder(t))

			err := tmux.New(env(socket)).ClosePane(ctx, orders)

			require.NoError(t, err)
			require.Equal(t, "shop", run(t, socket, "list-windows", "-a", "-F", "#{window_name}"))
		})
	})
}
