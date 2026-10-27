package platform

import (
	"encoding/json"
	"errors"
	"net/http"
)

// RateLimitError is returned when an API refuses requests because its rate
// limit or quota is used up. Its message is meant to be shown to users.
type RateLimitError struct {
	API string // e.g. "YouTube"
}

func (e *RateLimitError) Error() string {
	return e.API + " API Rate Limit reached"
}

// AsRateLimitError returns the RateLimitError in err's chain, if any.
func AsRateLimitError(err error) (*RateLimitError, bool) {
	var rateLimitErr *RateLimitError
	if errors.As(err, &rateLimitErr) {
		return rateLimitErr, true
	}
	return nil, false
}

// Error reasons the YouTube Data API uses when the daily quota or a rate
// limit is used up.
var youTubeRateLimitReasons = map[string]bool{
	"quotaExceeded":         true,
	"rateLimitExceeded":     true,
	"userRateLimitExceeded": true,
	"dailyLimitExceeded":    true,
}

// IsYouTubeRateLimited reports whether a YouTube Data API error response is
// about quota or rate limits. Other errors share the same status code (e.g.
// 403 for a disabled API), so the reason in the body is checked.
func IsYouTubeRateLimited(resp *http.Response) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}

	var body struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	for _, e := range body.Error.Errors {
		if youTubeRateLimitReasons[e.Reason] {
			return true
		}
	}
	return false
}

// IsITunesRateLimited reports whether an iTunes Search or Lookup API error
// response means the client is being throttled (roughly 20 requests per
// minute). The API answers 403 without an explanation when it throttles.
func IsITunesRateLimited(resp *http.Response) bool {
	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden
}
