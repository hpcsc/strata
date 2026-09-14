package tmux

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Pane struct {
	ID     string
	Window string
	Path   string
}

type Server struct {
	socket string
}

func New(getenv func(string) string) *Server {
	// tmux sets TMUX to the socket, the pid and the session index of its server
	socket, _, _ := strings.Cut(getenv("TMUX"), ",")
	return &Server{socket: socket}
}

func (s *Server) Panes(ctx context.Context) ([]Pane, error) {
	if s.socket == "" {
		return nil, nil
	}
	out, err := s.run(ctx, "list-panes", "-a", "-F", "#{pane_id}\t#{session_name}:#{window_name}\t#{pane_current_path}")
	if err != nil {
		return nil, err
	}
	var panes []Pane
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) == 3 {
			panes = append(panes, Pane{ID: fields[0], Window: fields[1], Path: fields[2]})
		}
	}
	return panes, nil
}

func (s *Server) ClosePane(ctx context.Context, id string) error {
	_, err := s.run(ctx, "kill-pane", "-t", id)
	return err
}

func (s *Server) run(ctx context.Context, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "tmux", append([]string{"-S", s.socket}, args...)...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
