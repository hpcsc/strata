//go:build integration

package diff_test

import (
	"context"
	"testing"

	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/git"
	"github.com/hpcsc/strata/internal/gittest"
	"github.com/stretchr/testify/require"
)

func TestLoader(t *testing.T) {
	ctx := context.Background()
	fileNamed := func(t *testing.T, files []diff.File, path string) diff.File {
		t.Helper()
		for _, f := range files {
			if f.Path == path {
				return f
			}
		}
		require.Failf(t, "file not listed", "%s is not in %v", path, files)
		return diff.File{}
	}

	t.Run("files", func(t *testing.T) {
		t.Run("lists added, modified, deleted and renamed files between the base and the branch", func(t *testing.T) {
			repo := gittest.New(t)
			repo.Commit("keep.txt", "one\n", "Add keep")
			repo.Commit("gone.txt", "gone\n", "Add gone")
			repo.Commit("old-name.txt", "a\nb\nc\nd\ne\n", "Add old-name")
			repo.SwitchNew("branch")
			repo.Write("keep.txt", "one\ntwo\n")
			repo.Git("rm", "-q", "gone.txt")
			repo.Git("mv", "old-name.txt", "new-name.txt")
			repo.Commit("added.txt", "new\n", "Change everything")

			files, err := diff.NewLoader(git.New(repo.Dir)).Files(ctx, "main", "branch")

			require.NoError(t, err)
			require.Len(t, files, 4)
			require.Equal(t, diff.Added, fileNamed(t, files, "added.txt").Status)
			require.Equal(t, diff.Modified, fileNamed(t, files, "keep.txt").Status)
			require.Equal(t, diff.Deleted, fileNamed(t, files, "gone.txt").Status)
			renamed := fileNamed(t, files, "new-name.txt")
			require.Equal(t, diff.Renamed, renamed.Status)
			require.Equal(t, "old-name.txt", renamed.OldPath)
		})

		t.Run("counts the lines each file adds and deletes, including a renamed file", func(t *testing.T) {
			repo := gittest.New(t)
			repo.Commit("keep.txt", "one\ntwo\n", "Add keep")
			repo.Commit("old-name.txt", "a\nb\nc\nd\ne\nf\n", "Add old-name")
			repo.SwitchNew("branch")
			repo.Write("keep.txt", "one\nthree\nfour\n")
			repo.Git("mv", "old-name.txt", "new-name.txt")
			repo.Commit("new-name.txt", "a\nb\nc\nd\ne\nf\ng\n", "Change keep and rename")

			files, err := diff.NewLoader(git.New(repo.Dir)).Files(ctx, "main", "branch")

			require.NoError(t, err)
			keep, renamed := fileNamed(t, files, "keep.txt"), fileNamed(t, files, "new-name.txt")
			require.Equal(t, [2]int{2, 1}, [2]int{keep.Insertions, keep.Deletions})
			require.Equal(t, [2]int{1, 0}, [2]int{renamed.Insertions, renamed.Deletions})
		})

		t.Run("gives an added file no old blob and a deleted file no new blob", func(t *testing.T) {
			repo := gittest.New(t)
			repo.Commit("gone.txt", "gone\n", "Add gone")
			repo.SwitchNew("branch")
			repo.Git("rm", "-q", "gone.txt")
			repo.Commit("added.txt", "new\n", "Swap files")

			files, err := diff.NewLoader(git.New(repo.Dir)).Files(ctx, "main", "branch")

			require.NoError(t, err)
			added, deleted := fileNamed(t, files, "added.txt"), fileNamed(t, files, "gone.txt")
			require.Empty(t, added.OldBlob)
			require.NotEmpty(t, added.NewBlob)
			require.NotEmpty(t, deleted.OldBlob)
			require.Empty(t, deleted.NewBlob)
		})
	})

	t.Run("patch", func(t *testing.T) {
		t.Run("numbers each line on the side it belongs to", func(t *testing.T) {
			repo := gittest.New(t)
			repo.Commit("list.txt", "a\nb\nc\n", "Add list")
			repo.SwitchNew("branch")
			repo.Commit("list.txt", "a\nB\nc\nd\n", "Change list")
			loader := diff.NewLoader(git.New(repo.Dir))
			files, err := loader.Files(ctx, "main", "branch")
			require.NoError(t, err)

			patch, err := loader.Patch(ctx, "main", "branch", files[0])

			require.NoError(t, err)
			require.Len(t, patch.Hunks, 1)
			require.Equal(t, []diff.Line{
				{Kind: diff.Context, Text: "a", OldNumber: 1, NewNumber: 1},
				{Kind: diff.Deletion, Text: "b", OldNumber: 2},
				{Kind: diff.Addition, Text: "B", NewNumber: 2},
				{Kind: diff.Context, Text: "c", OldNumber: 3, NewNumber: 3},
				{Kind: diff.Addition, Text: "d", NewNumber: 4},
			}, patch.Hunks[0].Lines)
		})

		t.Run("describes a binary file instead of showing lines", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("branch")
			repo.Commit("image.bin", "\x00\x01\x02binary", "Add a binary file")
			loader := diff.NewLoader(git.New(repo.Dir))
			files, err := loader.Files(ctx, "main", "branch")
			require.NoError(t, err)

			patch, err := loader.Patch(ctx, "main", "branch", files[0])

			require.NoError(t, err)
			require.Empty(t, patch.Hunks)
			require.Equal(t, "binary file changed", patch.Summary())
		})

		t.Run("describes a rename with no other changes", func(t *testing.T) {
			repo := gittest.New(t)
			repo.Commit("old.txt", "same\n", "Add old")
			repo.SwitchNew("branch")
			repo.Git("mv", "old.txt", "new.txt")
			repo.Git("commit", "-q", "-m", "Rename")
			loader := diff.NewLoader(git.New(repo.Dir))
			files, err := loader.Files(ctx, "main", "branch")
			require.NoError(t, err)

			patch, err := loader.Patch(ctx, "main", "branch", files[0])

			require.NoError(t, err)
			require.Equal(t, "renamed from old.txt with no other changes", patch.Summary())
		})
	})

	t.Run("contents", func(t *testing.T) {
		t.Run("returns the file's text before and after the change", func(t *testing.T) {
			repo := gittest.New(t)
			repo.Commit("list.txt", "a\n", "Add list")
			repo.SwitchNew("branch")
			repo.Commit("list.txt", "a\nb\n", "Extend list")
			loader := diff.NewLoader(git.New(repo.Dir))
			files, err := loader.Files(ctx, "main", "branch")
			require.NoError(t, err)

			before, after, err := loader.Contents(ctx, files[0])

			require.NoError(t, err)
			require.Equal(t, "a\n", before)
			require.Equal(t, "a\nb\n", after)
		})

		t.Run("returns no text before for an added file", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("branch")
			repo.Commit("new.txt", "fresh\n", "Add new")
			loader := diff.NewLoader(git.New(repo.Dir))
			files, err := loader.Files(ctx, "main", "branch")
			require.NoError(t, err)

			before, after, err := loader.Contents(ctx, files[0])

			require.NoError(t, err)
			require.Empty(t, before)
			require.Equal(t, "fresh\n", after)
		})
	})
}
