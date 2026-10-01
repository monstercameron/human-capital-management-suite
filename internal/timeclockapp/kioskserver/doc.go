// Package kioskserver serves the standalone managed-tablet time clock.
//
// It owns the browser shell and a deliberately small same-origin proxy. The
// proxy exposes only the public device methods used by timeclockapp; admin
// clock methods and the workspace are never reachable through this handler.
package kioskserver
