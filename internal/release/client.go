package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

var ErrNoRelease = errors.New("the repository has no release yet")

// maxDownload caps a download, so a wrong URL cannot fill memory.
const maxDownload = 200 << 20

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}

func (r Release) Asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

type Client struct {
	http  *http.Client
	api   string
	repo  string
	token string
}

func NewClient(httpClient *http.Client, api, repo, token string) *Client {
	return &Client{http: httpClient, api: api, repo: repo, token: token}
}

func (c *Client) Latest(ctx context.Context) (Release, error) {
	body, err := c.get(ctx, c.api+"/repos/"+c.repo+"/releases/latest", "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	var r Release
	if err := json.Unmarshal(body, &r); err != nil {
		return Release{}, fmt.Errorf("read the latest release of %s: %w", c.repo, err)
	}
	return r, nil
}

// Download fetches an asset through its API URL, which works for a private
// repository when the client has a token.
func (c *Client) Download(ctx context.Context, a Asset) ([]byte, error) {
	return c.get(ctx, a.URL, "application/octet-stream")
}

func (c *Client) get(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNoRelease
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxDownload))
}
