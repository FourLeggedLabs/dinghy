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
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	dinghylog "github.com/fourleggedlabs/dinghy/pkg/log"
)

func signature(t *testing.T, payload []byte, key string) string {
	t.Helper()
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write(payload)
	return "sha1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestIsValidSignature(t *testing.T) {
	logger := dinghylog.NewDinghyLogs(log.StandardLogger())
	key := "mysecret"
	payload := []byte(`{"ref":"refs/heads/master"}`)

	cases := []struct {
		name     string
		header   string
		expected bool
	}{
		{"valid signature", signature(t, payload, key), true},
		{"forged signature", "sha1=" + hex.EncodeToString([]byte("deadbeef")), false},
		{"wrong key", signature(t, payload, "notmysecret"), false},
		{"wrong prefix", "sha256=" + signature(t, payload, key)[5:], false},
		{"no equals", "malformed-header-no-equals", false},
		{"empty", "", false},
		{"trailing garbage", signature(t, payload, key) + "garbage", false},
		{"prefix only", "sha1=", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				assert.Equal(t, tc.expected, IsValidSignature(payload, tc.header, key, logger))
			})
		})
	}
}
