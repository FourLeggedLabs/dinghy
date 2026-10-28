package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/fourleggedlabs/dinghy/pkg/dinghyfile"
	"github.com/fourleggedlabs/dinghy/pkg/git/github"
	dinghylog "github.com/fourleggedlabs/dinghy/pkg/log"
	"github.com/fourleggedlabs/dinghy/pkg/logevents"
	"github.com/fourleggedlabs/dinghy/pkg/settings/global"
	"github.com/fourleggedlabs/dinghy/pkg/util"
)

type postedStatus struct {
	SHA, State, Description string
}

// fakeGitHub serves the commit status API, answering everything with GitHub's
// rate limit response while limited.
type fakeGitHub struct {
	mu         sync.Mutex
	limited    bool
	listFails  bool
	posted     []postedStatus
	listCalled int
}

func (f *fakeGitHub) setLimited(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limited = v
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.limited {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "0")
		// A reset in the past keeps go-github from refusing the retry itself.
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(-time.Second).Unix(), 10))
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
		return
	}
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/statuses"):
		f.listCalled++
		if f.listFails {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, "[]")
	case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/statuses/"):
		var s struct{ State, Description string }
		_ = json.NewDecoder(r.Body).Decode(&s)
		f.posted = append(f.posted, postedStatus{SHA: path.Base(r.URL.Path), State: s.State, Description: s.Description})
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, "{}")
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// stubRetries runs scheduled retries at once, calling beforeRetry first, and
// returns the delays they were scheduled with.
func stubRetries(t *testing.T, beforeRetry func()) *[]time.Duration {
	var delays []time.Duration
	orig := afterFunc
	afterFunc = func(d time.Duration, f func()) *time.Timer {
		delays = append(delays, d)
		beforeRetry()
		f()
		return nil
	}
	t.Cleanup(func() { afterFunc = orig })
	return &delays
}

type pushRun struct {
	gh      *fakeGitHub
	parser  dinghyfile.Parser
	files   []string
	branch  string
	lec     logevents.LogEventsClient
	retries func()
}

func (r pushRun) run(t *testing.T) (*httptest.ResponseRecorder, *[]time.Duration) {
	ts := httptest.NewServer(r.gh)
	t.Cleanup(ts.Close)

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	l := dinghylog.NewDinghyLogs(logger)

	p := &github.Push{
		Config:     github.Config{Endpoint: ts.URL, Token: "pat"},
		Repository: github.Repository{Organization: "launchdarkly", Name: "streamer"},
		Commits:    []github.Commit{{ID: "aaa", Modified: []string{"README.md"}}, {ID: "bbb", Modified: r.files}},
		Ref:        r.branch,
		Logger:     l,
	}
	s := &global.Settings{DinghyFilename: "dinghyfile", InstanceId: "dinghy"}

	wa := NewWebAPI(nil, nil, nil, logger, nil, nil, r.lec, logger)
	wa.Parser = r.parser
	if wa.Parser == nil {
		wa.Parser = dinghyfile.NewDinghyfileParser(&dinghyfile.PipelineBuilder{})
	}
	retries := r.retries
	if retries == nil {
		retries = func() {}
	}
	delays := stubRetries(t, retries)

	rr := httptest.NewRecorder()
	d := &github.FileService{GitHub: &p.Config, Logger: l}
	wa.buildPipelines(context.Background(), p, []byte("{}"), d, rr, l, "", nil, s)
	return rr, delays
}

func TestNoDinghyfilePushPostsStatusWhenLookupFails(t *testing.T) {
	gh := &fakeGitHub{listFails: true}
	rr, delays := pushRun{gh: gh, files: []string{"main.go"}, branch: "feature"}.run(t)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, 1, gh.listCalled)
	assert.Empty(t, *delays)
	assert.Equal(t, []postedStatus{{"bbb", "success", "No changes in dinghyfile."}}, gh.posted)
}

func TestNoDinghyfilePushRetriedAfterRateLimit(t *testing.T) {
	gh := &fakeGitHub{limited: true}
	rr, delays := pushRun{gh: gh, files: []string{"main.go"}, branch: "feature", retries: func() { gh.setLimited(false) }}.run(t)

	assert.Equal(t, http.StatusAccepted, rr.Code)
	assert.Equal(t, `{"status":"retrying"}`, rr.Body.String())
	assert.Len(t, *delays, 1)
	assert.Equal(t, []postedStatus{{"bbb", "success", "No changes in dinghyfile."}}, gh.posted)
}

func TestDinghyfilePushRetriedUntilGivingUp(t *testing.T) {
	ctrl := gomock.NewController(t)
	limited := &util.GithubRateLimitErr{RateLimit: 5000, Reset: time.Now()}

	parser := dinghyfile.NewMockParser(ctrl)
	parser.EXPECT().SetBuilder(gomock.Any()).AnyTimes()
	// The first attempt and every retry download the dinghyfile again.
	parser.EXPECT().Parse("launchdarkly", "streamer", "cmd/flagwatcher/dinghygen/dinghyfile", "main", nil).
		Return(nil, limited).Times(util.RateLimitRetries + 1)
	// A failure is recorded once, when dinghy gives up, not for each retry.
	lec := logevents.NewMockLogEventsClient(ctrl)
	lec.EXPECT().SaveLogEvent(gomock.Any()).Return(nil).Times(1)

	gh := &fakeGitHub{}
	rr, delays := pushRun{gh: gh, parser: parser, files: []string{"cmd/flagwatcher/dinghygen/dinghyfile"}, branch: "main", lec: lec}.run(t)

	assert.Equal(t, http.StatusAccepted, rr.Code)
	assert.Len(t, *delays, util.RateLimitRetries)
	if assert.NotEmpty(t, gh.posted) {
		for _, s := range gh.posted {
			assert.Equal(t, "bbb", s.SHA)
		}
		last := gh.posted[len(gh.posted)-1]
		assert.Equal(t, "error", last.State)
		assert.Contains(t, last.Description, "Exceeded Githib rate limit")
	}
}
