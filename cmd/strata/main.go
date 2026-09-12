package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/git"
	"github.com/hpcsc/strata/internal/progress"
	"github.com/hpcsc/strata/internal/release"
	"github.com/hpcsc/strata/internal/restack"
	"github.com/hpcsc/strata/internal/stack"
	"github.com/hpcsc/strata/internal/syntax"
	"github.com/hpcsc/strata/internal/ui"
	"github.com/hpcsc/strata/internal/version"
	"github.com/hpcsc/strata/internal/viewed"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v3"
)

const releaseRepository = "hpcsc/strata"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	syncErr := restack.Available(git.New(".").Version(ctx))
	if err := newCommand(syncErr).Run(ctx, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "strata:", err)
		os.Exit(1)
	}
}

func newCommand(syncErr error) *cli.Command {
	return &cli.Command{
		Name:      "strata",
		Version:   version.Current(),
		Usage:     "review stacked branches one at a time, each against the branch it sits on",
		ArgsUsage: "[pattern...]",
		Description: "Patterns are for-each-ref patterns that pick the branches, such as refs/heads/team/.\n" +
			"A branch sits on the nearest branch whose tip it contains. If the branch it was\n" +
			"created from has moved on since, the reflogs name that branch instead.",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "remote", Aliases: []string{"r"}, Usage: "review the remote's branches, such as a teammate's stack"},
			&cli.StringFlag{Name: "trunk", Usage: "the branch stacks sit on (default: origin/HEAD, origin/main, origin/master, main or master)"},
			&cli.BoolFlag{Name: "list", Aliases: []string{"l"}, Usage: "print the stack and exit"},
			&cli.StringFlag{Name: "theme", Value: "nord", Usage: "chroma style for syntax highlighting"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return run(ctx, cmd, syncErr)
		},
		Commands: []*cli.Command{
			{
				Name:  "version",
				Usage: "print the tag strata was built from, or its commit when it has no tag",
				Action: func(_ context.Context, cmd *cli.Command) error {
					_, err := fmt.Fprintln(cmd.Root().Writer, version.Current())
					return err
				},
			},
			{
				Name:  "update",
				Usage: "replace strata with the latest release when that release is newer",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "check", Usage: "only report whether a newer release exists"},
					&cli.BoolFlag{Name: "force", Usage: "replace a build that is not a release, such as one built from a commit"},
				},
				Action: update,
			},
			{
				Name:      "sync",
				Usage:     "fetch the trunk and move each stack onto it",
				ArgsUsage: "[pattern...]",
				Hidden:    syncErr != nil,
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "dry-run", Usage: "print the plan and change nothing"},
					&cli.BoolFlag{Name: "keep-merged", Usage: "do not delete the merged branches"},
					&cli.StringFlag{Name: "resolve", Usage: "start a sync rebase for the stack of this branch, which stops at its conflict for you to resolve"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if syncErr != nil {
						return syncErr
					}
					return syncStacks(ctx, cmd)
				},
			},
		},
	}
}

func run(ctx context.Context, cmd *cli.Command, syncErr error) error {
	repo := git.New(".")
	trunk, err := trunkOf(ctx, cmd, repo)
	if err != nil {
		return err
	}
	patterns := patternsOf(cmd)

	reader := stack.NewReader(repo, trunk, patterns)
	tree, err := reader.Read(ctx)
	if err != nil {
		return err
	}
	if len(tree.Branches) == 0 {
		return fmt.Errorf("no branches ahead of %s", trunk)
	}
	if cmd.Bool("list") {
		printTree(os.Stdout, tree)
		return nil
	}

	common, err := repo.Run(ctx, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	marks, err := viewed.Load(filepath.Join(strings.TrimSpace(common), "strata", "viewed"))
	if err != nil {
		return err
	}
	if cmd.Bool("remote") && syncErr == nil {
		syncErr = restack.ErrRemote
	}
	mover := restack.NewMover(repo)
	sync := restack.NewSync(restack.NewPlanner(repo, repo.FetchWithoutPrompt, trunk, patterns), mover, restack.NewResolver(repo, mover), syncErr)
	model := ui.New(ctx, tree, ui.Sources{
		Tree:        reader,
		Diffs:       diff.NewLoader(repo),
		Highlighter: syntax.NewHighlighter(cmd.String("theme")),
		Viewed:      marks,
		Sync:        sync,
	})
	_, err = tea.NewProgram(model, tea.WithContext(ctx)).Run()
	return err
}

func update(ctx context.Context, cmd *cli.Command) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if executable, err = filepath.EvalSymlinks(executable); err != nil {
		return err
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	api := os.Getenv("GITHUB_API_URL")
	if api == "" {
		api = "https://api.github.com"
	}
	client := release.NewClient(&http.Client{Timeout: 2 * time.Minute}, api, releaseRepository, token)
	platform := runtime.GOOS + "-" + runtime.GOARCH
	updater := release.NewUpdater(client, version.Current(), platform, executable)
	status := cmd.Root().ErrWriter
	fmt.Fprintf(status, "Finding the latest release of %s…\n", releaseRepository)
	check, err := updater.Check(ctx)
	if errors.Is(err, release.ErrNoRelease) {
		return fmt.Errorf("found no release of %s: it has none yet, or it is private and GITHUB_TOKEN is not set", releaseRepository)
	}
	if err != nil {
		return err
	}

	out := cmd.Root().Writer
	switch {
	case !version.IsRelease(check.Current) && !cmd.Bool("force"):
		_, err = fmt.Fprintf(out, "strata %s is not a release build. The latest release is %s.\n"+
			"Run strata update --force to replace this build with it.\n", check.Current, check.Latest.Tag)
		return err
	case version.IsRelease(check.Current) && !check.Newer:
		_, err = fmt.Fprintf(out, "strata %s is the latest release.\n", check.Current)
		return err
	case cmd.Bool("check"):
		_, err = fmt.Fprintf(out, "strata %s is available. This is %s. Run strata update to install it.\n", check.Latest.Tag, check.Current)
		return err
	}
	download := progress.Start(status, isTerminal(status), fmt.Sprintf("Downloading strata %s for %s", check.Latest.Tag, platform))
	err = updater.Install(ctx, check.Latest, download.Bytes)
	download.End()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Updated strata from %s to %s at %s.\n", check.Current, check.Latest.Tag, executable)
	return err
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && isatty.IsTerminal(f.Fd())
}

func syncStacks(ctx context.Context, cmd *cli.Command) error {
	if cmd.Bool("remote") {
		return restack.ErrRemote
	}
	dryRun, resolve := cmd.Bool("dry-run"), cmd.String("resolve")
	if dryRun && resolve != "" {
		return errors.New("--dry-run and --resolve do not go together: --resolve starts a rebase")
	}
	repo := git.New(".")
	trunk, err := trunkOf(ctx, cmd, repo)
	if err != nil {
		return err
	}
	out := cmd.Root().Writer
	mover := restack.NewMover(repo)
	resolver := restack.NewResolver(repo, mover)
	var problems []string

	pending, err := resolver.Pending(ctx)
	if err != nil {
		return err
	}
	switch {
	case pending.State == restack.RebaseWaits:
		_, err := resolver.Finish(ctx)
		return err
	case pending.State == restack.RebaseDone && dryRun:
		fmt.Fprintf(out, "The sync rebase for the stack of %s is done, and strata sync moves that stack first.\n\n", pending.Stack)
	case pending.State == restack.RebaseDone:
		result, err := resolver.Finish(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "The sync rebase for the stack of %s is done.", pending.Stack)
		problems = append(problems, printMoved(out, result)...)
		fmt.Fprintln(out)
	case pending.State == restack.RebaseAborted && !dryRun:
		if _, err := resolver.Finish(ctx); err != nil {
			return err
		}
		fmt.Fprintf(out, "You aborted the sync rebase for the stack of %s, and no branch of it moved.\n\n", pending.Stack)
	}

	plan, err := restack.NewPlanner(repo, repo.Fetch, trunk, patternsOf(cmd)).Plan(ctx)
	if err != nil {
		return err
	}
	if cmd.Bool("keep-merged") {
		plan = plan.KeepMerged()
	}
	printPlan(out, plan)

	if resolve != "" {
		started, err := resolver.Start(ctx, plan, resolve)
		if err != nil {
			return err
		}
		if started.State == restack.RebaseWaits {
			fmt.Fprintf(out, "\nThe sync rebase for the stack of %s stopped at the conflict, in %s.\n"+
				"Resolve the conflict there and run git rebase --continue. Then run strata sync to move the stack.\n"+
				"git rebase --abort ends the sync rebase, and no branch moves.\n", started.Stack, started.Worktree)
			return errors.New("the sync rebase waits for you to resolve the conflict")
		}
		result, err := resolver.Finish(ctx)
		if err != nil {
			return err
		}
		problems = append(problems, printMoved(out, result)...)
		return joinProblems(problems)
	}

	switch n := plan.StacksWithConflict(); {
	case n == 1:
		problems = append(problems, "1 stack stays because of a conflict")
	case n > 1:
		problems = append(problems, fmt.Sprintf("%d stacks stay because of conflicts", n))
	}
	if !dryRun {
		result, err := mover.Move(ctx, plan)
		if err != nil {
			return err
		}
		problems = append(problems, printMoved(out, result)...)
	}
	return joinProblems(problems)
}

func printMoved(w io.Writer, result restack.Result) (problems []string) {
	fmt.Fprintf(w, "\nMoved %s.\n", plural(result.Moved, "stack"))
	for _, s := range result.Stayed {
		fmt.Fprintf(w, "The stack of %s stays: %s\n", s.Stack, s.Reason)
	}
	if n := len(result.Stayed); n > 0 {
		problems = append(problems, plural(n, "stack")+" did not move")
	}
	return problems
}

func joinProblems(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

func trunkOf(ctx context.Context, cmd *cli.Command, repo *git.Repo) (string, error) {
	if trunk := cmd.String("trunk"); trunk != "" {
		return trunk, nil
	}
	return stack.FindTrunk(ctx, repo)
}

func patternsOf(cmd *cli.Command) []string {
	if patterns := cmd.Args().Slice(); len(patterns) > 0 {
		return patterns
	}
	if cmd.Bool("remote") {
		return []string{"refs/remotes/origin/"}
	}
	return []string{"refs/heads/"}
}

func printPlan(w io.Writer, plan restack.Plan) {
	tree := plan.Tree
	connectors := tree.Connectors()
	width := 0
	for i, b := range tree.Branches {
		width = max(width, len([]rune(connectors[i]))+len(b.Name))
	}
	fmt.Fprintln(w, tree.Trunk+"  "+plural(plan.NewCommits, "new commit"))
	for i, b := range tree.Branches {
		name := connectors[i] + b.Name
		fmt.Fprintln(w, name+strings.Repeat(" ", width-len([]rune(name)))+"  "+plan.Outcomes[b.Name].Text())
	}
}

func printTree(w io.Writer, tree stack.Tree) {
	connectors := tree.Connectors()
	width := 0
	for i, b := range tree.Branches {
		width = max(width, len([]rune(connectors[i]))+len(b.Name))
	}
	fmt.Fprintln(w, tree.Trunk)
	for i, b := range tree.Branches {
		name := connectors[i] + b.Name
		line := fmt.Sprintf("%s%s  %-11s %-9s +%d -%d", name, strings.Repeat(" ", width-len([]rune(name))),
			plural(b.Commits, "commit"), plural(b.Files, "file"), b.Insertions, b.Deletions)
		if b.Parent != tree.Trunk && b.Behind > 0 {
			line += fmt.Sprintf("  %d behind parent", b.Behind)
		}
		fmt.Fprintln(w, line)
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
