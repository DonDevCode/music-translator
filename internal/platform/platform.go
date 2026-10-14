// Package platform defines the abstraction for music streaming services.
package platform

// Platform is a music streaming service the bot can recognize and link to.
type Platform interface {
	// Name returns a human-readable name, e.g. "Spotify".
	Name() string
	// Matches reports whether the URL belongs to this platform.
	Matches(url string) bool
}
