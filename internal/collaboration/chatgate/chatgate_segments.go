package chatgate

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// CHATGATE-009: role labels and segments from answers. A rule maps one answer
// to a label (the informal role labels of Conversation details) and to a named
// segment, a set of members the answers define. Both are data about a person
// in this channel. They grant nothing: no permission, here or in the
// organization, reads them.

var (
	// ErrLimited is a request over a rate or abuse limit (CHATGATE-013).
	ErrLimited = errors.New("gate: too many requests")
	// ErrReservedLabel refuses a label that reads as a permission. Labels are
	// informal; one named "Administrator" would be read as authority it is not.
	ErrReservedLabel = errors.New("gate: a label cannot be named like a permission")
	// ErrRestricted refuses restricted visibility where it is not allowed: not
	// switched on for the channel, or the channel is not private.
	ErrRestricted = errors.New("gate: restricted visibility is not allowed here")
)

// ConsumerSegments is the registered consumer that reads answers to place
// people. A question feeds labels and segments only when its declaration says
// so, and only when it is also shown to members.
const ConsumerSegments = "segments"

const (
	PlacementLabel   = "label"
	PlacementSegment = "segment"
)

// SegmentRule maps one answer of a question to a label, a segment, or both.
type SegmentRule struct {
	ID, Field, Value, Label, Segment string
}

// SegmentRuleSet is one version of a gate's rules. Versions are kept so a change
// can be undone and so a person's placement can say which version placed them.
type SegmentRuleSet struct {
	Version int
	Rules   []SegmentRule
	By      string
	Reason  string
	At      time.Time
}

// Placement is one label or segment a person holds and why: the question and the
// answer that put them there. Source is empty for a rule and names an agent for
// a placement an agent applied.
type Placement struct {
	Kind, Name, RuleID, Field, FieldLabel, Value, Source string
}

// SegmentState is a gate's labels and segments. Addressing and Restricted are the
// two levels of CHATGATE-011, decided per channel and off by default.
type SegmentState struct {
	Rules       SegmentRuleSet
	History     []SegmentRuleSet
	Assignments map[string][]Placement
	Addressing  bool
	Restricted  bool
}

var segmentKinds = []string{"single_choice", "multiple_choice", "boolean", "team", "location"}

// reservedLabels are words that name authority somewhere in the product.
var reservedLabels = []string{"admin", "admins", "administrator", "administrators", "manager", "managers", "owner", "owners", "moderator", "channel manager", "workspace administrator", "workspace admin", "verwalter", "administrator:in", "مدير", "مشرف", "مالك"}

func validSegmentText(s string, max int) bool {
	if strings.TrimSpace(s) != s || s == "" || utf8.RuneCountInString(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// validateSegmentRules checks rules against the questions of the version in
// force. The security rule is here: a label or segment is visible to members, so
// it may only come from a question that is shown to members, and the question
// must also name this consumer (CHATGATE-009).
func validateSegmentRules(g Gate, rules []SegmentRule) error {
	d, e := current(g)
	if e != nil || len(rules) > 30 {
		return ErrInvalid
	}
	fields := map[string]Field{}
	for _, f := range d.Fields {
		fields[f.ID] = f
	}
	seen := map[string]bool{}
	for _, r := range rules {
		f, ok := fields[r.Field]
		if r.ID == "" || len(r.ID) > 40 || seen[r.ID] || !ok || !contains(segmentKinds, f.Kind) || strings.TrimSpace(r.Value) == "" || len(r.Value) > 240 {
			return ErrInvalid
		}
		seen[r.ID] = true
		if r.Label == "" && r.Segment == "" {
			return ErrInvalid
		}
		if (r.Label != "" && !validSegmentText(r.Label, 40)) || (r.Segment != "" && (!validSegmentText(r.Segment, 60) || strings.Contains(r.Segment, "|"))) {
			return ErrInvalid
		}
		for _, word := range reservedLabels {
			if strings.EqualFold(strings.TrimSpace(r.Label), word) {
				return ErrReservedLabel
			}
		}
		if f.Kind == "single_choice" || f.Kind == "multiple_choice" {
			if !contains(f.Options, r.Value) {
				return ErrInvalid
			}
		}
		if f.Kind == "boolean" && r.Value != "true" && r.Value != "false" {
			return ErrInvalid
		}
		if !f.Visibility.Members || !contains(f.Visibility.Consumers, ConsumerSegments) {
			return ErrDenied
		}
	}
	return nil
}

func answerMatches(raw json.RawMessage, want string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == want
	}
	var several []string
	if json.Unmarshal(raw, &several) == nil {
		return contains(several, want)
	}
	var yes bool
	if json.Unmarshal(raw, &yes) == nil {
		return strconv.FormatBool(yes) == want
	}
	return false
}

// placementsFor evaluates rules over one person's answers. The declaration used
// is the one the answers were given under, so a later version of the form cannot
// make an answer feed a placement it did not agree to.
func placementsFor(fields []Field, values map[string]json.RawMessage, rules SegmentRuleSet) []Placement {
	declared := map[string]Field{}
	for _, f := range fields {
		declared[f.ID] = f
	}
	out := []Placement{}
	seen := map[string]bool{}
	add := func(kind, name string, r SegmentRule, f Field) {
		key := kind + "\x00" + name
		if name == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, Placement{Kind: kind, Name: name, RuleID: r.ID, Field: r.Field, FieldLabel: f.Label, Value: r.Value})
	}
	for _, r := range rules.Rules {
		f, ok := declared[r.Field]
		raw, answered := values[r.Field]
		if !ok || !answered || !f.Visibility.Members || !contains(f.Visibility.Consumers, ConsumerSegments) || !answerMatches(raw, r.Value) {
			continue
		}
		add(PlacementLabel, r.Label, r, f)
		add(PlacementSegment, r.Segment, r, f)
	}
	sortPlacements(out)
	return out
}

func sortPlacements(p []Placement) {
	sort.SliceStable(p, func(i, j int) bool {
		if p[i].Kind != p[j].Kind {
			return p[i].Kind < p[j].Kind
		}
		return p[i].Name < p[j].Name
	})
}

func samePlacements(a, b []Placement) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// applySegments recomputes one person's placements from their live admitted
// submission. A person with none (declined, withdrawn, left, answers expired)
// holds none. Placements an agent applied are kept for as long as the person is
// admitted. audit says the answers were read for this and the read is recorded.
func (s *Service) applySegments(tx Transaction, c Command, person string, audit bool) {
	st := tx.State()
	var sub *Submission
	for i := len(st.Submissions) - 1; i >= 0; i-- {
		if x := &st.Submissions[i]; x.Person == person && x.Status == "admitted" {
			sub = x
			break
		}
	}
	var next []Placement
	read := []string{}
	if sub != nil {
		values := map[string]json.RawMessage{}
		for _, a := range *tx.Answers() {
			if a.SubmissionID == sub.ID && (s.now().Before(a.ExpiresAt) || a.Held) {
				values[a.FieldID] = a.Value
			}
		}
		next = placementsFor(sub.Fields, values, st.Segments.Rules)
		for _, p := range next {
			if !contains(read, p.Field) {
				read = append(read, p.Field)
			}
		}
		// An agent's placement was made from answers; it goes when they do.
		if len(values) > 0 {
			for _, old := range st.Segments.Assignments[person] {
				if old.Source != "" {
					next = append(next, old)
				}
			}
		}
		sortPlacements(next)
	}
	if audit && len(read) > 0 {
		sort.Strings(read)
		st.Audits = append(st.Audits, ReadAudit{Person: person, Consumer: ConsumerSegments, Purpose: "Role labels and segments", Fields: read, At: s.now()})
	}
	if samePlacements(st.Segments.Assignments[person], next) {
		return
	}
	if st.Segments.Assignments == nil {
		st.Segments.Assignments = map[string][]Placement{}
	}
	if len(next) == 0 {
		delete(st.Segments.Assignments, person)
	} else {
		st.Segments.Assignments[person] = next
	}
	s.event(st, c, "segments_changed", person, "", strconv.Itoa(st.Segments.Rules.Version))
}

// reapplySegments re-evaluates everybody who holds a placement or an admitted
// submission, in a stable order. It runs when rules change and when answers
// expire.
func (s *Service) reapplySegments(tx Transaction, c Command, audit bool) {
	st := tx.State()
	people := map[string]bool{}
	for p := range st.Segments.Assignments {
		people[p] = true
	}
	for _, sub := range st.Submissions {
		if sub.Status == "admitted" {
			people[sub.Person] = true
		}
	}
	ordered := make([]string, 0, len(people))
	for p := range people {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)
	for _, p := range ordered {
		s.applySegments(tx, c, p, audit)
	}
}

// SetSegments publishes a new version of the rules and applies it to every
// current member. The old version is kept: RevertSegments restores it.
func (s *Service) SetSegments(ctx context.Context, c Command, rules []SegmentRule, reason string) error {
	_, e := s.mutate(ctx, c, "admin", []any{"segments", rules, reason}, func(tx Transaction) (any, error) {
		if strings.TrimSpace(reason) == "" || len(reason) > 500 {
			return nil, ErrInvalid
		}
		return s.writeSegments(tx, c, rules, reason)
	})
	return e
}

func (s *Service) writeSegments(tx Transaction, c Command, rules []SegmentRule, reason string) (any, error) {
	st := tx.State()
	if e := validateSegmentRules(st.Gate, rules); e != nil {
		return nil, e
	}
	if st.Segments.Rules.Version > 0 {
		st.Segments.History = append(st.Segments.History, st.Segments.Rules)
	}
	st.Segments.Rules = SegmentRuleSet{Version: st.Segments.Rules.Version + 1, Rules: append([]SegmentRule(nil), rules...), By: c.Actor.Person, Reason: reason, At: s.now()}
	s.event(st, c, "segments_rules_changed", "", "", strconv.Itoa(st.Segments.Rules.Version))
	s.reapplySegments(tx, c, true)
	return st.Segments.Rules.Version, nil
}

// RevertSegments restores the rules of an earlier version as a new version, so
// the history only grows and the change is audited like any other.
func (s *Service) RevertSegments(ctx context.Context, c Command, version int, reason string) error {
	_, e := s.mutate(ctx, c, "admin", []any{"revert", version, reason}, func(tx Transaction) (any, error) {
		if strings.TrimSpace(reason) == "" || len(reason) > 500 {
			return nil, ErrInvalid
		}
		for _, old := range tx.State().Segments.History {
			if old.Version == version {
				if _, e := s.writeSegments(tx, c, old.Rules, "Reverted to version "+strconv.Itoa(version)+": "+reason); e != nil {
					if errors.Is(e, ErrInvalid) || errors.Is(e, ErrDenied) {
						// The questions changed since: the old rules no longer fit them.
						return nil, ErrConflict
					}
					return nil, e
				}
				return version, nil
			}
		}
		return nil, ErrNotFound
	})
	return e
}

// SetAudienceLevels decides, for the channel, whether content may be addressed to
// a segment and whether it may be restricted to one (CHATGATE-011). Restricted
// visibility changes who can read what, so it is offered only in a private
// channel, and only on top of addressing.
func (s *Service) SetAudienceLevels(ctx context.Context, c Command, addressing, restricted bool) error {
	_, e := s.mutate(ctx, c, "admin", []any{"levels", addressing, restricted}, func(tx Transaction) (any, error) {
		if restricted && !addressing {
			return nil, ErrInvalid
		}
		if restricted {
			p, e := s.Authority.Policy(ctx, c.Scope)
			if e != nil {
				return nil, e
			}
			if !p.Private {
				return nil, ErrRestricted
			}
		}
		st := tx.State()
		st.Segments.Addressing, st.Segments.Restricted = addressing, restricted
		s.event(st, c, "audience_levels_changed", "", "", strconv.Itoa(st.Segments.Rules.Version))
		return "set", nil
	})
	return e
}

// SegmentSummary is one label or segment with how many people hold it.
type SegmentSummary struct {
	Name   string
	People int
}

// SegmentOverview is what an administrator sees: the rules, their history and
// how many people each label and segment holds. Never who.
type SegmentOverview struct {
	Rules                SegmentRuleSet
	History              []SegmentRuleSet
	Labels, Segments     []SegmentSummary
	Addressing           bool
	Restricted           bool
	RestrictedMessageCnt int
}

func (s *Service) SegmentOverview(ctx context.Context, a Actor, scope Scope) (SegmentOverview, error) {
	var out SegmentOverview
	if s.Repository == nil {
		return out, ErrUnavailable
	}
	e := s.view(ctx, scope, func(state State) error {
		if e := s.access(ctx, a, scope, "admin"); e != nil {
			return e
		}
		seg := state.Segments
		out = SegmentOverview{Rules: seg.Rules, History: append([]SegmentRuleSet(nil), seg.History...), Addressing: seg.Addressing, Restricted: seg.Restricted}
		labels, segments := map[string]int{}, map[string]int{}
		for _, ps := range seg.Assignments {
			for _, p := range ps {
				if p.Kind == PlacementLabel {
					labels[p.Name]++
				} else {
					segments[p.Name]++
				}
			}
		}
		out.Labels, out.Segments = summaries(labels), summaries(segments)
		return nil
	})
	return out, e
}

func summaries(m map[string]int) []SegmentSummary {
	out := make([]SegmentSummary, 0, len(m))
	for name, n := range m {
		out = append(out, SegmentSummary{Name: name, People: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// MySegments is what a person sees of themself: every label and segment they
// hold and the answer that put them there.
func (s *Service) MySegments(ctx context.Context, a Actor, scope Scope) ([]Placement, error) {
	var out []Placement
	if s.Repository == nil {
		return nil, ErrUnavailable
	}
	e := s.view(ctx, scope, func(state State) error {
		if e := s.access(ctx, a, scope, "self"); e != nil {
			return e
		}
		out = append(out, state.Segments.Assignments[a.Person]...)
		return nil
	})
	return out, e
}

// Members is the people in any of the named segments, in a stable order.
func (st SegmentState) Members(names []string) []string {
	out := []string{}
	for person, ps := range st.Assignments {
		for _, p := range ps {
			if p.Kind == PlacementSegment && contains(names, p.Name) {
				out = append(out, person)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// InAny reports whether a person is in any of the named segments.
func (st SegmentState) InAny(person string, names []string) bool {
	return len(st.Signature(person, names)) > 0
}

// Signature is which of the named segments a person is in, sorted.
func (st SegmentState) Signature(person string, names []string) []string {
	out := []string{}
	for _, p := range st.Assignments[person] {
		if p.Kind == PlacementSegment && contains(names, p.Name) && !contains(out, p.Name) {
			out = append(out, p.Name)
		}
	}
	sort.Strings(out)
	return out
}

// Known reports whether every name is a segment of the rules in force.
func (st SegmentState) Known(names []string) bool {
	if len(names) == 0 {
		return false
	}
	for _, n := range names {
		found := false
		for _, r := range st.Rules.Rules {
			found = found || (r.Segment != "" && r.Segment == n)
		}
		if !found {
			return false
		}
	}
	return true
}

// SegmentNames lists the segments the rules define.
func (st SegmentState) SegmentNames() []string {
	out := []string{}
	for _, r := range st.Rules.Rules {
		if r.Segment != "" && !contains(out, r.Segment) {
			out = append(out, r.Segment)
		}
	}
	sort.Strings(out)
	return out
}

// Reach is who a piece of content addressed to segments goes to, as the author
// is told before sending it. People is a count; nobody is named.
type Reach struct {
	Segments   []string
	People     int
	Restricted bool
}

// ResolveAudience is the one rule for addressing content (CHATGATE-011). The
// segments must exist and addressing must be on. A manager may address any
// segment; a member only the segments they are in, so nobody learns a segment's
// members by addressing it. Restricted visibility needs its own level and a
// private channel. members is the audience at this moment; for restricted
// content it is frozen with the message.
func ResolveAudience(st SegmentState, manager, private bool, author string, names []string, restricted bool) (Reach, []string, error) {
	if !st.Addressing || len(names) == 0 || len(names) > 10 || !st.Known(names) {
		return Reach{}, nil, ErrInvalid
	}
	if restricted && (!st.Restricted || !private) {
		return Reach{}, nil, ErrRestricted
	}
	if !manager {
		for _, n := range names {
			if !st.InAny(author, []string{n}) {
				return Reach{}, nil, ErrDenied
			}
		}
	}
	members := st.Members(names)
	if restricted && !contains(members, author) {
		// The author always reads their own message.
		members = append(members, author)
		sort.Strings(members)
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return Reach{Segments: sorted, People: len(members), Restricted: restricted}, members, nil
}

// Reach tells the author who content addressed to these segments reaches.
func (s *Service) Reach(ctx context.Context, a Actor, scope Scope, names []string, restricted bool) (Reach, error) {
	var out Reach
	if s.Repository == nil {
		return out, ErrUnavailable
	}
	e := s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, PermissionMember); e != nil {
			return e
		}
		p, e := s.Authority.Policy(ctx, scope)
		if e != nil {
			return e
		}
		manager := s.access(ctx, a, scope, "admin") == nil
		out, _, e = ResolveAudience(tx.State().Segments, manager, p.Private, a.Person, names, restricted)
		return e
	})
	return out, e
}

// AddressableSegments lists the segments a person may address content to.
func (s *Service) AddressableSegments(ctx context.Context, a Actor, scope Scope) ([]string, error) {
	var out []string
	if s.Repository == nil {
		return nil, ErrUnavailable
	}
	e := s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, PermissionMember); e != nil {
			return e
		}
		st := tx.State().Segments
		out = []string{}
		if !st.Addressing {
			return nil
		}
		manager := s.access(ctx, a, scope, "admin") == nil
		for _, n := range st.SegmentNames() {
			if manager || st.InAny(a.Person, []string{n}) {
				out = append(out, n)
			}
		}
		return nil
	})
	return out, e
}
