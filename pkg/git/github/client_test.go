package github

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/fourleggedlabs/dinghy/pkg/settings/global"
	"github.com/fourleggedlabs/dinghy/pkg/util"
)

// Settings, and so Config, are rebuilt for every webhook; the installation
// token must still be fetched once, not once per API call.
func TestAppInstallationTokenReusedAcrossConfigs(t *testing.T) {
	var tokenRequests, statusRequests atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v3/app/installations/456/access_tokens":
			tokenRequests.Add(1)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"token":"inst-token","expires_at":%q}`, time.Now().Add(time.Hour).Format(time.RFC3339))
		case strings.HasPrefix(r.URL.Path, "/api/v3/repos/armory/dinghy/statuses/"):
			statusRequests.Add(1)
			assert.Equal(t, "Bearer inst-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, "{}")
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	app := global.GitHubAppConfig{AppID: 123, InstallationID: 456, PrivateKey: testKeyPEM}
	for range 3 {
		g := &Config{Endpoint: ts.URL, App: app}
		if err := g.CreateStatus(&Status{State: "success", Context: "dinghy"}, "armory", "dinghy", "abc"); err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, "inst-token", g.GetToken())
	}
	assert.Equal(t, int32(3), statusRequests.Load())
	assert.Equal(t, int32(1), tokenRequests.Load())
}

func TestCreateStatusRateLimited(t *testing.T) {
	reset := time.Now().Add(-time.Second).Truncate(time.Second)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
	}))
	defer ts.Close()

	g := &Config{Endpoint: ts.URL, Token: "pat"}
	err := g.CreateStatus(&Status{State: "success", Context: "dinghy"}, "armory", "dinghy", "abc")

	var rl *util.GithubRateLimitErr
	if !errors.As(err, &rl) {
		t.Fatalf("want a GithubRateLimitErr, got %v", err)
	}
	assert.Equal(t, 5000, rl.RateLimit)
	assert.True(t, reset.Equal(rl.Reset), "reset = %s, want %s", rl.Reset, reset)
	// Datadog searches dinghy's logs for this text.
	assert.Contains(t, err.Error(), "Exceeded Githib rate limit")
}
