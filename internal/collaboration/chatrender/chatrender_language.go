package chatrender

import (
	"context"
	"strings"
)

// Span offsets are UTF-8 byte offsets into the exact revision text.
type Span struct {
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Language   string  `json:"language"`
	Confidence float64 `json:"confidence"`
}
type Detection struct {
	Language   string  `json:"language"`
	Confidence float64 `json:"confidence"`
	Spans      []Span  `json:"spans,omitempty"`
	Corrected  bool    `json:"corrected"`
}

type revisionLanguageKey struct{}
type preparedLanguage struct {
	body      string
	detection Detection
}

// WithRevisionLanguage is the additive send/edit hook. The store reuses this
// detection only when the committed body exactly matches the prepared text.
func WithRevisionLanguage(ctx context.Context, body string) context.Context {
	body = strings.TrimSpace(body)
	return context.WithValue(ctx, revisionLanguageKey{}, preparedLanguage{body, Detect(body)})
}
func PreparedDetection(ctx context.Context, body string) Detection {
	if prepared, ok := ctx.Value(revisionLanguageKey{}).(preparedLanguage); ok && prepared.body == body {
		return prepared.detection
	}
	return Detect(body)
}

// Detect uses bounded, deployment-local word and character evidence and script
// evidence (chatlang002_detect.go). Undetermined text is "und" and must not be
// sent for translation.
func Detect(text string) Detection { return chatlang002Detect(text) }
