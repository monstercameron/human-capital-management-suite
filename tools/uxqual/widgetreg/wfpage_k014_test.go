package widgetreg

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/documents"
	"github.com/monstercameron/human-capital-management-suite/internal/documents/intake"
	"github.com/monstercameron/human-capital-management-suite/internal/documents/signing"
	"github.com/monstercameron/human-capital-management-suite/internal/documentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

// TestTodo_WFPAGE_014 proves the four Phase-3 widgets render through the
// closed registry: attachments project quarantine admission, signatures
// project the ceremony with its assurance level, repeating groups project
// rows with per-row errors under a cap, and computed fields project a
// server-recalculated read-only value.
func TestTodo_WFPAGE_014(t *testing.T) {
	t.Run("AttachmentRendersAdmission", func(t *testing.T) {
		r := NewWorkflowInputRegistry()
		view, err := r.Render(InputRenderRequest{
			Widget: pagedef.WorkflowPageWidget{ID: "offer", Kind: pagedef.WorkflowWidgetAttachment,
				Binding: "offer_letter", Label: "Offer letter", Accept: "pdf,png"},
			Locale: "en-US",
		})
		if err != nil {
			t.Fatalf("Render attachment: %v", err)
		}
		if view.Role == "" || view.InputType != "file" {
			t.Fatalf("attachment projection = %+v", view)
		}
		if len(view.Accept) != 2 || view.Accept[0] != "pdf" || view.Accept[1] != "png" {
			t.Fatalf("accept list = %v", view.Accept)
		}
		if view.LabelID == "" || view.ErrorID == "" {
			t.Fatalf("attachment lacks accessible associations: %+v", view)
		}
	})

	t.Run("SignatureRendersAssurance", func(t *testing.T) {
		r := NewWorkflowInputRegistry()
		view, err := r.Render(InputRenderRequest{
			Widget: pagedef.WorkflowPageWidget{ID: "ack", Kind: pagedef.WorkflowWidgetSignature,
				Binding: "acknowledgement", Label: "Acknowledgement", Assurance: "ENHANCED"},
			Locale: "de-DE",
		})
		if err != nil {
			t.Fatalf("Render signature: %v", err)
		}
		if view.Assurance != "ENHANCED" {
			t.Fatalf("assurance label = %q", view.Assurance)
		}
		if view.LabelID == "" || view.DescriptionID == "" {
			t.Fatalf("signature lacks accessible associations: %+v", view)
		}
	})

	t.Run("ComputedRecalculates", func(t *testing.T) {
		r := NewWorkflowInputRegistry()
		view, err := r.Render(InputRenderRequest{
			Widget: pagedef.WorkflowPageWidget{ID: "total", Kind: pagedef.WorkflowWidgetComputed,
				Binding: "total", Label: "Total", ComputedOp: pagedef.ComputedOpSum,
				ComputedInputs: []string{"base", "bonus"}},
			Locale: "en-US",
			Values: map[string]string{"base": "1200.00", "bonus": "300.50"},
		})
		if err != nil {
			t.Fatalf("Render computed: %v", err)
		}
		if !view.ReadOnly {
			t.Fatal("computed field is editable")
		}
		if view.ComputedValue != "1500.50" {
			t.Fatalf("computed value = %q", view.ComputedValue)
		}
	})

	t.Run("RepeatingGroupRows", func(t *testing.T) {
		r := NewWorkflowInputRegistry()
		group := pagedef.WorkflowPageWidget{ID: "contacts", Kind: pagedef.WorkflowWidgetRepeatingGroup,
			Binding: "emergency_contacts", Label: "Emergency contacts", RowCap: 2,
			RowWidgets: []pagedef.WorkflowPageWidget{
				{ID: "name", Kind: pagedef.WorkflowWidgetText, Binding: "name", Label: "Name"},
				{ID: "phone", Kind: pagedef.WorkflowWidgetText, Binding: "phone", Label: "Phone"},
			}}
		view, err := r.Render(InputRenderRequest{
			Widget: group, Locale: "en-US",
			RepeatingRows: []RepeatingRowInput{
				{Values: map[string]string{"name": "Ann", "phone": "1"}},
				{Values: map[string]string{"name": "", "phone": "2"}, Errors: map[string]string{"name": "required"}},
			},
		})
		if err != nil {
			t.Fatalf("Render repeating: %v", err)
		}
		if view.RowCap != 2 || view.CanAdd {
			t.Fatalf("at cap 2 with 2 rows, CanAdd must be false: %+v", view)
		}
		if !view.CanRemove {
			t.Fatal("with 2 rows, CanRemove must be true")
		}
		if len(view.Rows) != 2 || len(view.Rows[0].Fields) != 2 {
			t.Fatalf("rows = %+v", view.Rows)
		}
		if view.Rows[1].Fields[0].Error != "required" {
			t.Fatalf("per-row error not projected: %+v", view.Rows[1].Fields[0])
		}
		if view.Rows[0].Fields[0].LabelID == view.Rows[1].Fields[0].LabelID {
			t.Fatal("row field IDs are not unique across rows")
		}
		one, err := r.Render(InputRenderRequest{Widget: group, Locale: "en-US",
			RepeatingRows: []RepeatingRowInput{{Values: map[string]string{"name": "Ann"}}}})
		if err != nil || !one.CanAdd {
			t.Fatalf("below cap, CanAdd must be true: %+v, %v", one, err)
		}
		_, err = r.Render(InputRenderRequest{Widget: group, Locale: "en-US",
			RepeatingRows: []RepeatingRowInput{{}, {}, {}}})
		if err == nil {
			t.Fatal("rows above the cap were accepted")
		}
	})

	t.Run("AttachmentAdmissionRoundTrip", func(t *testing.T) {
		reg := documentsecurity.NewRegistry()
		l := documentsecurity.Limits{MaxBytes: 100, MaxDerivativeBytes: 100, MaxCompressionRatio: 10}
		u := documentsecurity.Upload{ID: "upload-1", Name: "offer.pdf", ContentType: "application/pdf", Bytes: []byte("bytes")}
		if _, err := reg.Scan(context.Background(), u, l, safeScanner{}); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ref, err := documents.AdmitAttachment(intake.NewService(reg), documents.AttachmentRequest{
			ArtifactID: u.ID, Purpose: "hire.offer", SubjectID: "worker-1", RequestedBy: "recruiter",
		})
		if err != nil {
			t.Fatalf("AdmitAttachment: %v", err)
		}
		evidenceRef, isRef := ref.Evidence.(intake.EvidenceRef)
		if ref.DocumentType != "PDF" || !isRef || evidenceRef.ID == "" {
			t.Fatalf("typed reference = %+v", ref)
		}
		widgets := []pagedef.WorkflowPageWidget{
			{ID: "offer", Kind: pagedef.WorkflowWidgetAttachment, Binding: "offer_letter", Label: "Offer", Required: true, Accept: "pdf"},
		}
		if errs := ValidateSubmission(widgets, PageSubmission{Attachments: map[string]documents.AttachmentReference{"offer_letter": ref}}); len(errs) != 0 {
			t.Fatalf("admitted attachment rejected: %v", errs)
		}
		// A quarantined artifact never admits: the submission then fails
		// on the missing required reference.
		reg2 := documentsecurity.NewRegistry()
		u2 := documentsecurity.Upload{ID: "upload-2", Name: "evil.pdf", ContentType: "application/pdf", Bytes: []byte("bytes")}
		if _, err := reg2.Scan(context.Background(), u2, l, quarantineScanner{}); err != nil {
			t.Fatalf("scan quarantined: %v", err)
		}
		if _, err := documents.AdmitAttachment(intake.NewService(reg2), documents.AttachmentRequest{
			ArtifactID: u2.ID, Purpose: "hire.offer", SubjectID: "worker-1", RequestedBy: "recruiter",
		}); err == nil {
			t.Fatal("quarantined artifact admitted")
		}
		if errs := ValidateSubmission(widgets, PageSubmission{}); len(errs) == 0 {
			t.Fatal("missing required attachment accepted")
		}
	})

	t.Run("ContractValidation", func(t *testing.T) {
		page := pagedef.WorkflowPageDefinition{
			Schema: pagedef.WorkflowPageSchema, SchemaVersion: pagedef.WorkflowPageSchemaVersion,
			WorkflowKey: "workflow.hire", WorkflowVersion: 1, PageVersion: 1, PageID: "workflow.hire.input",
			Sections: []pagedef.WorkflowPageSection{{ID: "s", Label: "S", Columns: 1}},
			Widgets: []pagedef.WorkflowPageWidget{
				{ID: "a", SectionID: "s", Column: 1, Kind: pagedef.WorkflowWidgetAttachment, Binding: "offer", Type: "STRING", Label: "Offer", Accept: "pdf"},
				{ID: "g", SectionID: "s", Column: 1, Kind: pagedef.WorkflowWidgetSignature, Binding: "ack", Type: "STRING", Label: "Ack", Assurance: "QUALIFIED"},
				{ID: "r", SectionID: "s", Column: 1, Kind: pagedef.WorkflowWidgetRepeatingGroup, Binding: "contacts", Type: "LIST", Label: "Contacts", RowCap: 3,
					RowWidgets: []pagedef.WorkflowPageWidget{
						{ID: "name", Kind: pagedef.WorkflowWidgetText, Binding: "name", Label: "Name"},
					}},
				{ID: "b", SectionID: "s", Column: 1, Kind: pagedef.WorkflowWidgetMoney, Binding: "base", Type: "MONEY", Label: "Base", Currency: "USD", PayBasis: "annual"},
				{ID: "t", SectionID: "s", Column: 1, Kind: pagedef.WorkflowWidgetComputed, Binding: "total", Type: "MONEY", Label: "Total",
					ComputedOp: pagedef.ComputedOpSum, ComputedInputs: []string{"base"}},
			},
			Accessibility: pagedef.Accessibility{Landmarks: []string{"main"}, LiveRegion: pagedef.LiveRegionPolite},
			BrandTokens:   []string{"brand.color.primary"},
		}
		if got := page.Validate(); len(got) != 0 {
			t.Fatalf("valid page rejected: %v", got)
		}
		plan := &workflow.CompiledWorkflow{WorkflowID: "workflow.hire", Version: 1, Inputs: []workflow.Field{
			{Path: "offer", Type: workflow.ValueType{Kind: workflow.KindString}},
			{Path: "ack", Type: workflow.ValueType{Kind: workflow.KindString}},
			{Path: "contacts", Type: workflow.ValueType{Kind: workflow.KindList, Element: &workflow.ValueType{Kind: workflow.KindString}}},
			{Path: "base", Type: workflow.ValueType{Kind: workflow.KindMoney}},
			{Path: "total", Type: workflow.ValueType{Kind: workflow.KindMoney}},
		}}
		if got := page.ValidateAgainst(plan); len(got) != 0 {
			t.Fatalf("valid page rejected against plan: %v", got)
		}
		bad := []pagedef.WorkflowPageWidget{
			{ID: "x", SectionID: "s", Column: 1, Kind: pagedef.WorkflowWidgetSignature, Binding: "ack", Label: "Ack", Assurance: "ULTRA"},
			{ID: "x", SectionID: "s", Column: 1, Kind: pagedef.WorkflowWidgetRepeatingGroup, Binding: "c", Label: "C",
				RowWidgets: []pagedef.WorkflowPageWidget{{ID: "n", Kind: pagedef.WorkflowWidgetText, Binding: "n", Label: "N"}}},
			{ID: "x", SectionID: "s", Column: 1, Kind: pagedef.WorkflowWidgetRepeatingGroup, Binding: "c", Label: "C", RowCap: 2,
				RowWidgets: []pagedef.WorkflowPageWidget{{ID: "n", Kind: pagedef.WorkflowWidgetRepeatingGroup, Binding: "n", Label: "N"}}},
			{ID: "x", SectionID: "s", Column: 1, Kind: pagedef.WorkflowWidgetComputed, Binding: "t", Label: "T", ComputedOp: "eval", ComputedInputs: []string{"base"}},
			{ID: "x", SectionID: "s", Column: 1, Kind: pagedef.WorkflowWidgetComputed, Binding: "t", Label: "T", ComputedOp: pagedef.ComputedOpSum, ComputedInputs: []string{"nope"}},
			{ID: "x", SectionID: "s", Column: 1, Kind: pagedef.WorkflowWidgetComputed, Binding: "t", Label: "T", ComputedOp: pagedef.ComputedOpSum, ComputedInputs: []string{"t"}},
		}
		for i, w := range bad {
			mut := page
			mut.Widgets = []pagedef.WorkflowPageWidget{w}
			if got := mut.Validate(); len(got) == 0 {
				t.Errorf("bad widget %d accepted", i)
			}
		}
		mismatch := page
		mismatch.Widgets = []pagedef.WorkflowPageWidget{
			{ID: "a", SectionID: "s", Column: 1, Kind: pagedef.WorkflowWidgetAttachment, Binding: "offer", Label: "Offer"},
		}
		intPlan := &workflow.CompiledWorkflow{WorkflowID: "workflow.hire", Version: 1, Inputs: []workflow.Field{
			{Path: "offer", Type: workflow.ValueType{Kind: workflow.KindInteger}},
		}}
		if got := mismatch.ValidateAgainst(intPlan); len(got) == 0 {
			t.Fatal("attachment on INTEGER input accepted")
		}
	})

	t.Run("SignatureCeremonyRoundTrip", func(t *testing.T) {
		sig, err := documents.BeginSignature(documents.SignatureRequest{
			ID: "sig-1", ArtifactHash: "sha256:abc", SignerID: "worker-1",
			AssuranceLevel: documents.AssuranceEnhanced, TrustedNow: 100, ExpiresAt: 200,
		})
		if err != nil {
			t.Fatalf("BeginSignature: %v", err)
		}
		if sig.AssuranceLabel() != "ENHANCED" {
			t.Fatalf("assurance label = %q", sig.AssuranceLabel())
		}
		ceremony, err := sig.Ceremony.Deliver("receipt-1", "cb-1", 110)
		if err != nil {
			t.Fatalf("deliver: %v", err)
		}
		ceremony, err = ceremony.Acknowledge("sha256:abc", 120)
		if err != nil {
			t.Fatalf("acknowledge: %v", err)
		}
		sig.Ceremony = ceremony
		signed, err := sig.Sign(signing.SignerProofFor("worker-1", "sha256:abc"), "sha256:abc", "cb-2", 130)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		widgets := []pagedef.WorkflowPageWidget{
			{ID: "ack", Kind: pagedef.WorkflowWidgetSignature, Binding: "acknowledgement", Label: "Ack", Required: true, Assurance: "ENHANCED"},
		}
		if errs := ValidateSubmission(widgets, PageSubmission{Signatures: map[string]documents.Signature{"acknowledgement": signed}}); len(errs) != 0 {
			t.Fatalf("signed ceremony rejected: %v", errs)
		}
		if _, err := documents.BeginSignature(documents.SignatureRequest{
			ID: "sig-2", ArtifactHash: "sha256:x", SignerID: "w",
			AssuranceLevel: "ULTRA", TrustedNow: 1, ExpiresAt: 2,
		}); err == nil {
			t.Fatal("unknown assurance level accepted")
		}
	})
}

// TestTodo_WFPAGE_014_Browser is the component-level browser contract: RTL,
// read-only computed values, assurance labels and row-cap state project
// without a server or a real browser process.
func TestTodo_WFPAGE_014_Browser(t *testing.T) {
	r := NewWorkflowInputRegistry()
	view, err := r.Render(InputRenderRequest{
		Widget: pagedef.WorkflowPageWidget{ID: "total", Kind: pagedef.WorkflowWidgetComputed,
			Binding: "total", Label: "Total", ComputedOp: pagedef.ComputedOpCount,
			ComputedInputs: []string{"a", "b"}},
		Locale: "ar",
		Values: map[string]string{"a": "x", "b": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Direction != "rtl" || !view.ReadOnly || view.ComputedValue != "1" {
		t.Fatalf("RTL computed projection = %+v", view)
	}
	sig, err := r.Render(InputRenderRequest{
		Widget: pagedef.WorkflowPageWidget{ID: "ack", Kind: pagedef.WorkflowWidgetSignature,
			Binding: "ack", Label: "Ack", Assurance: "QUALIFIED"},
		Locale: "ar",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sig.Direction != "rtl" || sig.Assurance != "QUALIFIED" {
		t.Fatalf("RTL signature projection = %+v", sig)
	}
	group := pagedef.WorkflowPageWidget{ID: "contacts", Kind: pagedef.WorkflowWidgetRepeatingGroup,
		Binding: "contacts", Label: "Contacts", RowCap: 3,
		RowWidgets: []pagedef.WorkflowPageWidget{
			{ID: "name", Kind: pagedef.WorkflowWidgetText, Binding: "name", Label: "Name"},
		}}
	rep, err := r.Render(InputRenderRequest{Widget: group, Locale: "ar"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Direction != "rtl" || rep.RowCap != 3 || !rep.CanAdd || rep.CanRemove {
		t.Fatalf("RTL repeating projection = %+v", rep)
	}
}

// TestTodo_WFPAGE_014_Security proves the server never trusts the client:
// computed values are recomputed and mismatches rejected, unknown bindings
// are refused, forged attachment references fail admission, and unsigned
// ceremonies are refused.
func TestTodo_WFPAGE_014_Security(t *testing.T) {
	t.Run("TamperedComputedRejected", func(t *testing.T) {
		widgets := []pagedef.WorkflowPageWidget{
			{ID: "base", Kind: pagedef.WorkflowWidgetMoney, Binding: "base", Label: "Base", Currency: "USD", PayBasis: "annual"},
			{ID: "total", Kind: pagedef.WorkflowWidgetComputed, Binding: "total", Label: "Total",
				ComputedOp: pagedef.ComputedOpSum, ComputedInputs: []string{"base"}},
		}
		honest := PageSubmission{Values: map[string]string{"base": "100.00", "total": "100.00"}}
		if errs := ValidateSubmission(widgets, honest); len(errs) != 0 {
			t.Fatalf("honest computed rejected: %v", errs)
		}
		tampered := PageSubmission{Values: map[string]string{"base": "100.00", "total": "999.99"}}
		errs := ValidateSubmission(widgets, tampered)
		if len(errs) != 1 || errs[0].Binding != "total" {
			t.Fatalf("tampered computed accepted: %v", errs)
		}
	})

	t.Run("UnknownBindingRejected", func(t *testing.T) {
		widgets := []pagedef.WorkflowPageWidget{
			{ID: "name", Kind: pagedef.WorkflowWidgetText, Binding: "name", Label: "Name"},
		}
		errs := ValidateSubmission(widgets, PageSubmission{Values: map[string]string{"name": "Ann", "is_admin": "true"}})
		if len(errs) != 1 || errs[0].Binding != "is_admin" {
			t.Fatalf("spoofed binding accepted: %v", errs)
		}
	})

	t.Run("AttachmentWithoutEvidenceRefused", func(t *testing.T) {
		widgets := []pagedef.WorkflowPageWidget{
			{ID: "offer", Kind: pagedef.WorkflowWidgetAttachment, Binding: "offer_letter", Label: "Offer", Required: true},
		}
		// A typed reference with no admission behind it must not reach Check,
		// which would panic on the nil seam.
		unadmitted := PageSubmission{Attachments: map[string]documents.AttachmentReference{
			"offer_letter": {ID: "x", ArtifactID: "a", DocumentType: "PDF"},
		}}
		errs := ValidateSubmission(widgets, unadmitted)
		if len(errs) != 1 || errs[0].Message != "attachment has no evidence reference" {
			t.Fatalf("errs = %v, want the single no-evidence refusal", errs)
		}
	})

	t.Run("ForgedAttachmentRejected", func(t *testing.T) {
		widgets := []pagedef.WorkflowPageWidget{
			{ID: "offer", Kind: pagedef.WorkflowWidgetAttachment, Binding: "offer_letter", Label: "Offer", Required: true},
		}
		forged := PageSubmission{Attachments: map[string]documents.AttachmentReference{
			"offer_letter": {ID: "fake", ArtifactID: "nope", DocumentType: "PDF"},
		}}
		if errs := ValidateSubmission(widgets, forged); len(errs) == 0 {
			t.Fatal("forged attachment reference accepted")
		}
	})

	t.Run("UnsignedCeremonyRejected", func(t *testing.T) {
		sig, err := documents.BeginSignature(documents.SignatureRequest{
			ID: "sig-9", ArtifactHash: "sha256:q", SignerID: "worker-9",
			AssuranceLevel: documents.AssuranceBasic, TrustedNow: 10, ExpiresAt: 20,
		})
		if err != nil {
			t.Fatal(err)
		}
		widgets := []pagedef.WorkflowPageWidget{
			{ID: "ack", Kind: pagedef.WorkflowWidgetSignature, Binding: "ack", Label: "Ack", Required: true, Assurance: "BASIC"},
		}
		if errs := ValidateSubmission(widgets, PageSubmission{Signatures: map[string]documents.Signature{"ack": sig}}); len(errs) == 0 {
			t.Fatal("unsigned ceremony accepted")
		}
	})

	t.Run("RowOverflowRejected", func(t *testing.T) {
		widgets := []pagedef.WorkflowPageWidget{
			{ID: "contacts", Kind: pagedef.WorkflowWidgetRepeatingGroup, Binding: "contacts", Label: "Contacts",
				RowCap: 1, RowWidgets: []pagedef.WorkflowPageWidget{
					{ID: "name", Kind: pagedef.WorkflowWidgetText, Binding: "name", Label: "Name", Required: true},
				}},
		}
		over := PageSubmission{Rows: map[string][]map[string]string{
			"contacts": {{"name": "a"}, {"name": "b"}},
		}}
		if errs := ValidateSubmission(widgets, over); len(errs) == 0 {
			t.Fatal("row overflow accepted")
		}
		blank := PageSubmission{Rows: map[string][]map[string]string{
			"contacts": {{"name": ""}},
		}}
		errs := ValidateSubmission(widgets, blank)
		if len(errs) != 1 || errs[0].Row != 0 || errs[0].Field != "name" {
			t.Fatalf("per-row required error wrong: %v", errs)
		}
	})
}

type safeScanner struct{}

func (safeScanner) Scan(context.Context, documentsecurity.ScanInput, documentsecurity.Limits) (documentsecurity.Verdict, error) {
	return documentsecurity.Verdict{State: documentsecurity.Safe, Scanner: "test-av", ScannerVersion: "1", Derivative: []byte("%PDF-1.7\nsafe derivative")}, nil
}

type quarantineScanner struct{}

func (quarantineScanner) Scan(context.Context, documentsecurity.ScanInput, documentsecurity.Limits) (documentsecurity.Verdict, error) {
	return documentsecurity.Verdict{State: documentsecurity.Quarantined, Scanner: "test-av", ScannerVersion: "1"}, nil
}
