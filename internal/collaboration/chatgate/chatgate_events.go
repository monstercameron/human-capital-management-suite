package chatgate

import (
	"context"
	"encoding/json"
)

// ConsumerDelivery is the integration boundary. External implementations sign
// the integration framework's webhook envelope and deduplicate by Event.ID.
type ConsumerDelivery interface {
	DeliverSignedGateEvent(context.Context, ConsumerEnvelope) error
}
type ConsumerEnvelope struct {
	Event                       Event
	ConsumerID, ConsumerVersion string
	Values                      map[string]json.RawMessage
}
type UnavailableConsumerDelivery struct{}

func (UnavailableConsumerDelivery) DeliverSignedGateEvent(context.Context, ConsumerEnvelope) error {
	return ErrUnavailable
}

// DeliverEvent resolves durable routing and reads only declared mapped fields.
func (s *Service) DeliverEvent(ctx context.Context, a Actor, scope Scope, eventID, consumerID, consumerVersion string, delivery ConsumerDelivery) error {
	if delivery == nil {
		return ErrUnavailable
	}
	var envelope ConsumerEnvelope
	e := s.Repository.Transact(ctx, scope, func(tx Transaction) error {
		if e := s.access(ctx, a, scope, "consumer:"+consumerID); e != nil {
			return e
		}
		descriptor, ok := s.Registry.Consumer(consumerID, consumerVersion)
		if !ok {
			return ErrDenied
		}
		var installed *Installation
		for i := range tx.State().Gate.Installations {
			in := &tx.State().Gate.Installations[i]
			if in.ConsumerID == consumerID && in.Version == consumerVersion {
				installed = in
				break
			}
		}
		if installed == nil {
			return ErrDenied
		}
		var event *Event
		for i := range tx.State().Events {
			x := &tx.State().Events[i]
			if x.ID == eventID {
				event = x
				break
			}
		}
		if event == nil || event.Tenant != scope.Tenant || event.Conversation != scope.Conversation {
			return ErrNotFound
		}
		envelope = ConsumerEnvelope{Event: *event, ConsumerID: descriptor.ID, ConsumerVersion: descriptor.Version, Values: map[string]json.RawMessage{}}
		if event.SubmissionID == "" || event.Kind == "withdrawn" || event.Kind == "expired" {
			return nil
		}
		fields := []string{}
		for _, field := range installed.Mapping {
			if !contains(fields, field) {
				fields = append(fields, field)
			}
		}
		values, e := s.read(ctx, tx, ReadRequest{Actor: a, Scope: scope, Person: event.Person, Consumer: consumerID, ConsumerVersion: consumerVersion, Purpose: "Gate event " + event.Kind, Fields: fields, SubmissionID: event.SubmissionID})
		if e != nil {
			return e
		}
		envelope.Values = values
		return nil
	})
	if e != nil {
		return e
	}
	return delivery.DeliverSignedGateEvent(ctx, envelope)
}

// Example extension: register Consumer{ID:"welcome",Version:"1.0.0",
// Kinds:[]string{"single_choice"},Effect:"post a channel welcome",
// Permission:"chat.manage_agents"}; declare Visibility.Consumers=["welcome"]
// on the selected field; Install with Mapping={"team":"team_question"}.
// The outbox worker calls DeliverEvent with its installed consumer identity.
// Adding that adapter changes neither gate evaluation nor answer visibility.
