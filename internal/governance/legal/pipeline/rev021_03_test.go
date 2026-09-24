package pipeline

import (
	"context"
	"errors"
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

type rev02103Authority struct {
	keys map[string][]byte
}

func (a rev02103Authority) AuthorizeLegalPrincipal(_ context.Context, tenantID, principalID string, role legal.SigningRole) ([]byte, error) {
	if tenantID != "tenant-1" {
		return nil, errors.New("tenant is not in scope")
	}
	key, ok := a.keys[principalID+":"+string(role)]
	if !ok {
		return nil, errors.New("principal lacks requested legal role")
	}
	return append([]byte(nil), key...), nil
}

func rev02103Keys(author, counsel, publisher *legal.Signer) rev02103Authority {
	return rev02103Authority{keys: map[string][]byte{
		"author:" + string(legal.SigningRoleRuleAuthor):                   author.PublicKey(),
		"counsel:" + string(legal.SigningRoleCustomerCounsel):             counsel.PublicKey(),
		"publisher:" + string(legal.SigningRoleReleasePublisher):          publisher.PublicKey(),
		"vendor-reviewer:" + string(legal.SigningRoleVendorLegalReviewer): counsel.PublicKey(),
	}}
}

func rev02103Finding() []legal.ReviewFinding {
	return []legal.ReviewFinding{{ObligationID: "us-wa-minimum-wage-floor", Severity: legal.FindingSeverityInfo, Note: "confirmed against the cited RCW section"}}
}

// TestTodo_REV_021_03 proves tenant-scoped counsel can review a typed
// obligation and promote its pack through a signed, verifiable release.
func TestTodo_REV_021_03(t *testing.T) {
	authorSigner := fixedSigner(t, 0x61)
	counselSigner := fixedSigner(t, 0x62)
	publisherSigner := fixedSigner(t, 0x63)
	authority := rev02103Keys(authorSigner, counselSigner, publisherSigner)
	p, err := AuthorAuthorized(context.Background(), "tenant-1", "author", waMinimumWageDefinitionJSON(), authorSigner, authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ReviewAuthorized(context.Background(), "tenant-1", "counsel", legal.ReviewStatusCustomerDefined, rev02103Finding(), counselSigner, authority); err != nil {
		t.Fatalf("ReviewAuthorized: %v", err)
	}
	if p.Stage != StageReviewed || p.ReviewRecord.Status != legal.ReviewStatusCustomerDefined || p.Events[1].ArtifactDigest != p.ReviewRecord.ComputeDigest() {
		t.Fatalf("review evidence does not capture promotion: stage=%s record=%+v event=%+v", p.Stage, p.ReviewRecord, p.Events[1])
	}
	registry := legal.NewRegistry()
	release, err := p.PublishAuthorized(context.Background(), "tenant-1", "publisher", publisherSigner, "counsel", counselSigner, authority, registry)
	if err != nil {
		t.Fatalf("PublishAuthorized: %v", err)
	}
	if release.ReviewStatus != legal.ReviewStatusCustomerDefined || len(release.Signatures) != 2 {
		t.Fatalf("promoted release = status %s with %d signatures; want CUSTOMER_DEFINED and publisher+counsel", release.ReviewStatus, len(release.Signatures))
	}
	roles := map[legal.SigningRole]bool{}
	for _, signature := range release.Signatures {
		roles[signature.Role] = true
	}
	if !roles[legal.SigningRoleReleasePublisher] || !roles[legal.SigningRoleCustomerCounsel] {
		t.Fatalf("release signatures = %v", roles)
	}
	if err := p.VerifyChain(); err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if got, err := registry.GetExact(release.Release()); err != nil || got.Digest != release.Digest {
		t.Fatalf("registry does not contain the promoted release: %v", err)
	}

	baseline, err := Author(waMinimumWageDefinitionJSON(), "author", authorSigner)
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.ReviewAuthorized(context.Background(), "tenant-1", "vendor-reviewer", legal.ReviewStatusVendorBaseline, rev02103Finding(), counselSigner, authority); err != nil {
		t.Fatalf("vendor baseline ReviewAuthorized: %v", err)
	}
	baselineRelease, err := baseline.PublishAuthorized(context.Background(), "tenant-1", "publisher", publisherSigner, "", nil, authority, legal.NewRegistry())
	if err != nil {
		t.Fatalf("vendor baseline PublishAuthorized: %v", err)
	}
	if baselineRelease.ReviewStatus != legal.ReviewStatusVendorBaseline || len(baselineRelease.Signatures) != 1 || baselineRelease.Signatures[0].Role != legal.SigningRoleReleasePublisher {
		t.Fatalf("vendor baseline release = status %s signatures %+v", baselineRelease.ReviewStatus, baselineRelease.Signatures)
	}
	if err := baseline.VerifyChain(); err != nil {
		t.Fatalf("vendor baseline VerifyChain: %v", err)
	}
}

// TestTodo_REV_021_03_Security proves authorization, review findings and
// signed draft evidence each gate promotion before a transition is recorded.
func TestTodo_REV_021_03_Security(t *testing.T) {
	authorSigner := fixedSigner(t, 0x71)
	counselSigner := fixedSigner(t, 0x72)
	otherSigner := fixedSigner(t, 0x73)
	publisherSigner := fixedSigner(t, 0x74)
	authority := rev02103Keys(authorSigner, counselSigner, publisherSigner)
	newDraft := func() *Pipeline {
		t.Helper()
		p, err := Author(waMinimumWageDefinitionJSON(), "author", authorSigner)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("wrong tenant and untrusted signing key are refused", func(t *testing.T) {
		p := newDraft()
		if err := p.ReviewAuthorized(context.Background(), "tenant-2", "counsel", legal.ReviewStatusCustomerDefined, nil, counselSigner, authority); !errors.Is(err, ErrPrincipalUnauthorized) {
			t.Fatalf("wrong tenant error = %v", err)
		}
		if err := p.ReviewAuthorized(context.Background(), "tenant-1", "counsel", legal.ReviewStatusCustomerDefined, nil, otherSigner, authority); !errors.Is(err, ErrSignerPrincipal) {
			t.Fatalf("wrong signer error = %v", err)
		}
		if p.Stage != StageAuthored || len(p.Events) != 1 {
			t.Fatalf("refused review mutated pipeline: stage=%s events=%d", p.Stage, len(p.Events))
		}
	})

	t.Run("blocking and unknown-obligation findings prevent promotion", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			finding legal.ReviewFinding
			want    error
		}{
			{"blocking", legal.ReviewFinding{ObligationID: "us-wa-minimum-wage-floor", Severity: legal.FindingSeverityBlocking, Note: "cannot confirm source"}, ErrBlockingFinding},
			{"unknown obligation", legal.ReviewFinding{ObligationID: "forged-obligation", Severity: legal.FindingSeverityInfo, Note: "reviewed"}, ErrFindingObligation},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := newDraft()
				err := p.ReviewAuthorized(context.Background(), "tenant-1", "counsel", legal.ReviewStatusCustomerDefined, []legal.ReviewFinding{tc.finding}, counselSigner, authority)
				if !errors.Is(err, tc.want) {
					t.Fatalf("review error = %v, want %v", err, tc.want)
				}
				if p.Stage != StageAuthored || len(p.Events) != 1 {
					t.Fatalf("refused finding mutated pipeline: stage=%s events=%d", p.Stage, len(p.Events))
				}
			})
		}
	})

	t.Run("every obligation requires typed review evidence", func(t *testing.T) {
		p := newDraft()
		err := p.ReviewAuthorized(context.Background(), "tenant-1", "counsel", legal.ReviewStatusCustomerDefined, nil, counselSigner, authority)
		if !errors.Is(err, ErrFindingMissing) {
			t.Fatalf("missing finding error = %v", err)
		}
		if p.Stage != StageAuthored || len(p.Events) != 1 {
			t.Fatalf("review without typed evidence mutated pipeline: stage=%s events=%d", p.Stage, len(p.Events))
		}
	})

	t.Run("draft mutation after author signature is refused", func(t *testing.T) {
		p := newDraft()
		p.Draft.Definition.Obligations[0].Body.FloorAmount.Amount = "99.99"
		err := p.ReviewAuthorized(context.Background(), "tenant-1", "counsel", legal.ReviewStatusCustomerDefined, rev02103Finding(), counselSigner, authority)
		if !errors.Is(err, ErrDraftChanged) {
			t.Fatalf("review changed draft error = %v", err)
		}
		if p.Stage != StageAuthored || len(p.Events) != 1 {
			t.Fatalf("refused changed draft mutated pipeline: stage=%s events=%d", p.Stage, len(p.Events))
		}
	})

	t.Run("publisher and counsel must be independently authorized", func(t *testing.T) {
		p := newDraft()
		if err := p.ReviewAuthorized(context.Background(), "tenant-1", "counsel", legal.ReviewStatusCustomerDefined, rev02103Finding(), counselSigner, authority); err != nil {
			t.Fatal(err)
		}
		if _, err := p.PublishAuthorized(context.Background(), "tenant-1", "publisher", otherSigner, "counsel", counselSigner, authority, legal.NewRegistry()); !errors.Is(err, ErrSignerPrincipal) {
			t.Fatalf("wrong publisher key error = %v", err)
		}
		if _, err := p.PublishAuthorized(context.Background(), "tenant-1", "publisher", publisherSigner, "other", otherSigner, authority, legal.NewRegistry()); !errors.Is(err, ErrNotPublishable) {
			t.Fatalf("different counsel principal error = %v", err)
		}
		if p.Stage != StageReviewed || len(p.Events) != 2 {
			t.Fatalf("refused publication mutated pipeline: stage=%s events=%d", p.Stage, len(p.Events))
		}
	})

	t.Run("tampered review artifact breaks evidence verification", func(t *testing.T) {
		p := newDraft()
		if err := p.ReviewAuthorized(context.Background(), "tenant-1", "counsel", legal.ReviewStatusCustomerDefined, rev02103Finding(), counselSigner, authority); err != nil {
			t.Fatal(err)
		}
		p.ReviewRecord.Findings = []legal.ReviewFinding{{Severity: legal.FindingSeverityConcern}}
		if err := p.VerifyChain(); !errors.Is(err, ErrChainBroken) {
			t.Fatalf("tampered review record verification error = %v", err)
		}
	})
}

// TestTodo_REV_021_03_Golden pins the deterministic review, release and
// promotion-event digests for the customer-defined path.
func TestTodo_REV_021_03_Golden(t *testing.T) {
	const (
		wantReviewDigest  = "c4d840e6fb77c15208144210b63a4cb4d93361ec2d9f4347b686c893619ef23b"
		wantReviewEvent   = "d3d4172d08a78fdec216999c8c9398f990f58b567dc44deab153941626c89398"
		wantReleaseDigest = "5b4c97629af754e5f7262e1eb82690935173f46796de29de096f5004a6ef2490"
		wantPublishEvent  = "539a6f3e4b7644189976dcc2980654f9b8f692b39e5e0e7f2b476ddf6b41e195"
	)
	p, err := Author(waMinimumWageDefinitionJSON(), "author", fixedSigner(t, 0x81))
	if err != nil {
		t.Fatal(err)
	}
	findings := []legal.ReviewFinding{{ObligationID: "us-wa-minimum-wage-floor", Severity: legal.FindingSeverityInfo, Note: "confirmed against the cited RCW section"}}
	if err := p.Review("counsel", legal.ReviewStatusCustomerDefined, findings, fixedSigner(t, 0x82)); err != nil {
		t.Fatal(err)
	}
	release, err := p.Publish("publisher", fixedSigner(t, 0x83), "counsel", fixedSigner(t, 0x82), legal.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if got := p.ReviewRecord.ComputeDigest(); got != wantReviewDigest {
		t.Errorf("review digest = %s, want %s", got, wantReviewDigest)
	}
	if got := p.Events[1].Digest; got != wantReviewEvent {
		t.Errorf("review event digest = %s, want %s", got, wantReviewEvent)
	}
	if got := release.Digest; got != wantReleaseDigest {
		t.Errorf("release digest = %s, want %s", got, wantReleaseDigest)
	}
	if got := p.Events[2].Digest; got != wantPublishEvent {
		t.Errorf("publish event digest = %s, want %s", got, wantPublishEvent)
	}
	if err := p.VerifyChain(); err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
}
