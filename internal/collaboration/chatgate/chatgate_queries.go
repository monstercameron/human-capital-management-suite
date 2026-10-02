package chatgate

import (
	"context"
	"encoding/json"
	"strings"
)

// ChannelGateServiceV1 is the sole boundary used by pages and consumers.
type ChannelGateServiceV1 interface {
	ConsumerLabels(string) map[string]string
	TryAs(context.Context, Actor, Scope, string, Definition, map[string]json.RawMessage) (string, error)
	ReviewMany(context.Context, Command, []ReviewDecision, string) error
	ExportAllCSV(context.Context, Actor, Scope) (string, error)
	Define(context.Context, Command, Definition) error
	Publish(context.Context, Command, Version) (Definition, error)
	Lifecycle(context.Context, Command, string) error
	Get(context.Context, Actor, Scope, bool) (Gate, error)
	Submit(context.Context, Command, string, map[string]json.RawMessage) (Submission, error)
	Review(context.Context, Command, string, uint64, bool, string) (Submission, error)
	Withdraw(context.Context, Command, string, uint64) error
	ListSubmissions(context.Context, Actor, Scope, string) ([]Submission, error)
	ReadAnswers(context.Context, ReadRequest) (map[string]json.RawMessage, error)
	Install(context.Context, Command, Installation) error
	SaveDraft(context.Context, Command, string, map[string]json.RawMessage) error
	DraftAnswers(context.Context, Actor, Scope) (map[string]json.RawMessage, error)
	Try(context.Context, Actor, Scope, Definition, map[string]json.RawMessage) (string, error)
	MySubmission(context.Context, Actor, Scope) (*Submission, error)
	Reads(context.Context, Actor, Scope) ([]ReadAudit, error)
	ExportCSV(context.Context, ReadRequest) (string, error)
	CanExport(context.Context, Actor, Scope) bool
	RenderControls(Definition, string) map[string]Control
	HasHold(context.Context, Actor, Scope) (bool, error)
}

var _ ChannelGateServiceV1 = (*Service)(nil)

func (s *Service) ConsumerLabels(locale string) map[string]string {
	return s.Registry.ConsumerLabels(locale)
}

func (s *Service) RenderControls(d Definition, locale string) map[string]Control {
	out := map[string]Control{}
	for _, f := range d.Fields {
		if k, ok := s.Registry.Kind(f.Kind, f.KindVersion); ok {
			out[f.ID] = k.Render(f, locale)
		}
	}
	return out
}
func (s *Service) HasHold(ctx context.Context, a Actor, scope Scope) (bool, error) {
	held := false
	e := s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, "self"); e != nil {
			return e
		}
		for _, answer := range *tx.Answers() {
			held = held || (answer.Person == a.Person && answer.Held)
		}
		return nil
	})
	return held, e
}

func (s *Service) CanExport(ctx context.Context, a Actor, scope Scope) bool {
	return s.access(ctx, a, scope, "export") == nil
}

// SaveDraft uses the answer store, not the definition or submission metadata.
func (s *Service) SaveDraft(ctx context.Context, c Command, version string, values map[string]json.RawMessage) error {
	_, e := s.mutate(ctx, c, "eligible", []any{version, values}, func(tx Transaction) (any, error) {
		d, e := current(tx.State().Gate)
		if e != nil || d.Version.String() != version || tx.State().Gate.State != "active" {
			return nil, ErrConflict
		}
		d.Fields = append([]Field(nil), d.Fields...)
		for i := range d.Fields {
			d.Fields[i].Required = false
		}
		copy := map[string]json.RawMessage{}
		for key, value := range values {
			copy[key] = value
		}
		for _, f := range d.Fields {
			if f.Kind == "acknowledgement" && string(copy[f.ID]) == "false" {
				delete(copy, f.ID)
			}
		}
		values = copy
		if e = s.validateAnswers(ctx, c.Actor, c.Scope, d, values); e != nil {
			return nil, e
		}
		id := "draft:" + c.Actor.Person
		kept := []Answer{}
		for _, a := range *tx.Answers() {
			if a.SubmissionID != id {
				kept = append(kept, a)
			}
		}
		for _, f := range d.Fields {
			if v, ok := values[f.ID]; ok {
				kept = append(kept, Answer{SubmissionID: id, Person: c.Actor.Person, FieldID: f.ID, Value: v, ExpiresAt: s.now().AddDate(0, 0, f.RetentionDays)})
			}
		}
		*tx.Answers() = kept
		return "saved", nil
	})
	return e
}
func (s *Service) DraftAnswers(ctx context.Context, a Actor, scope Scope) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	e := s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, "eligible"); e != nil {
			return e
		}
		for _, v := range *tx.Answers() {
			if v.SubmissionID == "draft:"+a.Person && s.now().Before(v.ExpiresAt) {
				out[v.FieldID] = v.Value
			}
		}
		fields := []string{}
		for id := range out {
			fields = append(fields, id)
		}
		tx.State().Audits = append(tx.State().Audits, ReadAudit{Person: a.Person, Reader: a.Person, Purpose: "Resume gate draft", Fields: fields, At: s.now()})
		return nil
	})
	return out, e
}
func (s *Service) MySubmission(ctx context.Context, a Actor, scope Scope) (*Submission, error) {
	var out *Submission
	e := s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, "self"); e != nil {
			return e
		}
		for i := len(tx.State().Submissions) - 1; i >= 0; i-- {
			sub := tx.State().Submissions[i]
			if sub.Person == a.Person {
				x := sub
				x.Fields = nil
				out = &x
				break
			}
		}
		return nil
	})
	return out, e
}
func (s *Service) Reads(ctx context.Context, a Actor, scope Scope) ([]ReadAudit, error) {
	out := []ReadAudit{}
	e := s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, "self"); e != nil {
			return e
		}
		for _, audit := range tx.State().Audits {
			if audit.Person == a.Person {
				out = append(out, audit)
			}
		}
		return nil
	})
	return out, e
}
func (s *Service) Try(ctx context.Context, a Actor, scope Scope, d Definition, values map[string]json.RawMessage) (string, error) {
	return s.TryAs(ctx, a, scope, a.Person, d, values)
}
func (s *Service) TryAs(ctx context.Context, a Actor, scope Scope, person string, d Definition, values map[string]json.RawMessage) (string, error) {
	if e := s.access(ctx, a, scope, "admin"); e != nil {
		return "", e
	}
	if person == "" {
		person = a.Person
	}
	sample := Actor{Tenant: a.Tenant, Person: person}
	if person != a.Person {
		value, _ := json.Marshal(person)
		if e := s.Authority.Reference(ctx, a, scope, Field{Kind: "person", KindVersion: "1.0.0"}, value); e != nil {
			return "", e
		}
	}
	p, e := s.Authority.Policy(ctx, scope)
	if e != nil {
		return "", e
	}
	if e = s.Registry.ValidateDefinition(d, p); e != nil {
		return "", e
	}
	if e = s.validateAnswers(ctx, sample, scope, d, values); e != nil {
		return "", e
	}
	if d.Mode == "automatic" {
		return "admitted", nil
	}
	if d.Mode == "review" {
		return "review", nil
	}
	facts, e := s.Authority.Facts(ctx, sample, scope)
	if e != nil {
		return "", e
	}
	for _, rule := range d.Rules {
		if evaluate(rule.When, values, facts) {
			return rule.Outcome, nil
		}
	}
	return "review", nil
}

// SearchGateQuestions filters by discovery authority before matching any text.
func (s *Service) SearchGateQuestions(ctx context.Context, a Actor, scope Scope, query string) ([]Field, error) {
	g, e := s.Get(ctx, a, scope, false)
	if e != nil {
		return nil, e
	}
	d, e := current(g)
	if e != nil {
		return nil, e
	}
	out := []Field{}
	for _, f := range d.Fields {
		if strings.Contains(strings.ToLower(f.Label+" "+f.Help), strings.ToLower(query)) {
			f.Visibility.Consumers = nil
			out = append(out, f)
		}
	}
	return out, nil
}

type InventoryRow struct {
	Scope   Scope
	Version string
	Fields  []Field
	State   string
}
type Catalog interface {
	ListGateScopes(context.Context, string) ([]Scope, error)
}

func (s *Service) Inventory(ctx context.Context, a Actor) ([]InventoryRow, error) {
	catalog, ok := s.Repository.(Catalog)
	if !ok {
		return nil, ErrUnavailable
	}
	if e := s.access(ctx, a, Scope{Tenant: a.Tenant, Conversation: "*"}, "privacy_report"); e != nil {
		return nil, e
	}
	scopes, e := catalog.ListGateScopes(ctx, a.Tenant)
	if e != nil {
		return nil, e
	}
	out := []InventoryRow{}
	for _, scope := range scopes {
		var g Gate
		e := s.Repository.Transact(ctx, scope, func(tx Transaction) error {
			if e := s.access(ctx, a, scope, "privacy_report"); e != nil {
				return e
			}
			g = tx.State().Gate
			return nil
		})
		if e != nil {
			return nil, e
		}
		for _, d := range g.Versions {
			out = append(out, InventoryRow{Scope: scope, Version: d.Version.String(), Fields: d.Fields, State: g.State})
		}
	}
	return out, nil
}
