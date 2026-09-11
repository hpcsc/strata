package stack

import (
	"context"
	"errors"
	"strings"
)

var ErrNoTrunk = errors.New("no origin/HEAD, origin/main, origin/master, main or master to use as the trunk")

func FindTrunk(ctx context.Context, git runner) (string, error) {
	if out, err := git.Run(ctx, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if trunk := strings.TrimSpace(out); trunk != "" {
			return trunk, nil
		}
	}
	for _, candidate := range []string{"origin/main", "origin/master", "main", "master"} {
		exists, err := git.Check(ctx, "rev-parse", "--verify", "--quiet", candidate+"^{commit}")
		if err != nil {
			return "", err
		}
		if exists {
			return candidate, nil
		}
	}
	return "", ErrNoTrunk
}
