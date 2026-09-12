package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Repo struct {
	dir string
}

func New(dir string) *Repo {
	return &Repo{dir: dir}
}

func (r *Repo) Run(ctx context.Context, args ...string) (string, error) {
	return r.RunInput(ctx, "", args...)
}

func (r *Repo) RunInput(ctx context.Context, stdin string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := r.command(ctx, args)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", &Error{Args: args, Stderr: strings.TrimSpace(stderr.String()), Err: err}
	}
	return stdout.String(), nil
}

// Check runs a git command that answers through its exit code: 0 is yes, 1 is no.
func (r *Repo) Check(ctx context.Context, args ...string) (bool, error) {
	var stderr bytes.Buffer
	cmd := r.command(ctx, args)
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, &Error{Args: args, Stderr: strings.TrimSpace(stderr.String()), Err: err}
}

// Try runs a git command for which exit status 1 is an answer, not a failure:
// ok is false, and out still holds what git printed.
func (r *Repo) Try(ctx context.Context, args ...string) (out string, ok bool, err error) {
	var stdout, stderr bytes.Buffer
	cmd := r.command(ctx, args)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		return stdout.String(), true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return stdout.String(), false, nil
	}
	return "", false, &Error{Args: args, Stderr: strings.TrimSpace(stderr.String()), Err: err}
}

func (r *Repo) Version(ctx context.Context) (Version, error) {
	out, err := r.Run(ctx, "version")
	if err != nil {
		return Version{}, err
	}
	return ParseVersion(out)
}

func (r *Repo) Fetch(ctx context.Context, remote string) error {
	args := []string{"fetch", "--prune", remote}
	cmd := r.command(ctx, args)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return &Error{Args: args, Err: err}
	}
	return nil
}

func (r *Repo) command(ctx context.Context, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat")
	return cmd
}

type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), e.Stderr)
	}
	return fmt.Sprintf("git %s: %v", strings.Join(e.Args, " "), e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}
