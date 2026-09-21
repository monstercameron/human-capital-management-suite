package providerreceipts_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/providerreceipts"
)

func TestValidateRejectsEachBadField(t *testing.T) {
	tenant := uuid.New()
	cases := map[string]func(*providerreceipts.Receipt){
		"tenant_id":       func(r *providerreceipts.Receipt) { r.TenantID = uuid.Nil },
		"provider":        func(r *providerreceipts.Receipt) { r.Provider = "workday" },
		"event_id":        func(r *providerreceipts.Receipt) { r.EventID = " " },
		"event_id long":   func(r *providerreceipts.Receipt) { r.EventID = strings.Repeat("e", 201) },
		"event_type":      func(r *providerreceipts.Receipt) { r.EventType = "" },
		"change_ref":      func(r *providerreceipts.Receipt) { r.ChangeRef = "" },
		"change_ref long": func(r *providerreceipts.Receipt) { r.ChangeRef = strings.Repeat("c", 201) },
		"correlation_key": func(r *providerreceipts.Receipt) { r.CorrelationKey = "" },
		"outcome":         func(r *providerreceipts.Receipt) { r.Outcome = "DONE" },
		"reason":          func(r *providerreceipts.Receipt) { r.Reason = strings.Repeat("r", 2001) },
		"origin":          func(r *providerreceipts.Receipt) { r.Origin = "email" },
		"payload":         func(r *providerreceipts.Receipt) { r.Payload = nil },
		"payload_digest":  func(r *providerreceipts.Receipt) { r.PayloadDigest = "" },
		"received_at":     func(r *providerreceipts.Receipt) { r.ReceivedAt = time.Time{} },
	}
	valid := receipt(tenant, "evt-v", "payroll:v", t0)
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid receipt: %v", err)
	}
	for name, mutate := range cases {
		r := valid
		mutate(&r)
		err := r.Validate()
		if !errors.Is(err, providerreceipts.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
			continue
		}
		// Record refuses before touching the (nil) transaction.
		if _, err := (providerreceipts.Store{}).Record(context.Background(), nil, r); !errors.Is(err, providerreceipts.ErrInvalid) {
			t.Errorf("%s: Record err = %v", name, err)
		}
	}
	if _, _, err := (providerreceipts.Store{}).LatestForChange(context.Background(), nil, uuid.Nil, "x"); !errors.Is(err, providerreceipts.ErrInvalid) {
		t.Errorf("LatestForChange nil tenant err = %v", err)
	}
}

func TestValidateNewOutcomesOriginAndSecretIndex(t *testing.T) {
	base := receipt(uuid.New(), "evt-n", "payroll:n", t0)
	zero, big, neg := 0, providerreceipts.MaxSecretIndex+1, -1
	for name, tc := range map[string]struct {
		mutate func(*providerreceipts.Receipt)
		ok     bool
	}{
		"reversed":             {func(r *providerreceipts.Receipt) { r.Outcome = providerreceipts.OutcomeReversed }, true},
		"revoked":              {func(r *providerreceipts.Receipt) { r.Outcome = providerreceipts.OutcomeRevoked }, true},
		"status poll":          {func(r *providerreceipts.Receipt) { r.Origin = providerreceipts.OriginStatusPoll }, true},
		"webhook secret index": {func(r *providerreceipts.Receipt) { r.SecretIndex = &zero }, true},
		"secret index on poll": {func(r *providerreceipts.Receipt) { r.Origin, r.SecretIndex = providerreceipts.OriginStatusPoll, &zero }, false},
		"secret index on rejection": {func(r *providerreceipts.Receipt) {
			r.Origin, r.SecretIndex = providerreceipts.OriginDeliveryRejection, &zero
		}, false},
		"negative secret index": {func(r *providerreceipts.Receipt) { r.SecretIndex = &neg }, false},
		"huge secret index":     {func(r *providerreceipts.Receipt) { r.SecretIndex = &big }, false},
	} {
		r := base
		tc.mutate(&r)
		if err := r.Validate(); (err == nil) != tc.ok || (err != nil && !errors.Is(err, providerreceipts.ErrInvalid)) {
			t.Errorf("%s: err = %v, want ok=%v", name, err, tc.ok)
		}
	}
	store := providerreceipts.Store{}
	for name, outcomes := range map[string][]string{"none": nil, "unknown": {"APPLIED", "DONE"}} {
		if _, _, err := store.LatestForChangeByKind(context.Background(), nil, uuid.New(), "c", outcomes...); !errors.Is(err, providerreceipts.ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, _, err := store.LatestForChangeByKind(context.Background(), nil, uuid.New(), " ", "APPLIED"); !errors.Is(err, providerreceipts.ErrInvalid) {
		t.Errorf("blank change ref: err = %v", err)
	}
}
