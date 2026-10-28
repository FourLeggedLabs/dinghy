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

package util

import (
	"errors"
	"math/rand/v2"
	"time"
)

// RateLimitRetries is how many times work that failed because the GitHub rate
// limit ran out is retried.
const RateLimitRetries = 3

const (
	// GitHub rate limits reset hourly; a later reset means a skewed clock.
	rateLimitMaxWait = time.Hour
	rateLimitJitter  = 30 * time.Second
)

// RateLimitRetryDelay reports whether err comes from an exhausted GitHub rate
// limit and, if so, how long to wait before retrying: until the limit resets,
// plus up to 30s of jitter so that work held back during the window doesn't
// all reach GitHub in the same second.
func RateLimitRetryDelay(err error, now time.Time) (time.Duration, bool) {
	var rl *GithubRateLimitErr
	if !errors.As(err, &rl) {
		return 0, false
	}
	wait := min(max(rl.Reset.Sub(now), 0), rateLimitMaxWait)
	return wait + rand.N(rateLimitJitter), true //nolint:gosec // jitter, not security
}
