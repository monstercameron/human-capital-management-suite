package siemhttp

import "net/http"

// Overlay mounts the SIEM route ahead of the existing HTTP edge.
func Overlay(next, feed http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	if feed == nil {
		return next
	}
	mux := http.NewServeMux()
	mux.Handle(Path, feed)
	mux.Handle("/", next)
	return mux
}
