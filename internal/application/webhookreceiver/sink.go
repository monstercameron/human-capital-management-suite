// Package webhookreceiver composes the HTTP receiver's port with its durable
// tenant scoped inbox store.
package webhookreceiver

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerreceipt"
	corewebhook "github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/webhookreceipts"
	transportwebhook "github.com/monstercameron/human-capital-management-suite/internal/transport/webhook"
)

type receiptStore interface {
	Record(context.Context, providerreceipt.Parsed, time.Time) (bool, error)
	Replay(context.Context, string, corewebhook.ReplayApproval) (providerreceipt.Parsed, error)
}

// Sink is the application adapter from transport callbacks to durable inbox.
type Sink struct{ store receiptStore }

// NewSink binds an endpoint to durable receipt storage.
func NewSink(db dbport.Beginner, scope webhookreceipts.Scope) (*Sink, error) {
	store, err := webhookreceipts.New(db, scope)
	if err != nil {
		return nil, err
	}
	return &Sink{store: store}, nil
}

func (s *Sink) Accept(ctx context.Context, parsed providerreceipt.Parsed, at time.Time) (transportwebhook.Disposition, error) {
	if s == nil || s.store == nil {
		return "", errors.New("webhookreceiver: sink unavailable")
	}
	duplicate, err := s.store.Record(ctx, parsed, at)
	if err != nil {
		return "", err
	}
	if duplicate {
		return transportwebhook.DispositionDuplicate, nil
	}
	return transportwebhook.DispositionAccepted, nil
}

func (s *Sink) Replay(ctx context.Context, eventID string, approval corewebhook.ReplayApproval) (providerreceipt.Parsed, error) {
	if s == nil || s.store == nil {
		return providerreceipt.Parsed{}, errors.New("webhookreceiver: sink unavailable")
	}
	return s.store.Replay(ctx, eventID, approval)
}

var _ transportwebhook.Sink = (*Sink)(nil)
