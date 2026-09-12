//go:build unit

package release_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/hpcsc/strata/internal/release"
	"github.com/stretchr/testify/require"
)

type fakeGitHub struct {
	tag    string
	assets map[string][]byte
	// noRelease makes the latest-release endpoint answer 404.
	noRelease bool
}

func (f fakeGitHub) serve(t *testing.T) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/hpcsc/strata/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if f.noRelease {
			http.NotFound(w, r)
			return
		}
		body := map[string]any{"tag_name": f.tag}
		var assets []map[string]string
		for name := range f.assets {
			assets = append(assets, map[string]string{"name": name, "url": server.URL + "/assets/" + name})
		}
		body["assets"] = assets
		require.NoError(t, json.NewEncoder(w).Encode(body))
	})
	mux.HandleFunc("/assets/{name}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/octet-stream" {
			http.Error(w, "assets need Accept: application/octet-stream", http.StatusBadRequest)
			return
		}
		data := f.assets[r.PathValue("name")]
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func archiveHolding(t *testing.T, binary string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	files := tar.NewWriter(gz)
	for name, content := range map[string]string{"README.md": "# strata\n", "strata": binary} {
		require.NoError(t, files.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}))
		_, err := files.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, files.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func checksumsFor(files map[string][]byte) []byte {
	var buf bytes.Buffer
	for name, data := range files {
		sum := sha256.Sum256(data)
		fmt.Fprintf(&buf, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	return buf.Bytes()
}

func TestUpdater(t *testing.T) {
	ctx := context.Background()
	releaseWith := func(t *testing.T, tag string, archives map[string][]byte) *release.Client {
		t.Helper()
		assets := map[string][]byte{"checksums.txt": checksumsFor(archives)}
		for name, data := range archives {
			assets[name] = data
		}
		server := fakeGitHub{tag: tag, assets: assets}.serve(t)
		return release.NewClient(server.Client(), server.URL, "hpcsc/strata", "")
	}
	installedBinary := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "strata")
		require.NoError(t, os.WriteFile(path, []byte("old binary"), 0o755))
		return path
	}

	t.Run("check", func(t *testing.T) {
		t.Run("a newer release is newer than the current release", func(t *testing.T) {
			client := releaseWith(t, "v0.2.0", nil)

			check, err := release.NewUpdater(client, "v0.1.0", "darwin-arm64", "").Check(ctx)

			require.NoError(t, err)
			require.True(t, check.Newer)
			require.Equal(t, "v0.2.0", check.Latest.Tag)
		})

		t.Run("the current release is not newer than itself", func(t *testing.T) {
			client := releaseWith(t, "v0.2.0", nil)

			check, err := release.NewUpdater(client, "v0.2.0", "darwin-arm64", "").Check(ctx)

			require.NoError(t, err)
			require.False(t, check.Newer)
		})

		t.Run("a build from a commit is never older than a release", func(t *testing.T) {
			client := releaseWith(t, "v0.2.0", nil)

			check, err := release.NewUpdater(client, "8755588-dirty", "darwin-arm64", "").Check(ctx)

			require.NoError(t, err)
			require.False(t, check.Newer)
			require.Equal(t, "v0.2.0", check.Latest.Tag)
		})

		t.Run("a repository with no release says so", func(t *testing.T) {
			server := fakeGitHub{noRelease: true}.serve(t)
			client := release.NewClient(server.Client(), server.URL, "hpcsc/strata", "")

			_, err := release.NewUpdater(client, "v0.1.0", "darwin-arm64", "").Check(ctx)

			require.ErrorIs(t, err, release.ErrNoRelease)
		})
	})

	t.Run("install", func(t *testing.T) {
		t.Run("replaces the executable with the binary from the archive for this platform", func(t *testing.T) {
			client := releaseWith(t, "v0.2.0", map[string][]byte{
				"strata-darwin-arm64.tar.gz": archiveHolding(t, "darwin arm64 binary"),
				"strata-linux-amd64.tar.gz":  archiveHolding(t, "linux amd64 binary"),
			})
			path := installedBinary(t)
			updater := release.NewUpdater(client, "v0.1.0", "darwin-arm64", path)
			check, err := updater.Check(ctx)
			require.NoError(t, err)

			require.NoError(t, updater.Install(ctx, check.Latest, nil))

			installed, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "darwin arm64 binary", string(installed))
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0o755), info.Mode().Perm())
		})

		t.Run("reports the bytes of the archive as they arrive, up to the size of the archive", func(t *testing.T) {
			archive := archiveHolding(t, "darwin arm64 binary")
			client := releaseWith(t, "v0.2.0", map[string][]byte{"strata-darwin-arm64.tar.gz": archive})
			updater := release.NewUpdater(client, "v0.1.0", "darwin-arm64", installedBinary(t))
			check, err := updater.Check(ctx)
			require.NoError(t, err)
			var reported [][2]int64

			err = updater.Install(ctx, check.Latest, func(done, total int64) {
				reported = append(reported, [2]int64{done, total})
			})

			require.NoError(t, err)
			require.NotEmpty(t, reported)
			size := int64(len(archive))
			require.Equal(t, [2]int64{size, size}, reported[len(reported)-1])
		})

		t.Run("leaves the executable alone when the download does not match its checksum", func(t *testing.T) {
			archive := archiveHolding(t, "new binary")
			assets := map[string][]byte{
				"strata-darwin-arm64.tar.gz": archive,
				"checksums.txt":              checksumsFor(map[string][]byte{"strata-darwin-arm64.tar.gz": []byte("something else")}),
			}
			server := fakeGitHub{tag: "v0.2.0", assets: assets}.serve(t)
			path := installedBinary(t)
			updater := release.NewUpdater(release.NewClient(server.Client(), server.URL, "hpcsc/strata", ""), "v0.1.0", "darwin-arm64", path)
			check, err := updater.Check(ctx)
			require.NoError(t, err)

			err = updater.Install(ctx, check.Latest, nil)

			require.ErrorContains(t, err, "does not match its checksum")
			installed, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			require.Equal(t, "old binary", string(installed))
		})

		t.Run("a release with no archive for this platform names the platform", func(t *testing.T) {
			client := releaseWith(t, "v0.2.0", map[string][]byte{"strata-linux-amd64.tar.gz": archiveHolding(t, "linux amd64 binary")})
			path := installedBinary(t)
			updater := release.NewUpdater(client, "v0.1.0", "darwin-arm64", path)
			check, err := updater.Check(ctx)
			require.NoError(t, err)

			err = updater.Install(ctx, check.Latest, nil)

			require.ErrorContains(t, err, "darwin-arm64")
		})
	})
}
