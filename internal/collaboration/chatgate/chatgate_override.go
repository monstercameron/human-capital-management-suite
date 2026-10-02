package chatgate

import (
	"context"
	"strings"
)

// Override is an explicit administrator action; it records a reason and does
// not bypass ordinary channel eligibility in the membership transaction port.
func (s *Service) Override(ctx context.Context, c Command, person, reason string) error {
	_, e := s.mutate(ctx, c, "admin", []string{person, reason}, func(tx Transaction) (any, error) {
		if strings.TrimSpace(person) == "" || strings.TrimSpace(reason) == "" {
			return nil, ErrInvalid
		}
		st := tx.State()
		st.Overrides = append(st.Overrides, AdmissionOverride{Person: person, Administrator: c.Actor.Person, Version: st.Gate.Current, Reason: reason, At: s.now()})
		if e := tx.Membership(ctx, Actor{Tenant: c.Scope.Tenant, Person: person}, st.Gate.Current, true); e != nil {
			return nil, e
		}
		s.event(st, c, "administrator_override", person, "", st.Gate.Current)
		return "admitted", nil
	})
	return e
}
