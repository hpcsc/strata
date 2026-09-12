package restack

import (
	"errors"
	"fmt"

	"github.com/hpcsc/strata/internal/git"
)

var minimumGit = git.Version{Major: 2, Minor: 44}

var ErrRemote = errors.New("sync moves local branches, so it does not work with --remote")

func Available(v git.Version, err error) error {
	need := fmt.Sprintf("sync needs git %d.%d or later", minimumGit.Major, minimumGit.Minor)
	if err != nil {
		return fmt.Errorf("%s, and strata cannot read the git version: %w", need, err)
	}
	if !v.AtLeast(minimumGit) {
		return fmt.Errorf("%s, and this is git %s", need, v)
	}
	return nil
}
