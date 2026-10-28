/*
* Copyright 2019 Armory, Inc.

* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at

*    http://www.apache.org/licenses/LICENSE-2.0

* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
 */

package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/fourleggedlabs/dinghy/pkg/settings/global"
	"github.com/fourleggedlabs/dinghy/pkg/util"
	"github.com/google/go-github/v74/github"
	"golang.org/x/oauth2"
)

const (
	head_commit = "head_commit"
	id          = "id"
)

type GitHubClient interface {
	DownloadContents(string, string, string, string) (string, error)
	CreateStatus(*Status, string, string, string) error
	GetEndpoint() string
	GetToken() string
}

type Config struct {
	Endpoint string
	Token    string
	App      global.GitHubAppConfig
}

// authedClient is a GitHub API client and the token source behind it.
type authedClient struct {
	gh     *github.Client
	tokens oauth2.TokenSource
}

type clientKey struct {
	endpoint string
	token    string
	app      global.GitHubAppConfig
}

// Settings, and so Config, are rebuilt for every webhook, so clients are cached
// by credentials. Reusing one keeps go-github's view of the rate limit and the
// GitHub App installation token across requests.
var (
	clientsMu sync.Mutex
	clients   = map[clientKey]*authedClient{}
)

func (g *Config) client() (*authedClient, error) {
	key := clientKey{endpoint: g.Endpoint, token: g.Token, app: g.App}
	clientsMu.Lock()
	defer clientsMu.Unlock()
	if c, ok := clients[key]; ok {
		return c, nil
	}
	c, err := newAuthedClient(g.Endpoint, g.Token, g.App)
	if err != nil {
		return nil, err
	}
	clients[key] = c
	return c, nil
}

// rateLimitErr converts go-github's rate limit error so callers can recognize
// it without depending on go-github.
func rateLimitErr(err error) error {
	var e *github.RateLimitError
	if errors.As(err, &e) {
		return &util.GithubRateLimitErr{RateLimit: e.Rate.Limit, Reset: e.Rate.Reset.Time}
	}
	return err
}

func (g *Config) DownloadContents(org, repo, path, branch string) (string, error) {
	ctx := context.Background()
	c, err := g.client()
	if err != nil {
		return "", err
	}

	opt := &github.RepositoryContentGetOptions{Ref: branch}
	r, _, err := c.gh.Repositories.DownloadContents(ctx, org, repo, path, opt)
	if err != nil {
		var rl *github.RateLimitError
		if !errors.As(err, &rl) && util.IsGitHubFileNotFoundErr(err.Error()) {
			return "", &util.GitHubFileNotFoundErr{Org: org, Repo: repo, Path: path}
		}
		return "", rateLimitErr(err)
	}

	b := new(bytes.Buffer)
	_, _ = b.ReadFrom(r) //nolint:gosec // read failure yields empty content, handled by caller
	_ = r.Close()

	return b.String(), nil
}

func (g *Config) CreateStatus(status *Status, org, repo, ref string) error {
	repoStatus := &github.RepoStatus{
		State:       &status.State,
		TargetURL:   &status.DeckBaseURL,
		Context:     &status.Context,
		Description: &status.Description,
	}

	ctx := context.Background()
	c, err := g.client()
	if err != nil {
		return err
	}

	_, _, err = c.gh.Repositories.CreateStatus(ctx, org, repo, ref, repoStatus)
	return rateLimitErr(err)
}

func (g *Config) ListStatuses(org, repo, ref string) (error, []*github.RepoStatus) {

	ctx := context.Background()
	c, err := g.client()
	if err != nil {
		return err, nil
	}

	repoStatuses, _, err := c.gh.Repositories.ListStatuses(ctx, org, repo, ref, nil)
	if err != nil {
		return rateLimitErr(err), nil
	}

	return nil, repoStatuses
}

func (g *Config) GetPullRequest(org, repo, ref, sha string) (*github.PullRequest, error) {

	ctx := context.Background()
	c, err := g.client()
	if err != nil {
		return nil, err
	}

	pullRequests, _, err := c.gh.PullRequests.ListPullRequestsWithCommit(ctx, org, repo, sha, &github.ListOptions{})

	if err != nil {
		return nil, err
	}
	if len(pullRequests) > 0 {
		return pullRequests[0], nil
	} else {
		return nil, nil
	}
}

func (g *Config) GetShaFromRawData(rawPushData []byte) string {

	// deserialize push data to a map. used in template logic later
	content := make(map[string]interface{})
	if err := json.Unmarshal(rawPushData, &content); err != nil {
		return ""
	}

	// head_commit is null when a push deletes a branch or tag.
	head, ok := content[head_commit].(map[string]interface{})
	if !ok {
		return ""
	}
	sha, _ := head[id].(string)
	return sha
}

func (g *Config) GetEndpoint() string {
	return g.Endpoint
}

func (g *Config) GetToken() string {
	// With GitHub App auth this is the installation token, otherwise the PAT.
	c, err := g.client()
	if err != nil {
		return ""
	}
	tok, err := c.tokens.Token()
	if err != nil {
		return ""
	}
	return tok.AccessToken
}
