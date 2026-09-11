package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/git"
	"github.com/hpcsc/strata/internal/stack"
	"github.com/hpcsc/strata/internal/syntax"
	"github.com/hpcsc/strata/internal/ui"
	"github.com/hpcsc/strata/internal/viewed"
	"github.com/urfave/cli/v3"
)

// the release build sets version through -ldflags
var version = "dev"

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
		Version:   version,
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
