// Package scheduled translates immutable schedule occurrences into the shared
// idempotent agent-run admission inbox. It owns no schedule, grant, or provider
// authority; callers supply current schedule checks and the admission service.
package scheduled
