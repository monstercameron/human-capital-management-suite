package chatgate

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Authority is resolved on every call, inside the storage transaction. Facts and
// directory references come from the server, never from submitted profile data.
type Authority interface {
	Check(context.Context, Actor, Scope, string) error
	Policy(context.Context, Scope) (Policy, error)
	Facts(context.Context, Actor, Scope) (map[string]string, error)
	Reference(context.Context, Actor, Scope, Field, json.RawMessage) error
}

// Transaction owns admission and its decision together. Implementations must
// recheck ordinary channel policy before Membership and append the existing outbox.
type Transaction interface {
	State() *State
	Answers() *[]Answer
	Membership(context.Context, Actor, string, bool) error
}
type Repository interface {
	Transact(context.Context, Scope, func(Transaction) error) error
}
type Service struct {
	Repository Repository
	Authority  Authority
	Registry   *Registry
	Clock      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) access(ctx context.Context, a Actor, scope Scope, permission string) error {
	if a.Tenant == "" || a.Tenant != scope.Tenant || a.Person == "" || scope.Conversation == "" {
		return ErrDenied
	}
	if s.Authority == nil {
		return ErrUnavailable
	}
	return s.Authority.Check(ctx, a, scope, permission)
}
func (s *Service) mutate(ctx context.Context, c Command, permission string, input any, fn func(Transaction) (any, error)) (json.RawMessage, error) {
	if s.Repository == nil || s.Registry == nil {
		return nil, ErrUnavailable
	}
	if strings.TrimSpace(c.Key) == "" || len(c.Key) > 200 {
		return nil, ErrInvalid
	}
	var out json.RawMessage
	err := s.Repository.Transact(ctx, c.Scope, func(tx Transaction) error {
		if e := s.access(ctx, c.Actor, c.Scope, permission); e != nil {
			return e
		}
		st := tx.State()
		if st.Receipts == nil {
			st.Receipts = map[string]Receipt{}
		}
		key := c.Actor.Person + ":" + c.Key
		fp := digest(struct {
			Permission string
			Input      any
		}{permission, input})
		if receipt, ok := st.Receipts[key]; ok {
			if receipt.Fingerprint != fp {
				return ErrConflict
			}
			out = append(out, receipt.Result...)
			return nil
		}
		if st.Gate.Revision != c.ExpectedRevision {
			return ErrConflict
		}
		result, e := fn(tx)
		if e != nil {
			return e
		}
		st.Gate.Revision++
		out, e = json.Marshal(result)
		if e != nil {
			return e
		}
		st.Receipts[key] = Receipt{fp, out}
		return nil
	})
	return out, err
}
func (s *Service) event(st *State, c Command, kind, person, id, version string) {
	st.Events = append(st.Events, Event{ID: digest([]string{c.Scope.Tenant, c.Scope.Conversation, c.Actor.Person, c.Key, kind, person, id, version}), Kind: kind, Tenant: c.Scope.Tenant, Conversation: c.Scope.Conversation, Person: person, SubmissionID: id, Version: version, At: s.now()})
}
func (s *Service) Define(ctx context.Context, c Command, d Definition) error {
	_, e := s.mutate(ctx, c, "admin", d, func(tx Transaction) (any, error) {
		p, e := s.Authority.Policy(ctx, c.Scope)
		if e != nil {
			return nil, e
		}
		if d.Mode == "" {
			d.Mode = "automatic"
			if p.Private {
				d.Mode = "review"
			}
		}
		if e = s.Registry.ValidateDefinition(d, p); e != nil {
			return nil, e
		}
		d.Digest = ""
		tx.State().Gate.Draft = &d
		return d, nil
	})
	return e
}
func (s *Service) Publish(ctx context.Context, c Command, v Version) (Definition, error) {
	raw, e := s.mutate(ctx, c, "admin", v, func(tx Transaction) (any, error) {
		g := &tx.State().Gate
		if g.Draft == nil || v.Major == 0 {
			return nil, ErrInvalid
		}
		d := *g.Draft
		p, e := s.Authority.Policy(ctx, c.Scope)
		if e != nil {
			return nil, e
		}
		if e = s.Registry.ValidateDefinition(d, p); e != nil {
			return nil, e
		}
		if old, e := current(*g); e == nil && v.Less(RequiredBump(old, d)) {
			return nil, ErrInvalid
		}
		for _, old := range g.Versions {
			if old.Version == v {
				return nil, ErrConflict
			}
		}
		d.Version = v
		if old, e := current(*g); e == nil && v.Major > old.Version.Major && d.AnswerBy.IsZero() {
			d.AnswerBy = s.now().AddDate(0, 0, 30)
		}
		d.Digest = ContentDigest(d)
		g.Versions = append(g.Versions, d)
		g.Current = v.String()
		g.State = "active"
		g.Draft = nil
		s.event(tx.State(), c, "gate_version_changed", "", "", v.String())
		return d, nil
	})
	var d Definition
	if e == nil {
		e = json.Unmarshal(raw, &d)
	}
	return d, e
}
func (s *Service) Lifecycle(ctx context.Context, c Command, state string) error {
	_, e := s.mutate(ctx, c, "admin", state, func(tx Transaction) (any, error) {
		if !contains([]string{"paused", "retired"}, state) {
			return nil, ErrInvalid
		}
		tx.State().Gate.State = state
		s.event(tx.State(), c, "gate_"+state, "", "", tx.State().Gate.Current)
		return state, nil
	})
	return e
}
func (s *Service) Get(ctx context.Context, a Actor, scope Scope, draft bool) (Gate, error) {
	var out Gate
	if s.Repository == nil {
		return out, ErrUnavailable
	}
	permission := "discover"
	if draft {
		permission = "admin"
	}
	e := s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, permission); e != nil {
			return e
		}
		out = tx.State().Gate
		if !draft {
			out.Draft = nil
			out.Installations = nil
		}
		return nil
	})
	return out, e
}
func (s *Service) validateAnswers(ctx context.Context, a Actor, scope Scope, d Definition, values map[string]json.RawMessage) error {
	known := map[string]bool{}
	for _, f := range d.Fields {
		known[f.ID] = true
		v, ok := values[f.ID]
		if !ok {
			if f.Required {
				return FieldError{Field: f.ID, Cause: ErrRequired}
			}
			continue
		}
		k, ok := s.Registry.Kind(f.Kind, f.KindVersion)
		if !ok {
			return ErrInvalid
		}
		if e := k.Validate(f, v); e != nil {
			return FieldError{Field: f.ID, Cause: e}
		}
		if contains([]string{"person", "team", "location", "acknowledgement"}, f.Kind) {
			if e := s.Authority.Reference(ctx, a, scope, f, v); e != nil {
				return e
			}
		}
	}
	for id := range values {
		if !known[id] {
			return ErrInvalid
		}
	}
	return nil
}
func (s *Service) Submit(ctx context.Context, c Command, version string, values map[string]json.RawMessage) (Submission, error) {
	input := struct {
		Version string
		Answers map[string]json.RawMessage
	}{version, values}
	raw, e := s.mutate(ctx, c, "eligible", input, func(tx Transaction) (any, error) {
		st := tx.State()
		d, e := current(st.Gate)
		if e != nil {
			return nil, e
		}
		if st.Gate.State != "active" || version != d.Version.String() {
			return nil, ErrConflict
		}
		if e = s.validateAnswers(ctx, c.Actor, c.Scope, d, values); e != nil {
			return nil, e
		}
		sub := Submission{ID: digest([]string{c.Scope.Tenant, c.Scope.Conversation, c.Actor.Person, c.Key}), Person: c.Actor.Person, Version: version, Revision: 1, Status: "review", SubmittedAt: s.now(), Fields: d.Fields, Digest: digest(values)}
		kind := "submitted"
		previousAdmission := false
		for i, old := range st.Submissions {
			if old.Person == sub.Person && old.Status != "withdrawn" {
				if old.Status == "superseded" {
					continue
				}
				previousAdmission = old.Status == "admitted"
				sub.Revision = old.Revision + 1
				st.Submissions[i].Status = "superseded"
				st.Submissions[i].Revision++
				kind = "answers_changed"
			}
		}
		if d.Mode == "automatic" {
			sub.Status = "admitted"
		}
		if d.Mode == "rule" {
			facts, e := s.Authority.Facts(ctx, c.Actor, c.Scope)
			if e != nil {
				return nil, e
			}
			for _, rule := range d.Rules {
				if evaluate(rule.When, values, facts) {
					sub.Status = rule.Outcome
					sub.Reason = rule.Reason
					break
				}
			}
		}
		if sub.Status == "admitted" {
			st.Submissions = append(st.Submissions, sub)
			if e := tx.Membership(ctx, c.Actor, version, true); e != nil {
				return nil, e
			}
		} else {
			if previousAdmission {
				if e := tx.Membership(ctx, c.Actor, version, false); e != nil {
					return nil, e
				}
				s.erase(tx, c.Actor.Person, false)
			}
			st.Submissions = append(st.Submissions, sub)
		}
		for _, f := range d.Fields {
			if v, ok := values[f.ID]; ok {
				*tx.Answers() = append(*tx.Answers(), Answer{SubmissionID: sub.ID, Person: sub.Person, FieldID: f.ID, Value: append(json.RawMessage(nil), v...), ExpiresAt: s.now().Add(time.Duration(f.RetentionDays) * 24 * time.Hour)})
			}
		}
		s.event(st, c, kind, sub.Person, sub.ID, version)
		if sub.Status == "admitted" || sub.Status == "declined" {
			s.event(st, c, sub.Status, sub.Person, sub.ID, version)
		}
		return sub, nil
	})
	var sub Submission
	if e == nil {
		e = json.Unmarshal(raw, &sub)
	}
	return sub, e
}
func (s *Service) Review(ctx context.Context, c Command, id string, expected uint64, admit bool, reason string) (Submission, error) {
	raw, e := s.mutate(ctx, c, "admin", []any{id, expected, admit, reason}, func(tx Transaction) (any, error) {
		if strings.TrimSpace(reason) == "" {
			return nil, ErrInvalid
		}
		for i := range tx.State().Submissions {
			sub := &tx.State().Submissions[i]
			if sub.ID != id {
				continue
			}
			if sub.Status != "review" || sub.Revision != expected || tx.State().Gate.State != "active" || sub.Version != tx.State().Gate.Current {
				return nil, ErrConflict
			}
			if admit {
				sub.Status = "admitted"
				if e := tx.Membership(ctx, Actor{Tenant: c.Scope.Tenant, Person: sub.Person}, sub.Version, true); e != nil {
					return nil, e
				}
			} else {
				sub.Status = "declined"
			}
			sub.Revision++
			sub.Reason = reason
			sub.Reviewer = c.Actor.Person
			s.event(tx.State(), c, sub.Status, sub.Person, sub.ID, sub.Version)
			return *sub, nil
		}
		return nil, ErrNotFound
	})
	var sub Submission
	if e == nil {
		e = json.Unmarshal(raw, &sub)
	}
	return sub, e
}
func (s *Service) Withdraw(ctx context.Context, c Command, id string, expected uint64) error {
	_, e := s.mutate(ctx, c, "self", []any{id, expected}, func(tx Transaction) (any, error) {
		st := tx.State()
		for i := range st.Submissions {
			sub := &st.Submissions[i]
			if sub.ID != id {
				continue
			}
			if sub.Person != c.Actor.Person {
				return nil, ErrDenied
			}
			if sub.Revision != expected || sub.Status == "withdrawn" {
				return nil, ErrConflict
			}
			required := false
			for _, f := range sub.Fields {
				required = required || f.Required
			}
			if required && sub.Status == "admitted" {
				if e := tx.Membership(ctx, c.Actor, sub.Version, false); e != nil {
					return nil, e
				}
			}
			sub.Status = "withdrawn"
			sub.Revision++
			s.erase(tx, sub.Person, false)
			s.event(st, c, "withdrawn", sub.Person, id, sub.Version)
			return *sub, nil
		}
		return nil, ErrNotFound
	})
	return e
}
func (s *Service) erase(tx Transaction, person string, expired bool) {
	kept := []Answer{}
	for _, a := range *tx.Answers() {
		remove := a.Person == person
		if expired {
			remove = !s.now().Before(a.ExpiresAt)
		}
		if !remove || a.Held {
			kept = append(kept, a)
		}
	}
	*tx.Answers() = kept
}

// Erase is called by the membership lifecycle and retention scheduler. Holds
// preserve values but do not preserve a former member's right to read them.
func (s *Service) Erase(ctx context.Context, c Command, person string, expired bool) error {
	_, e := s.mutate(ctx, c, "retention", []any{person, expired}, func(tx Transaction) (any, error) {
		s.erase(tx, person, expired)
		s.event(tx.State(), c, "expired", person, "", tx.State().Gate.Current)
		return "erased", nil
	})
	return e
}

type ReadRequest struct {
	SubmissionID                               string
	Actor                                      Actor
	Scope                                      Scope
	Person, Consumer, ConsumerVersion, Purpose string
	Fields                                     []string
	Export                                     bool
}

func (s *Service) read(ctx context.Context, tx Transaction, r ReadRequest) (map[string]json.RawMessage, error) {
	if r.Purpose == "" {
		return nil, ErrInvalid
	}
	if e := s.access(ctx, r.Actor, r.Scope, "read_answers"); e != nil {
		return nil, e
	}
	st := tx.State()
	var sub *Submission
	for i := len(st.Submissions) - 1; i >= 0; i-- {
		x := &st.Submissions[i]
		if x.Person == r.Person && x.Status != "withdrawn" && (r.SubmissionID == "" && x.Status != "superseded" || r.SubmissionID == x.ID) {
			sub = x
			break
		}
	}
	if sub == nil {
		return nil, ErrNotFound
	}
	if r.Export {
		if e := s.access(ctx, r.Actor, r.Scope, "export"); e != nil {
			return nil, e
		}
	}
	admin := s.access(ctx, r.Actor, r.Scope, "admin") == nil
	own := r.Actor.Person == r.Person && r.Consumer == ""
	fields := map[string]Field{}
	for _, f := range sub.Fields {
		fields[f.ID] = f
	}
	requested := r.Fields
	if len(requested) == 0 {
		for _, f := range sub.Fields {
			requested = append(requested, f.ID)
		}
	}
	var descriptor Consumer
	var install *Installation
	if r.Consumer != "" {
		var ok bool
		descriptor, ok = s.Registry.Consumer(r.Consumer, r.ConsumerVersion)
		if !ok {
			return nil, ErrDenied
		}
		if e := s.access(ctx, r.Actor, r.Scope, "consumer:"+r.Consumer); e != nil {
			return nil, e
		}
		for i := range st.Gate.Installations {
			x := &st.Gate.Installations[i]
			if x.ConsumerID == r.Consumer && x.Version == r.ConsumerVersion {
				install = x
				break
			}
		}
		if install == nil {
			return nil, ErrDenied
		}
	}
	out := map[string]json.RawMessage{}
	read := []string{}
	for _, id := range requested {
		f, ok := fields[id]
		if !ok {
			return nil, ErrDenied
		}
		allowed := own || (admin && f.Visibility.Administrators) || (!admin && f.Visibility.Members)
		if r.Consumer != "" {
			mapped := false
			for _, mappedID := range install.Mapping {
				mapped = mapped || mappedID == id
			}
			allowed = contains(f.Visibility.Consumers, r.Consumer) && mapped && (contains(descriptor.Fields, id) || contains(descriptor.Kinds, f.Kind))
		}
		if !allowed {
			if r.Consumer != "" || len(r.Fields) > 0 {
				return nil, ErrDenied
			}
			continue
		}
		for _, a := range *tx.Answers() {
			if a.SubmissionID == sub.ID && a.FieldID == id && (s.now().Before(a.ExpiresAt) || a.Held) {
				out[id] = append(json.RawMessage(nil), a.Value...)
				read = append(read, id)
			}
		}
	}
	sort.Strings(read)
	st.Audits = append(st.Audits, ReadAudit{Person: r.Person, Reader: r.Actor.Person, Consumer: r.Consumer, Purpose: r.Purpose, Fields: read, At: s.now(), Export: r.Export})
	return out, nil
}
func (s *Service) ReadAnswers(ctx context.Context, r ReadRequest) (map[string]json.RawMessage, error) {
	var out map[string]json.RawMessage
	if s.Repository == nil {
		return nil, ErrUnavailable
	}
	e := s.Repository.Transact(ctx, r.Scope, func(tx Transaction) error { var e error; out, e = s.read(ctx, tx, r); return e })
	return out, e
}
func (s *Service) Install(ctx context.Context, c Command, in Installation) error {
	_, e := s.mutate(ctx, c, "admin", in, func(tx Transaction) (any, error) {
		descriptor, ok := s.Registry.Consumer(in.ConsumerID, in.Version)
		if !ok {
			return nil, ErrDenied
		}
		if e := s.access(ctx, c.Actor, c.Scope, descriptor.Permission); e != nil {
			return nil, e
		}
		d, e := current(tx.State().Gate)
		if e != nil {
			return nil, e
		}
		fields := map[string]Field{}
		for _, f := range d.Fields {
			fields[f.ID] = f
		}
		if len(in.Mapping) == 0 {
			return nil, ErrInvalid
		}
		for input, id := range in.Mapping {
			f, ok := fields[id]
			if input == "" || !ok || !contains(f.Visibility.Consumers, in.ConsumerID) || (!contains(descriptor.Fields, id) && !contains(descriptor.Kinds, f.Kind)) {
				return nil, ErrDenied
			}
		}
		for _, old := range tx.State().Gate.Installations {
			if old.ConsumerID == in.ConsumerID {
				return nil, ErrConflict
			}
		}
		tx.State().Gate.Installations = append(tx.State().Gate.Installations, in)
		return in, nil
	})
	return e
}
func (s *Service) ListSubmissions(ctx context.Context, a Actor, scope Scope, status string) ([]Submission, error) {
	var out []Submission
	e := s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, "admin"); e != nil {
			return e
		}
		for _, sub := range tx.State().Submissions {
			if status == "" || sub.Status == status {
				x := sub
				x.Fields = nil
				out = append(out, x)
			}
		}
		return nil
	})
	return out, e
}
func (s *Service) SearchOwnAnswers(ctx context.Context, r ReadRequest, query string) (map[string]json.RawMessage, error) {
	if r.Actor.Person != r.Person || r.Consumer != "" || r.Export {
		return nil, ErrDenied
	}
	values, e := s.ReadAnswers(ctx, r)
	if e != nil {
		return nil, e
	}
	for k, v := range values {
		if !strings.Contains(strings.ToLower(string(v)), strings.ToLower(query)) {
			delete(values, k)
		}
	}
	return values, nil
}
func (s *Service) ExportCSV(ctx context.Context, r ReadRequest) (string, error) {
	r.Export = true
	values, e := s.ReadAnswers(ctx, r)
	if e != nil {
		return "", e
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"field", "answer"})
	ids := []string{}
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		v := string(values[id])
		var text string
		if json.Unmarshal(values[id], &text) == nil {
			v = text
		}
		if len(v) > 0 && strings.ContainsAny(v[:1], "=+-@\t\r") {
			v = "'" + v
		}
		_ = w.Write([]string{id, v})
	}
	w.Flush()
	return b.String(), w.Error()
}
func (s *Service) CanJoin(ctx context.Context, a Actor, scope Scope) error {
	return s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, "eligible"); e != nil {
			return e
		}
		g := tx.State().Gate
		if g.Current == "" || g.State == "paused" || g.State == "retired" {
			return nil
		}
		for _, sub := range tx.State().Submissions {
			if sub.Person == a.Person && sub.Status == "admitted" {
				accepted, err := ParseVersion(sub.Version)
				live, liveErr := ParseVersion(g.Current)
				if err == nil && liveErr == nil && accepted.Major == live.Major {
					return nil
				}
			}
		}
		for _, override := range tx.State().Overrides {
			if override.Person == a.Person && override.Version == g.Current {
				return nil
			}
		}
		return ErrRequired
	})
}
