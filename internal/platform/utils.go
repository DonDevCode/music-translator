package platform

import "errors"

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
