/*
* Copyright 2026 Four Legged Labs
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
*    http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
 */

package github

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fourleggedlabs/dinghy/pkg/settings/global"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v74/github"
	"golang.org/x/oauth2"
	"net/http"
)

const (
	installationTokenExpiry = 55 * time.Minute
	installationTokenSlack  = 5 * time.Minute
)

// appTokenSource provides installation tokens for GitHub App authentication.
// It implements oauth2.TokenSource so tokens are fetched lazily and refreshed
// automatically by oauth2.NewClient.
type appTokenSource struct {
	cfg global.GitHubAppConfig
	key *rsa.PrivateKey
	mu  sync.Mutex
	tok *oauth2.Token
	exp time.Time
	api string // base API URL for installation token endpoint
}

func newAppTokenSource(cfg global.GitHubAppConfig, apiBase string) (*appTokenSource, error) {
	pem, err := loadPrivateKey(cfg)
	if err != nil {
		return nil, err
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM(pem)
	if err != nil {
		return nil, fmt.Errorf("unable to parse GitHub App private key: %w", err)
	}
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	return &appTokenSource{cfg: cfg, key: key, api: apiBase}, nil
}

func loadPrivateKey(cfg global.GitHubAppConfig) ([]byte, error) {
	if cfg.PrivateKeyPath != "" {
		return os.ReadFile(cfg.PrivateKeyPath)
	}
	if cfg.PrivateKey == "" {
		return nil, fmt.Errorf("githubApp: either privateKeyPath or privateKey must be set")
	}
	// Support base64-encoded or raw PEM content
	pem, err := base64.StdEncoding.DecodeString(cfg.PrivateKey)
	if err != nil || !strings.Contains(string(pem), "PRIVATE KEY") {
		return []byte(cfg.PrivateKey), nil
	}
	return pem, nil
}

// appJWT creates a short-lived JWT signed with the App's private key.
func (a *appTokenSource) appJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    fmt.Sprintf("%d", a.cfg.AppID),
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(a.key)
}

// Token returns a cached installation token, refreshing it when near expiry.
func (a *appTokenSource) Token() (*oauth2.Token, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.tok != nil && time.Now().Before(a.exp.Add(-installationTokenSlack)) {
		return a.tok, nil
	}

	jwtTok, err := a.appJWT()
	if err != nil {
		return nil, err
	}
	jwtClient := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: jwtTok},
	))
	jwtGH, err := github.NewEnterpriseClient(a.api, a.api, jwtClient)
	if err != nil {
		return nil, fmt.Errorf("unable to create github client for app auth: %w", err)
	}

	installationToken, resp, err := jwtGH.Apps.CreateInstallationToken(
		context.Background(), a.cfg.InstallationID, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create installation token (appID=%d, installationID=%d): %w",
			a.cfg.AppID, a.cfg.InstallationID, err)
	}
	_ = resp

	tok := &oauth2.Token{
		AccessToken: installationToken.GetToken(),
		Expiry:      installationToken.GetExpiresAt().Time,
	}
	a.tok = tok
	a.exp = tok.Expiry
	return tok, nil
}

// newAuthClient returns an authenticated HTTP client for the given config.
// GitHub App auth takes precedence over a static PAT when configured.
func newAuthClient(cfg global.GitHubAppConfig, token, apiBase string) (*http.Client, error) {
	if cfg.AppID != 0 && cfg.InstallationID != 0 {
		ts, err := newAppTokenSource(cfg, apiBase)
		if err != nil {
			return nil, err
		}
		return oauth2.NewClient(context.Background(), ts), nil
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	return oauth2.NewClient(context.Background(), ts), nil
}
