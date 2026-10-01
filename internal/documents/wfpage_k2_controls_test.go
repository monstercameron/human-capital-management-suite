package documents

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/documents/intake"
	"github.com/monstercameron/human-capital-management-suite/internal/documentsecurity"
)

type wfpageK2Scanner func(context.Context, documentsecurity.ScanInput, documentsecurity.Limits) (documentsecurity.Verdict, error)

func (f wfpageK2Scanner) Scan(ctx context.Context, input documentsecurity.ScanInput, limits documentsecurity.Limits) (documentsecurity.Verdict, error) {
	return f(ctx, input, limits)
}

func TestTodo_WFPAGE_014(t *testing.T) {
	registry := documentsecurity.NewRegistry()
	limits := documentsecurity.Limits{MaxBytes: 1024, MaxDerivativeBytes: 1024, MaxCompressionRatio: 10}
	_, err := registry.Scan(context.Background(), documentsecurity.Upload{ID: "offer", Name: "offer.pdf", ContentType: "application/pdf", Bytes: []byte("offer")}, limits, wfpageK2Scanner(func(context.Context, documentsecurity.ScanInput, documentsecurity.Limits) (documentsecurity.Verdict, error) {
		return documentsecurity.Verdict{State: documentsecurity.Safe, Scanner: "av", ScannerVersion: "1", Derivative: []byte("%PDF-1.7\n safe")}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	attachment, err := AdmitAttachment(intake.NewService(registry), AttachmentRequest{ArtifactID: "offer", Purpose: "workflow.offer", SubjectID: "worker-1", RequestedBy: "hr-1"})
	if err != nil || attachment.Evidence.ID == "" || attachment.DocumentType != "PDF" {
		t.Fatalf("attachment = %+v, %v", attachment, err)
	}
	ceremony, err := BeginSignature(SignatureRequest{ID: "ack", ArtifactHash: "sha256:offer", SignerID: "worker-1", AssuranceLevel: AssuranceEnhanced, TrustedNow: 1, ExpiresAt: 10})
	if err != nil {
		t.Fatal(err)
	}
	if ceremony.AssuranceLabel() != "ENHANCED" || ceremony.Validate() != nil {
		t.Fatalf("signature = %+v", ceremony)
	}
}

func TestTodo_WFPAGE_014_Browser(t *testing.T) {
	if !AssuranceQualified.valid() {
		t.Fatal("qualified assurance must be renderable")
	}
	if _, err := BeginSignature(SignatureRequest{ID: "x", ArtifactHash: "h", SignerID: "s", AssuranceLevel: "forged", TrustedNow: 1, ExpiresAt: 2}); !errors.Is(err, ErrAssuranceLevel) {
		t.Fatalf("invalid assurance = %v", err)
	}
}

func TestTodo_WFPAGE_014_Security(t *testing.T) {
	registry := documentsecurity.NewRegistry()
	limits := documentsecurity.Limits{MaxBytes: 100, MaxDerivativeBytes: 100, MaxCompressionRatio: 10}
	_, _ = registry.Scan(context.Background(), documentsecurity.Upload{ID: "unsafe", Bytes: []byte("x")}, limits, wfpageK2Scanner(func(context.Context, documentsecurity.ScanInput, documentsecurity.Limits) (documentsecurity.Verdict, error) {
		return documentsecurity.Verdict{State: documentsecurity.Unsafe, Scanner: "av", ScannerVersion: "1"}, nil
	}))
	if _, err := AdmitAttachment(intake.NewService(registry), AttachmentRequest{ArtifactID: "unsafe", Purpose: "x", SubjectID: "w", RequestedBy: "a"}); !errors.Is(err, intake.ErrNotReleasable) {
		t.Fatalf("unsafe attachment admitted: %v", err)
	}
	ceremony, err := BeginSignature(SignatureRequest{ID: "ack", ArtifactHash: "sha256:offer", SignerID: "worker-1", AssuranceLevel: AssuranceBasic, TrustedNow: 1, ExpiresAt: 10})
	if err != nil {
		t.Fatal(err)
	}
	ceremony.Ceremony.ProviderReceipt = "forged"
	if ceremony.Validate() == nil {
		t.Fatal("mutated ceremony accepted")
	}
}
