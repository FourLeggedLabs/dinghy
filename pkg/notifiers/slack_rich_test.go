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

package notifiers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/armory/plank/v4"
	log "github.com/sirupsen/logrus"
)

func TestChannelsFromNotifications(t *testing.T) {
	n := NewRichSlackNotifier(log.New())

	notifications := plank.NotificationsType{
		"slack": []interface{}{
			map[string]interface{}{
				"addresses": []interface{}{"#ci", "deploys"},
			},
		},
		"microsoftTeams": []interface{}{
			map[string]interface{}{"addresses": []interface{}{"https://teams.example.com"}},
		},
	}

	got := n.channels(notifications)
	if len(got) != 2 {
		t.Fatalf("expected 2 channels, got %v", got)
	}
	if got[0] != "#ci" || got[1] != "#deploys" {
		t.Fatalf("unexpected channels: %v", got)
	}
}

func TestChannelsNormalizesDuplicates(t *testing.T) {
	got := normalizeChannels([]string{"#ci", "ci", "C0123", "#ci"})
	if len(got) != 2 {
		t.Fatalf("expected 2 unique channels, got %v", got)
	}
	if got[0] != "#ci" || got[1] != "C0123" {
		t.Fatalf("unexpected channels: %v", got)
	}
}

func TestSendSkipsWithoutToken(t *testing.T) {
	n := NewRichSlackNotifier(log.New())
	n.BotToken = ""
	// Must not panic and must not call HTTP
	n.SendSuccess("org", "repo", "path", plank.NotificationsType{}, map[string]interface{}{})
	_ = n
}

func TestSendPostsBlockKitPayload(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))
	defer server.Close()

	n := NewRichSlackNotifier(log.New())
	n.BotToken = "xoxb-test"
	n.HTTP = server.Client()
	// Route requests to the test server via host rewrite
	n.HTTP.Transport = rewriteTransport{base: http.DefaultTransport, host: server.URL}

	notifications := plank.NotificationsType{
		"slack": []interface{}{
			map[string]interface{}{"addresses": []interface{}{"#ci"}},
		},
	}
	content := map[string]interface{}{
		"rawdata": map[string]interface{}{
			"pusher":      map[string]interface{}{"name": "behn"},
			"head_commit": map[string]interface{}{"id": "abcdef123456", "message": "update pipeline\nsecond line"},
		},
	}

	n.SendSuccess("myorg", "myrepo", "dinghyfile", notifications, content)

	if received["channel"] != "#ci" {
		t.Fatalf("expected channel #ci, got %v", received["channel"])
	}
	blocks, ok := received["blocks"].([]interface{})
	if !ok || len(blocks) < 3 {
		t.Fatalf("expected >=3 blocks, got %v", received["blocks"])
	}
	fallback, _ := received["text"].(string)
	if fallback == "" {
		t.Fatal("expected non-empty fallback text")
	}
}

type rewriteTransport struct {
	base http.RoundTripper
	host string
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	orig := req.URL.Host
	host := t.host
	host = host[len("http://"):]
	req.URL.Scheme = "http"
	req.URL.Host = host
	resp, err := t.base.RoundTrip(req)
	req.URL.Host = orig
	return resp, err
}
