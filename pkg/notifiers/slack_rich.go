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

// Package notifiers provides notification integrations. This file implements
// a rich Slack notifier using Block Kit that posts to a channel whenever a
// pipeline update is processed by dinghy.
package notifiers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/armory/plank/v4"
	log "github.com/sirupsen/logrus"
)

const (
	slackPostMessageURL = "https://slack.com/api/chat.postMessage"
	slackTimeout        = 10 * time.Second

	// Slack bot token env var, e.g. xoxb-...
	envSlackBotToken = "SLACK_BOT_TOKEN" //nolint:gosec // env var name, not a credential
)

// slackBlock is a minimal Slack Block Kit block. We only need the shapes we
// emit; full typing lives in slack-go/slack if this grows.
type slackBlock struct {
	Type     string                   `json:"type"`
	Text     *slackTextObject         `json:"text,omitempty"`
	Fields   []slackTextObject        `json:"fields,omitempty"`
	Elements []map[string]interface{} `json:"elements,omitempty"`
}

type slackTextObject struct {
	Type string `json:"type"` // "mrkdwn" or "plain_text"
	Text string `json:"text"`
}

type slackPayload struct {
	Channel string       `json:"channel,omitempty"`
	Text    string       `json:"text"` // fallback text for notifications preview
	Blocks  []slackBlock `json:"blocks"`
}

// RichSlackNotifier posts Block Kit messages to Slack when a pipeline is
// created or updated by dinghy.
//
// Configuration:
//   - SLACK_BOT_TOKEN env var (bot token starting with xoxb-)
//   - Dinghyfile or Spinnaker application notification block:
//     "notifications": { "slack": [{ "addresses": ["#channel"], "when": ["pipeline.update"] }] }
//   - When no per-app notification block exists, SLACK_DEFAULT_CHANNEL env
//     var is used if set.
type RichSlackNotifier struct {
	BotToken string
	HTTP     *http.Client
	Logger   *log.Logger
	// NotifyOnValidation determines whether messages are sent during
	// validation-only runs (defaults to false: only real updates notify).
	NotifyOnValidation bool
}

func NewRichSlackNotifier(logger *log.Logger) *RichSlackNotifier {
	return &RichSlackNotifier{
		BotToken: os.Getenv(envSlackBotToken),
		HTTP:     &http.Client{Timeout: slackTimeout},
		Logger:   logger,
	}
}

func (n *RichSlackNotifier) SendOnValidation() bool {
	return n.NotifyOnValidation
}

func (n *RichSlackNotifier) SendSuccess(org, repo, path string, notifications plank.NotificationsType, content map[string]interface{}) {
	n.send("Pipeline updated", "good", org, repo, path, nil, notifications, content)
}

func (n *RichSlackNotifier) SendFailure(org, repo, path string, err error, notifications plank.NotificationsType, content map[string]interface{}) {
	n.send("Pipeline update failed", "danger", org, repo, path, err, notifications, content)
}

func (n *RichSlackNotifier) send(title, color string, org, repo, path string, err error, notifications plank.NotificationsType, content map[string]interface{}) {
	if n.BotToken == "" {
		n.Logger.Debug("slack: SLACK_BOT_TOKEN not set, skipping notification")
		return
	}

	channels := n.channels(notifications)
	if len(channels) == 0 {
		n.Logger.Debug("slack: no channels configured for this application, skipping notification")
		return
	}

	payload := n.buildPayload(title, color, org, repo, path, err, content)
	for _, channel := range channels {
		payload.Channel = channel
		if err := n.post(payload); err != nil {
			n.Logger.Errorf("slack: failed to post notification to %s: %v", channel, err)
		}
	}
}

// channels extracts Slack channels from the dinghyfile/application
// notification block, e.g. { "slack": [ { "addresses": ["#ci"] } ] }.
func (n *RichSlackNotifier) channels(notifications plank.NotificationsType) []string {
	var channels []string
	for key, valueSlice := range notifications {
		if key != "slack" {
			continue
		}
		raw, err := json.Marshal(valueSlice)
		if err != nil {
			continue
		}
		var entries []struct {
			Addresses []string `json:"addresses"`
		}
		if json.Unmarshal(raw, &entries) == nil {
			for _, e := range entries {
				channels = append(channels, e.Addresses...)
			}
		}
	}
	if len(channels) == 0 {
		if def := os.Getenv("SLACK_DEFAULT_CHANNEL"); def != "" {
			channels = append(channels, def)
		}
	}
	return normalizeChannels(channels)
}

func normalizeChannels(channels []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, c := range channels {
		// Accept "#channel" or bare "channel" or a channel ID (C0123...).
		// Channel IDs start with an uppercase letter followed by uppercase
		// letters/digits and must not get a "#" prefix.
		if regexp.MustCompile(`^[A-Z][A-Z0-9]+$`).MatchString(c) {
			// channel ID, leave as-is
		} else {
			c = regexp.MustCompile(`^#?`).ReplaceAllString(c, "#")
		}
		if c == "#" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

func (n *RichSlackNotifier) buildPayload(title, color string, org, repo, path string, err error, content map[string]interface{}) slackPayload {
	emoji := "✅"
	if color == "danger" {
		emoji = "❌"
	}

	headerText := fmt.Sprintf("%s %s — *%s/%s*", emoji, title, org, repo)
	blocks := []slackBlock{
		{Type: "header", Text: &slackTextObject{Type: "plain_text", Text: truncate(headerText, 150)}},
	}

	fields := []slackTextObject{
		{Type: "mrkdwn", Text: fmt.Sprintf("*Dinghyfile:*\n`%s`", path)},
		{Type: "mrkdwn", Text: fmt.Sprintf("*Repository:*\n%s/%s", org, repo)},
	}
	blocks = append(blocks, slackBlock{Type: "section", Fields: fields})

	if err != nil {
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: &slackTextObject{Type: "mrkdwn", Text: fmt.Sprintf("*Error:*\n```%s```", truncate(err.Error(), 500))},
		})
	}

	// Extract commit/author info from raw webhook data when available.
	if rawdata, ok := content["rawdata"].(map[string]interface{}); ok {
		if commit := extractCommitInfo(rawdata); commit != "" {
			blocks = append(blocks, slackBlock{
				Type: "section",
				Text: &slackTextObject{Type: "mrkdwn", Text: commit},
			})
		}
	}

	blocks = append(blocks, slackBlock{
		Type: "context",
		Elements: []map[string]interface{}{
			{"type": "plain_text", "text": fmt.Sprintf("Updated by dinghy at %s", time.Now().UTC().Format(time.RFC1123))},
		},
	})

	return slackPayload{Text: headerText, Blocks: blocks}
}

func extractCommitInfo(rawdata map[string]interface{}) string {
	pusher, _ := rawdata["pusher"].(map[string]interface{})
	name, _ := pusher["name"].(string)
	head, _ := rawdata["head_commit"].(map[string]interface{})
	msg, _ := head["message"].(string)
	sha, _ := head["id"].(string)

	var parts []string
	if name != "" {
		parts = append(parts, fmt.Sprintf("*Pusher:* %s", name))
	}
	if sha != "" {
		parts = append(parts, fmt.Sprintf("*Commit:* `%s`", shortSHA(sha)))
	}
	if msg != "" {
		parts = append(parts, fmt.Sprintf("*Message:* %s", truncate(firstLine(msg), 120)))
	}
	if len(parts) == 0 {
		return ""
	}
	return joinLines(parts)
}

func joinLines(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " | "
		}
		out += p
	}
	return out
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func (n *RichSlackNotifier) post(payload slackPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, slackPostMessageURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+n.BotToken)

	resp, err := n.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack api returned %d", resp.StatusCode)
	}
	var apiResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return err
	}
	if !apiResp.OK {
		return fmt.Errorf("slack api error: %s", apiResp.Error)
	}
	return nil
}
