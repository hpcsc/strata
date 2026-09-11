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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/git"
	"github.com/hpcsc/strata/internal/release"
	"github.com/hpcsc/strata/internal/stack"
	"github.com/hpcsc/strata/internal/syntax"
	"github.com/hpcsc/strata/internal/ui"
	"github.com/hpcsc/strata/internal/version"
	"github.com/hpcsc/strata/internal/viewed"
	"github.com/urfave/cli/v3"
)

const releaseRepository = "hpcsc/strata"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := newCommand().Run(ctx, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "strata:", err)
		os.Exit(1)
	}
}

func newCommand() *cli.Command {
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
		Action: run,
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
		},
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	repo := git.New(".")
	trunk := cmd.String("trunk")
	if trunk == "" {
		found, err := stack.FindTrunk(ctx, repo)
		if err != nil {
			return err
		}
		trunk = found
	}
	patterns := cmd.Args().Slice()
	if len(patterns) == 0 {
		patterns = []string{"refs/heads/"}
		if cmd.Bool("remote") {
			patterns = []string{"refs/remotes/origin/"}
		}
	}

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
	model := ui.New(ctx, tree, ui.Sources{
		Tree:        reader,
		Diffs:       diff.NewLoader(repo),
		Highlighter: syntax.NewHighlighter(cmd.String("theme")),
		Viewed:      marks,
	})
	_, err = tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
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
	updater := release.NewUpdater(client, version.Current(), runtime.GOOS+"-"+runtime.GOARCH, executable)
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
	if err := updater.Install(ctx, check.Latest); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Updated strata from %s to %s at %s.\n", check.Current, check.Latest.Tag, executable)
	return err
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
