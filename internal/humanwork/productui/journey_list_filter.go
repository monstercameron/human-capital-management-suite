package productui

import (
	"net/url"
	"strings"
	"time"
)

// JourneyListFilter is the Journeys tracker's address-backed narrowing state
// (UXLIVE-031): a search over the person and the request reference, a
// lifecycle status, an inclusive last-updated date range, a recency order
// and a grouping. It is presentation state over records the server has
// already authorized for the viewer; it never widens, and it is never an
// authorization filter.
type JourneyListFilter struct {
	Query  string
	Status string
	From   string
	To     string
	Sort   string
	Group  string
}

// Journey list status values. "open" is every request that is not closed;
// the other four are the lifecycle buckets the tracker already groups by.
const (
	JourneyListStatusOpen    = "open"
	JourneyListStatusReview  = "review"
	JourneyListStatusWaiting = "waiting"
	JourneyListStatusIssue   = "issue"
	JourneyListStatusClosed  = "closed"
)

// Journey list sort and grouping values. The zero value of each is the
// default presentation: most recently updated first, grouped by person.
const (
	JourneyListSortRecent  = "recent"
	JourneyListSortOldest  = "oldest"
	JourneyListGroupPerson = "person"
	JourneyListGroupStatus = "status"
	JourneyListGroupNone   = "none"
)

// Product route keys for the tracker. They carry the "journey_" prefix the
// way Workflow History's keys carry "history_", so they cannot collide with
// People's q/sort/dir on a shared address.
const (
	JourneyListQueryKey  = "journey_q"
	JourneyListStatusKey = "journey_status"
	JourneyListFromKey   = "journey_from"
	JourneyListToKey     = "journey_to"
	JourneyListSortKey   = "journey_sort"
	JourneyListGroupKey  = "journey_group"
)

// JourneyListRouteKeys is the tracker's key set in canonical order.
func JourneyListRouteKeys() []string {
	return []string{JourneyListQueryKey, JourneyListStatusKey, JourneyListFromKey, JourneyListToKey, JourneyListSortKey, JourneyListGroupKey}
}

// JourneyListStatuses is the status vocabulary in presentation order.
func JourneyListStatuses() []string {
	return []string{JourneyListStatusOpen, JourneyListStatusReview, JourneyListStatusWaiting, JourneyListStatusIssue, JourneyListStatusClosed}
}

// JourneyListGroupings is the grouping vocabulary in presentation order.
func JourneyListGroupings() []string {
	return []string{JourneyListGroupPerson, JourneyListGroupStatus, JourneyListGroupNone}
}

// JourneyListSorts is the sort vocabulary in presentation order.
func JourneyListSorts() []string {
	return []string{JourneyListSortRecent, JourneyListSortOldest}
}

const journeyListQueryMaxRunes = 120

// NormalizeJourneyListFilter trims every value, folds enumerations to lower
// case, drops values outside the vocabulary and malformed dates, orders an
// inverted date range, and resets defaults to the zero value so one state
// has exactly one spelling in the address bar.
func NormalizeJourneyListFilter(filter JourneyListFilter) JourneyListFilter {
	result := JourneyListFilter{Query: strings.Join(strings.Fields(filter.Query), " ")}
	if runes := []rune(result.Query); len(runes) > journeyListQueryMaxRunes {
		result.Query = string(runes[:journeyListQueryMaxRunes])
	}
	result.Status = oneOfJourneyList(filter.Status, JourneyListStatuses())
	result.Sort = oneOfJourneyList(filter.Sort, JourneyListSorts())
	if result.Sort == JourneyListSortRecent {
		result.Sort = ""
	}
	result.Group = oneOfJourneyList(filter.Group, JourneyListGroupings())
	if result.Group == JourneyListGroupPerson {
		result.Group = ""
	}
	result.From, result.To = journeyListDate(filter.From), journeyListDate(filter.To)
	if result.From != "" && result.To != "" && result.From > result.To {
		result.From, result.To = result.To, result.From
	}
	return result
}

// ValidJourneyListValues reports whether address values admitted for the
// tracker are inside its vocabulary. An absent value is valid. It is the
// strict parse used at the product router's untrusted boundary; the
// journey client's own fragment parser normalizes instead of refusing.
func ValidJourneyListValues(values url.Values) bool {
	valid := func(key string, allowed []string) bool {
		value := strings.ToLower(strings.TrimSpace(values.Get(key)))
		return value == "" || oneOfJourneyList(value, allowed) != ""
	}
	if !valid(JourneyListStatusKey, JourneyListStatuses()) || !valid(JourneyListSortKey, JourneyListSorts()) || !valid(JourneyListGroupKey, JourneyListGroupings()) {
		return false
	}
	for _, key := range []string{JourneyListFromKey, JourneyListToKey} {
		if raw := strings.TrimSpace(values.Get(key)); raw != "" && journeyListDate(raw) == "" {
			return false
		}
	}
	return true
}

// JourneyListFilterFromValues reads the tracker's product route keys.
func JourneyListFilterFromValues(values url.Values) JourneyListFilter {
	return NormalizeJourneyListFilter(JourneyListFilter{
		Query: values.Get(JourneyListQueryKey), Status: values.Get(JourneyListStatusKey),
		From: values.Get(JourneyListFromKey), To: values.Get(JourneyListToKey),
		Sort: values.Get(JourneyListSortKey), Group: values.Get(JourneyListGroupKey),
	})
}

// SetValues writes the non-default parts of filter onto values under the
// product route keys, and removes the keys it does not set.
func (filter JourneyListFilter) SetValues(values url.Values) {
	normalized := NormalizeJourneyListFilter(filter)
	for _, pair := range []struct{ key, value string }{
		{JourneyListQueryKey, normalized.Query}, {JourneyListStatusKey, normalized.Status},
		{JourneyListFromKey, normalized.From}, {JourneyListToKey, normalized.To},
		{JourneyListSortKey, normalized.Sort}, {JourneyListGroupKey, normalized.Group},
	} {
		if pair.value == "" {
			values.Del(pair.key)
			continue
		}
		values.Set(pair.key, pair.value)
	}
}

// Narrowing reports whether the filter removes any record from the list.
// Sort and grouping reorder and never narrow.
func (filter JourneyListFilter) Narrowing() bool {
	normalized := NormalizeJourneyListFilter(filter)
	return normalized.Query != "" || normalized.Status != "" || normalized.From != "" || normalized.To != ""
}

// IsZero reports whether the filter is the default presentation.
func (filter JourneyListFilter) IsZero() bool {
	return NormalizeJourneyListFilter(filter) == JourneyListFilter{}
}

func oneOfJourneyList(value string, allowed []string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range allowed {
		if value == candidate {
			return candidate
		}
	}
	return ""
}

func journeyListDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) != len("2006-01-02") {
		return ""
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return ""
	}
	return parsed.Format("2006-01-02")
}

// MatchesJourneyListQuery is the tracker's search primitive: the same
// case-insensitive substring match Workflow History applies to its records,
// extended so every whitespace-separated term must match one of fields. An
// empty query matches everything.
func MatchesJourneyListQuery(query string, fields ...string) bool {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return true
	}
	searchable := strings.ToLower(strings.Join(fields, "\n"))
	for _, term := range terms {
		if !strings.Contains(searchable, term) {
			return false
		}
	}
	return true
}

// JourneyListDateInRange reports whether an ISO date (yyyy-mm-dd) falls in
// the inclusive range [from, to]; an empty bound is open. A record with no
// date is outside any bounded range: the tracker does not guess a date.
func JourneyListDateInRange(date, from, to string) bool {
	if from == "" && to == "" {
		return true
	}
	date = journeyListDate(date)
	if date == "" {
		return false
	}
	return (from == "" || date >= from) && (to == "" || date <= to)
}
