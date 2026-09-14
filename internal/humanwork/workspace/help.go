package workspace

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
)

// HelpService is the server-owned boundary for Help's two stateful
// capabilities. Implementations must apply the authenticated principal,
// tenant and purpose from ctx before reading or writing anything. Those
// values intentionally do not appear in these request types: accepting them
// from the workspace would make the UI an authority boundary.
type HelpService interface {
	SearchKnowledge(context.Context, KnowledgeSearchRequest) (KnowledgeSearchResponse, error)
	CreateSupportRequest(context.Context, SupportRequest) (SupportRequestReceipt, error)
}

var (
	ErrHelpDenied  = errors.New("workspace: help service denied the operation")
	ErrHelpInvalid = errors.New("workspace: invalid help request")
	ErrHelpUnready = errors.New("workspace: help service is unavailable")
)

const (
	MaxKnowledgeQuery   = 256
	MaxKnowledgeResults = 50
	MaxSupportCategory  = 80
	MaxSupportDetails   = 4000
)

// KnowledgeSearchRequest is the bounded, tenant-free query accepted by the
// governed knowledge service. An empty query is refused so a caller cannot
// turn Help into an unbounded article enumeration endpoint.
type KnowledgeSearchRequest struct {
	Query  string
	Locale string
}

func (r KnowledgeSearchRequest) Validate() error {
	if strings.TrimSpace(r.Query) == "" || len([]rune(r.Query)) > MaxKnowledgeQuery {
		return fmt.Errorf("%w: query must contain 1-%d characters", ErrHelpInvalid, MaxKnowledgeQuery)
	}
	if strings.TrimSpace(r.Locale) == "" || len([]rune(r.Locale)) > 32 {
		return fmt.Errorf("%w: locale is required", ErrHelpInvalid)
	}
	return nil
}

// KnowledgeSearchResponse contains only records the service has authorized
// for the caller. The UI must render ProjectKnowledgeSearchResponse rather
// than treating a raw adapter response as displayable data.
type KnowledgeSearchResponse struct {
	Results       []KnowledgeArticle
	PolicyVersion string
}

type KnowledgeArticle struct {
	ID         string
	Title      string
	Summary    string
	Href       string
	Authorized bool
}

// ProjectKnowledgeSearchResponse is a non-disclosing projection. It drops
// unauthorized or malformed records and caps the response. No fallback
// article is synthesized when the service has no authorized result.
func ProjectKnowledgeSearchResponse(in KnowledgeSearchResponse) KnowledgeSearchResponse {
	out := KnowledgeSearchResponse{PolicyVersion: in.PolicyVersion}
	for _, article := range in.Results {
		if !article.Authorized || strings.TrimSpace(article.ID) == "" || strings.TrimSpace(article.Title) == "" {
			continue
		}
		if len(out.Results) == MaxKnowledgeResults {
			break
		}
		article.ID = strings.TrimSpace(article.ID)
		article.Title = strings.TrimSpace(article.Title)
		article.Summary = strings.TrimSpace(article.Summary)
		article.Href = normalizeHelpHref(article.Href)
		out.Results = append(out.Results, article)
	}
	return out
}

// normalizeHelpHref prevents a governed service response from turning the
// workspace into an open redirect or an external-content launcher. Until a
// resolver publishes signed article destinations, only same-origin Help
// routes are displayable; unsupported links are omitted without hiding the
// authorized article itself.
func normalizeHelpHref(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil {
		return ""
	}
	if parsed.Path == "" || parsed.Path != path.Clean(parsed.Path) || !strings.HasPrefix(parsed.Path, "/workspace/app/help/") {
		return ""
	}
	parsed.Fragment = ""
	return parsed.String()
}

// SupportRequest is the minimal human-entered intake. It has no tenant,
// principal or destination fields, and details are never suitable for a URL.
type SupportRequest struct {
	Category string
	Details  string
}

func (r SupportRequest) Validate() error {
	if strings.TrimSpace(r.Category) == "" || len([]rune(r.Category)) > MaxSupportCategory {
		return fmt.Errorf("%w: category must contain 1-%d characters", ErrHelpInvalid, MaxSupportCategory)
	}
	if strings.TrimSpace(r.Details) == "" || len([]rune(r.Details)) > MaxSupportDetails {
		return fmt.Errorf("%w: details must contain 1-%d characters", ErrHelpInvalid, MaxSupportDetails)
	}
	return nil
}

// SupportRequestReceipt is the only successful create result. A service
// adapter must return a durable service-issued reference; the workspace must
// not manufacture one when the service is unavailable or denied.
type SupportRequestReceipt struct {
	RequestID     string
	Status        string
	PolicyVersion string
}

func (r SupportRequestReceipt) Validate() error {
	if strings.TrimSpace(r.RequestID) == "" || strings.TrimSpace(r.Status) == "" {
		return fmt.Errorf("%w: service receipt is incomplete", ErrHelpUnready)
	}
	return nil
}
