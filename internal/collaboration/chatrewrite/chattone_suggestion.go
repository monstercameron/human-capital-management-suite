package chatrewrite

import (
	"context"
	"strings"
	"sync"
	"time"
)

type ConversationFacts struct {
	RecentLengths    []int
	FormalMarkers    int
	MemberCount      int
	Purpose          string
	Status           string
	RepliesToManager bool
	CustomerFacing   bool
	CloseColleagues  bool
}
type Suggestion struct {
	StyleID   string    `json:"style_id"`
	Reason    string    `json:"reason"`
	ReasonKey string    `json:"reason_key"`
	ExpiresAt time.Time `json:"expires_at"`
}
type AudienceQuestion struct {
	Identity Identity
	Facts    ConversationFacts
	Styles   []Style
	Question string
}
type AudienceDecision interface {
	Suggest(context.Context, AudienceQuestion) (string, error)
}

const AudienceStyleQuestion = "Which registered writing style fits this conversation's audience and register? Return one registered style ID."

type suggestionEntry struct{ suggestion Suggestion }
type Suggestions struct {
	mu       sync.Mutex
	cache    map[[2]string]suggestionEntry
	Decision AudienceDecision
	Registry *Registry
	Now      func() time.Time
}

type HeuristicAudienceDecision struct{}

func (HeuristicAudienceDecision) Suggest(ctx context.Context, q AudienceQuestion) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if q.Question != AudienceStyleQuestion || len(q.Styles) == 0 {
		return "", ErrInvalid
	}
	register, _, _ := audienceHeuristic(BoundFacts(q.Facts))
	for _, s := range q.Styles {
		if s.Register == register {
			return s.ID, nil
		}
	}
	return q.Styles[0].ID, nil
}
func NewSuggestions(registry *Registry, decision AudienceDecision) *Suggestions {
	if decision == nil {
		decision = HeuristicAudienceDecision{}
	}
	return &Suggestions{Registry: registry, Decision: decision, cache: make(map[[2]string]suggestionEntry)}
}
func BoundFacts(f ConversationFacts) ConversationFacts {
	if len(f.RecentLengths) > 20 {
		f.RecentLengths = f.RecentLengths[len(f.RecentLengths)-20:]
	}
	f.RecentLengths = append([]int(nil), f.RecentLengths...)
	for i, n := range f.RecentLengths {
		if n < 0 {
			n = 0
		}
		if n > 4000 {
			n = 4000
		}
		f.RecentLengths[i] = n
	}
	if f.FormalMarkers < 0 {
		f.FormalMarkers = 0
	}
	if f.FormalMarkers > 20 {
		f.FormalMarkers = 20
	}
	if f.MemberCount < 0 {
		f.MemberCount = 0
	}
	if f.MemberCount > 10000 {
		f.MemberCount = 10000
	}
	switch f.Purpose {
	case "announcement", "incident":
	default:
		f.Purpose = "discussion"
	}
	switch f.Status {
	case "active", "resolved":
	default:
		f.Status = "ordinary"
	}
	return f
}
func audienceHeuristic(f ConversationFacts) (string, string, string) {
	if f.Purpose == "incident" && f.Status != "resolved" {
		return "concise", "incident", "Incident conversations benefit from short, direct messages."
	}
	if f.CustomerFacing || f.RepliesToManager || f.Purpose == "announcement" || f.MemberCount > 12 || f.FormalMarkers >= 3 {
		return "professional", "formal", "This audience benefits from neutral, courteous wording."
	}
	total := 0
	for _, n := range f.RecentLengths {
		total += n
	}
	if len(f.RecentLengths) > 0 && total/len(f.RecentLengths) < 80 {
		return "concise", "short", "Recent messages in this conversation are short and direct."
	}
	if f.CloseColleagues || (f.MemberCount > 0 && f.MemberCount <= 4) {
		return "friendly", "close", "A small group of colleagues benefits from warm wording."
	}
	return "professional", "neutral", "Neutral, courteous wording suits this conversation."
}
func (s *Suggestions) Read(ctx context.Context, id Identity, facts ConversationFacts) (Suggestion, error) {
	if ctx == nil || !id.Valid() {
		return Suggestion{}, ErrInvalid
	}
	if s == nil || s.Registry == nil {
		return Suggestion{}, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	styles, enabled := s.Registry.Styles(id.Tenant)
	if !enabled {
		return Suggestion{}, ErrDisabled
	}
	key := [2]string{id.Tenant, id.Conversation}
	for k, row := range s.cache {
		if !now.Before(row.suggestion.ExpiresAt) {
			delete(s.cache, k)
		}
	}
	if row, ok := s.cache[key]; ok {
		for _, style := range styles {
			if row.suggestion.StyleID == style.ID {
				return row.suggestion, nil
			}
		}
		// Registry changes do not cause another decision call inside the hour.
		row.suggestion.StyleID = styles[0].ID
		row.suggestion.ReasonKey = "neutral"
		row.suggestion.Reason = "Neutral, courteous wording suits this conversation."
		s.cache[key] = row
		return row.suggestion, nil
	}
	facts = BoundFacts(facts)
	register, reasonKey, reason := audienceHeuristic(facts)
	selected := styles[0].ID
	for _, style := range styles {
		if style.Register == register {
			selected = style.ID
			break
		}
	}
	if s.Decision != nil {
		decision, err := s.Decision.Suggest(ctx, AudienceQuestion{Identity: id, Facts: facts, Styles: append([]Style(nil), styles...), Question: AudienceStyleQuestion})
		if err == nil {
			for _, style := range styles {
				if style.ID == decision {
					if selected != decision {
						reasonKey = "audience"
						reason = "This style fits the conversation's audience and recent register."
					}
					selected = decision
					break
				}
			}
		}
	}
	result := Suggestion{selected, reason, reasonKey, now.Add(time.Hour)}
	s.cache[key] = suggestionEntry{result}
	return result, nil
}

// SearchStyles exposes the workspace's enabled style list; callers authorize access first.
func (r *Registry) SearchStyles(tenant, query string) []Style {
	styles, enabled := r.Styles(tenant)
	if !enabled {
		return nil
	}
	result := []Style{}
	for _, style := range styles {
		if strings.Contains(strings.ToLower(style.Label), strings.ToLower(query)) {
			result = append(result, style)
		}
	}
	return result
}
