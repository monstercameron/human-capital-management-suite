package journeyclient

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	journeytransport "github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestTodo_REV_091_01_Integration drives a typed refusal through the real
// journey transport and the live client, and proves the message on the form
// is the one productui's refusal mapper produces for that violation -- the
// client holds no second table to disagree with it. The refusal used is the
// token-shaped business reason REV-095-02 added, so the typed reason also
// survives the transport's closed reason allowlist.
func TestTodo_REV_091_01_Integration(t *testing.T) {
	engine := &promotionRefusalWireEngine{refusal: &workspace.JourneyInputError{
		FieldPath: "reason", ReasonRef: workspace.JourneyReasonNotProse, Detail: "private: promotion_into_senior_hrbp",
	}}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(transport.Config{Verifier: promotionRefusalWireVerifier{}})))
	journeytransport.Register(srv, journeytransport.Dependencies{Engine: engine, RoleAccess: promotionRefusalRoleAccess{}})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { srv.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	h := newHarness(t)
	h.svc.workers[0].BasePay = "100.03"
	h.svc.workers[0].PayZone = "US-EAST"
	h.svc.workforce = testWorkforceOptions()
	h.app.svc = promotionRefusalWireService{Service: h.svc, wire: NewGRPCService(conn, "test-promotion-refusal")}
	h.app.Start(context.Background(), ProposalHref("omar-reyes"))
	h.awaitPage(t, "promotion form", proposalFor("omar-reyes"))
	h.app.Submit(ActionPropose, map[string]string{
		NameWorker: "omar-reyes", NameJobCode: "OPS-HRBP3", NameGrade: "P3",
		NameBase: "110.00", NameEffective: "2026-12-01", NameReason: "promotion_into_senior_hrbp",
	})
	page := h.awaitPage(t, "typed reason refusal", noticeTitled("That proposal is not valid"))

	want := productui.MapPromotionWireRefusal([]productui.PromotionRefusalViolation{
		{FieldPath: "reason", RuleRef: workspace.JourneyReasonNotProse},
	})[productui.PromotionFieldReason].Message(productui.ResolveProductLocale("en-US"))
	if want == "" {
		t.Fatal("productui has no message for the reason refusal")
	}
	reason, ok := fieldByID(page.Proposal.Form.Fields, FieldReason)
	if !ok || reason.Error != want {
		t.Fatalf("reason field error = %+v, want productui's %q", reason, want)
	}
	if reason.Value != "promotion_into_senior_hrbp" {
		t.Fatalf("the entered reason was not kept for correction: %q", reason.Value)
	}
	doc, err := journey.RenderToString(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `href="#`+FieldReason+`"`) {
		t.Fatalf("the refusal summary does not link to the reason field")
	}
	for _, secret := range []string{"private:", workspace.JourneyReasonNotProse, "manager-jane"} {
		if strings.Contains(doc, secret) {
			t.Errorf("typed refusal leaked %q", secret)
		}
	}
}
