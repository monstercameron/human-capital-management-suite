// Package candidate contains the small route contract shared by workflow
// adapters and the human-work resolver.
package candidate

// Source says how a principal reached the candidate set.
type Source string

const (
	SourceUnspecified Source = ""
	SourceDirect      Source = "DIRECT"
	SourceDelegated   Source = "DELEGATED"
	SourceFallback    Source = "FALLBACK"
)
