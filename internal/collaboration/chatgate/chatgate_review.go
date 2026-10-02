package chatgate

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"sort"
	"strings"
)

type ReviewDecision struct {
	SubmissionID string
	Revision     uint64
	Admit        bool
}

func (s *Service) ReviewMany(ctx context.Context, c Command, decisions []ReviewDecision, reason string) error {
	_, e := s.mutate(ctx, c, "admin", []any{decisions, reason}, func(tx Transaction) (any, error) {
		if len(decisions) == 0 || len(decisions) > 100 || strings.TrimSpace(reason) == "" {
			return nil, ErrInvalid
		}
		seen := map[string]bool{}
		indices := []int{}
		for _, decision := range decisions {
			if seen[decision.SubmissionID] {
				return nil, ErrInvalid
			}
			seen[decision.SubmissionID] = true
			index := -1
			for i, sub := range tx.State().Submissions {
				if sub.ID == decision.SubmissionID {
					if sub.Revision != decision.Revision || sub.Status != "review" || sub.Version != tx.State().Gate.Current || tx.State().Gate.State != "active" {
						return nil, ErrConflict
					}
					index = i
					break
				}
			}
			if index < 0 {
				return nil, ErrNotFound
			}
			indices = append(indices, index)
		}
		for i, index := range indices {
			sub := &tx.State().Submissions[index]
			sub.Status = "declined"
			if decisions[i].Admit {
				sub.Status = "admitted"
			}
			sub.Revision++
			sub.Reviewer = c.Actor.Person
			sub.Reason = reason
			if decisions[i].Admit {
				if e := tx.Membership(ctx, Actor{Tenant: c.Scope.Tenant, Person: sub.Person}, sub.Version, true); e != nil {
					return nil, e
				}
			}
			s.event(tx.State(), c, sub.Status, sub.Person, sub.ID, sub.Version)
		}
		return "reviewed", nil
	})
	return e
}
func (s *Service) ExportAllCSV(ctx context.Context, a Actor, scope Scope) (string, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"person", "field", "answer"})
	e := s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, "admin"); e != nil {
			return e
		}
		if e := s.access(ctx, a, scope, "export"); e != nil {
			return e
		}
		for _, sub := range tx.State().Submissions {
			if sub.Status == "withdrawn" || sub.Status == "superseded" {
				continue
			}
			values, e := s.read(ctx, tx, ReadRequest{Actor: a, Scope: scope, Person: sub.Person, Purpose: "Gate answers CSV export", Export: true})
			if e != nil {
				return e
			}
			ids := []string{}
			for id := range values {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				value := string(values[id])
				var text string
				if json.Unmarshal(values[id], &text) == nil {
					value = text
				}
				if len(value) > 0 && strings.ContainsAny(value[:1], "=+-@\t\r") {
					value = "'" + value
				}
				if e = w.Write([]string{sub.Person, id, value}); e != nil {
					return e
				}
			}
		}
		return nil
	})
	if e != nil {
		return "", e
	}
	w.Flush()
	return b.String(), w.Error()
}
