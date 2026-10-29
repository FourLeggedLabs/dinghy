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
	"github.com/fourleggedlabs/dinghy/pkg/git"
)

/* Example: POST /repos/:owner/:repo/statuses/:sha
{
  "state": "success",
  "target_url": "https://example.com/build/status",
  "description": "The build succeeded!",
  "context": "continuous-integration/jenkins"
} */

// Status is a type that contains information relevant to the commit status
type Status struct {
	State       string
	DeckBaseURL string
	Description string
	Context     string
}

// SetCommitStatus sets the status on the push's head commit. Required checks are
// evaluated on a pull request's head commit, so the push's earlier commits don't
// need one, and posting one per commit spent the rate limit on statuses nobody
// reads. A push with no commits (a new branch, a rewind) gets no status.
func (p *Push) SetCommitStatus(instanceId string, status git.Status, description string) error {
	if len(p.Commits) == 0 {
		return nil
	}
	head := p.Commits[len(p.Commits)-1].ID
	s := newStatus(instanceId, status, p.DeckBaseURL, description)
	if err := p.Config.CreateStatus(s, p.Org(), p.Repo(), head); err != nil {
		p.Logger.Error(err)
		return err
	}
	return nil
}

func (p *Push) GetCommitStatus() (error, git.Status, string) {
	err, statuses := p.Config.ListStatuses(p.Org(), p.Repo(), p.Branch())
	if err != nil {
		p.Logger.Warnf("Failed to get status information for %v/%v/%v", p.Org(), p.Repo(), p.Branch())
		return err, "", ""
	}
	for _, status := range statuses {
		if status.GetContext() == "dinghy" {
			return nil, git.Status(status.GetState()), status.GetDescription()
		}
	}
	return nil, "", ""
}

// Commits return the list of commit hashes
func (p *Push) GetCommits() []string {
	result := make([]string, 0, len(p.Commits))
	for _, val := range p.Commits {
		result = append(result, val.ID)
	}
	return result
}

func newStatus(instanceId string, s git.Status, deckURL string, description string) *Status {
	state := string(s)
	context := instanceId

	if len(description) > 140 {
		description = description[0:136] + "..."
	}

	return &Status{
		State:       state,
		DeckBaseURL: deckURL,
		Context:     context,
		Description: description,
	}
}
