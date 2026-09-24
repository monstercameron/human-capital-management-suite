package industrypack

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
)

func authorityForIndustry(t *testing.T, industry Industry) *IndustryEntitlementAuthority {
	t.Helper()
	return authorityFor(t, industry, "acme", commercial.StatusActive, []string{IndustryEntitlementCapability(industry)},
		publishNow.Add(-24*time.Hour), publishNow.Add(24*time.Hour), publishNow, 24*time.Hour)
}

func authorityFor(t *testing.T, industry Industry, tenant string, status commercial.ContractStatus, capabilities []string, from, to, now time.Time, maxAge time.Duration) *IndustryEntitlementAuthority {
	t.Helper()
	ctx := context.Background()
	contract := commercial.FixedPricePilotContract{
		TenantID: tenant, ContractID: "contract-1", Revision: 1, EffectiveFrom: from, EffectiveTo: to,
		Status: status, Capabilities: append([]string(nil), capabilities...), Bound: commercial.EntitlementBound{Seats: 10}, PriceCents: 1000, Currency: "USD",
	}
	store := commercial.NewMemoryStore()
	if err := store.PutContractRevision(ctx, contract); err != nil {
		t.Fatalf("store contract revision: %v", err)
	}
	snapshot, err := commercial.NewEntitlementSnapshot(contract)
	if err != nil {
		t.Fatalf("create stored entitlement snapshot: %v", err)
	}
	if err := store.PutEntitlementSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("store entitlement snapshot: %v", err)
	}
	authority, err := NewIndustryEntitlementAuthority(store, ContractRevisionResolverFunc(func(_ context.Context, gotTenant string) (ContractRevisionRef, error) {
		if gotTenant != tenant {
			return ContractRevisionRef{}, fmt.Errorf("tenant has no selected contract")
		}
		return ContractRevisionRef{ContractID: "contract-1", Revision: 1}, nil
	}), EntitlementClockFunc(func() time.Time { return now }), maxAge)
	if err != nil {
		t.Fatalf("compose entitlement authority: %v", err)
	}
	return authority
}

func entitlementRefusalState(t *testing.T, err error, want string) {
	t.Helper()
	var refusal *EntitlementRejection
	if !errors.As(err, &refusal) || !errors.Is(err, ErrEntitlementRejected) || refusal.Code != EntitlementRejectionCode || refusal.State != want {
		t.Fatalf("error = %v, want entitlement refusal %s", err, want)
	}
}

func TestTodo_REV_047_02(t *testing.T) {
	ctx := context.Background()
	authority := authorityForIndustry(t, IndustryHealthcare)
	check := publicationCheck(t)
	report, err := CheckPublication(ctx, authority, check)
	if err != nil || report.Entitlement.Fingerprint() == "" {
		t.Fatalf("publication = %+v, %v", report, err)
	}
	activation := activationRequest()
	receipt, effects, err := ActivateSigned(ctx, &countingActivations{}, authority, activation)
	if err != nil || receipt.Entitlement.Fingerprint() == "" || receipt.Entitlement.Fingerprint() != report.Entitlement.Fingerprint() || effects.AuthoritativeRows != 1 {
		t.Fatalf("activation = %+v, %+v, %v", receipt, effects, err)
	}
	if receipt.Digest == "" || receipt.Industry != IndustryHealthcare {
		t.Fatalf("receipt does not pin accepted industry entitlement: %+v", receipt)
	}
}

func TestTodo_REV_047_02_Security(t *testing.T) {
	cases := []struct {
		name     string
		status   commercial.ContractStatus
		caps     []string
		from, to time.Time
		state    string
	}{
		{"sku absent", commercial.StatusActive, []string{"hcmnext.unrelated/v1"}, publishNow.Add(-time.Hour), publishNow.Add(time.Hour), EntitlementSKUAbsent},
		{"suspended", commercial.StatusSuspended, []string{IndustryEntitlementCapability(IndustryHealthcare)}, publishNow.Add(-time.Hour), publishNow.Add(time.Hour), EntitlementSuspended},
		{"expired", commercial.StatusActive, []string{IndustryEntitlementCapability(IndustryHealthcare)}, publishNow.Add(-time.Hour), publishNow, EntitlementExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authority := authorityFor(t, IndustryHealthcare, "acme", tc.status, tc.caps, tc.from, tc.to, publishNow, 24*time.Hour)
			check := publicationCheck(t)
			_, err := CheckPublication(context.Background(), authority, check)
			entitlementRefusalState(t, err, tc.state)
			publicationStore := &countingPublication{}
			_, publicationEffects, err := PublishChecked(context.Background(), publicationStore, authority, "acme", check)
			entitlementRefusalState(t, err, tc.state)
			if publicationStore.calls != 0 || publicationEffects != (ActivationEffects{}) {
				t.Fatal("refused publication persisted")
			}
			activationStore := &countingActivations{}
			_, effects, err := ActivateSigned(context.Background(), activationStore, authority, activationRequest())
			entitlementRefusalState(t, err, tc.state)
			if activationStore.calls != 0 || effects != (ActivationEffects{}) {
				t.Fatal("refused activation persisted")
			}

			industryStore := &countingIndustryActivations{}
			spec := industryComposition(t, IndustryHealthcare, "healthcare", "credential-promotion", "license-verification")
			_, _, effects, err = ActivateIndustry(context.Background(), industryStore, authority, spec, activationRequest())
			wantConformanceRejection(t, err, "publication", "commercial_entitlement", tc.state, "")
			if industryStore.calls != 0 || effects != (ActivationEffects{}) {
				t.Fatal("refused composed industry activation persisted")
			}
		})
	}

	// A target tenant cannot borrow an entitled composition from another
	// tenant: activation resolves that target's own contract reference.
	spec := industryComposition(t, IndustryHealthcare, "healthcare", "credential-promotion", "license-verification")
	authority := authorityForIndustry(t, IndustryHealthcare)
	composed, err := ComposeIndustry(context.Background(), authority, spec)
	if err != nil {
		t.Fatal(err)
	}
	activation := signedFor(composed)
	activation.Target.Tenant = "other"
	activation.Envelope.Target = activation.Target
	activation.Envelope = SignPackVersion(activation.Envelope, "pack-2026", publishKey)
	store := &countingIndustryActivations{}
	_, _, effects, err := ActivateIndustry(context.Background(), store, authority, spec, activation)
	wantConformanceRejection(t, err, "activation", "commercial_entitlement", EntitlementSnapshotAbsent, "")
	if store.calls != 0 || effects != (ActivationEffects{}) {
		t.Fatal("activation for another tenant persisted")
	}
}

func TestTodo_REV_047_02_Mutation(t *testing.T) {
	ctx := context.Background()
	store := &countingActivations{}
	authority := authorityForIndustry(t, IndustryHealthcare)
	req := activationRequest()
	req.Envelope.Industry = IndustryRetail
	if _, effects, err := ActivateSigned(ctx, store, authority, req); err == nil || store.calls != 0 || effects != (ActivationEffects{}) {
		t.Fatalf("industry tampering accepted: effects=%+v calls=%d err=%v", effects, store.calls, err)
	}

	// The resolver loads only the stored contract revision, so a locally
	// fabricated, entitled snapshot cannot replace a persisted SKU-absent one.
	noSKU := authorityFor(t, IndustryHealthcare, "acme", commercial.StatusActive, []string{"hcmnext.unrelated/v1"},
		publishNow.Add(-time.Hour), publishNow.Add(time.Hour), publishNow, 24*time.Hour)
	fabricated, err := commercial.NewEntitlementSnapshot(commercial.FixedPricePilotContract{
		TenantID: "acme", ContractID: "contract-evil", Revision: 1, EffectiveFrom: publishNow.Add(-time.Hour), EffectiveTo: publishNow.Add(time.Hour),
		Status: commercial.StatusActive, Capabilities: []string{IndustryEntitlementCapability(IndustryHealthcare)}, Bound: commercial.EntitlementBound{Seats: 10}, PriceCents: 1000, Currency: "USD",
	})
	if err != nil || fabricated.Fingerprint() == "" {
		t.Fatalf("fabrication fixture: %v", err)
	}
	if _, _, err := ActivateSigned(ctx, store, noSKU, activationRequest()); err == nil || store.calls != 0 {
		t.Fatalf("local snapshot influenced admission: calls=%d err=%v", store.calls, err)
	}

	// The request carries no clock value. The authority's clock is after the
	// stored entitlement window, even though the signed envelope is otherwise valid.
	expired := authorityFor(t, IndustryHealthcare, "acme", commercial.StatusActive,
		[]string{IndustryEntitlementCapability(IndustryHealthcare)}, publishNow.Add(-time.Hour), publishNow, publishNow, 24*time.Hour)
	if _, _, err := ActivateSigned(ctx, store, expired, activationRequest()); err == nil || store.calls != 0 {
		t.Fatalf("expired stored entitlement accepted: calls=%d err=%v", store.calls, err)
	}

	// The product industry is covered by the publisher's signature.
	valid := activationRequest()
	if valid.Envelope.Industry != IndustryHealthcare || !ed25519.Verify(publishKey.Public().(ed25519.PublicKey), valid.Envelope.SigningPayload(), valid.Envelope.Signature) {
		t.Fatal("activation request lost signed industry binding")
	}
}
