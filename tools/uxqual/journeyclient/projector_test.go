package journeyclient

import (
	"strconv"
	"strings"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestUXBlindPromotionWithoutPublishedPathIsActionable(t *testing.T) {
	worker := &journeyv1.Worker{JobCode: "SAL-AE3", Grade: "P4"}
	form := focusedProposalForm(nil, "adrian", &journeyv1.WorkforceOptions{}, worker)
	if !form.Disabled || !strings.Contains(form.DisabledReason, "career path") {
		t.Fatalf("missing promotion path must disable submission with recovery guidance: %+v", form)
	}
	if strings.Contains(form.DisabledReason, "access role") {
		t.Fatal("job architecture gap must not suggest changing authorization roles")
	}
}

// ---------------------------------------------------------------------
// Fixtures. One promotion -- Omar Reyes, OPS-HRBP2/P2 to OPS-HRBP3/P3, USD
// 93,000.00 to 98,000.00 effective 1 June 2026 -- shaped exactly as the
// engine sends it, so the projection tests below assert against wire values
// rather than against strings already in the page's own vocabulary.
// ---------------------------------------------------------------------

const (
	testIntentID   = "int_01JX6Y8B2C7D9EFG"
	testInstanceID = "wfi_01JX7Q2M4K8N3RA6"
	testApprover   = "dana.whitfield@northwind.example"
)

func stamp(t *testing.T, value string) *timestamppb.Timestamp {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parsing %q: %v", value, err)
	}
	return timestamppb.New(parsed)
}

func testConfig() Config {
	return Config{
		TunnelURL:    "wss://cell.example/grpc",
		Bearer:       "tok",
		Tenant:       "Northwind Trading · US",
		Subject:      testApprover,
		Roles:        []string{"hr.business_partner", "promotion.approver"},
		Purpose:      "promotion_review",
		JourneysPath: DefaultJourneysPath,
	}
}

// testDiagnosticsAuthorizedConfig is testConfig plus PROMOUX-008 diagnostics
// authorization, for the handful of tests whose whole point is that every
// projected section reaches the renderer -- not testConfig itself, because
// a non-empty PagePermissions also flips DetailPage's unrelated back-compat
// default for its own action-visibility check (CanPageAction("journeys" /
// "work", "update")), which every other action-focused test here still
// relies on being permissive by default.
func testDiagnosticsAuthorizedConfig() Config {
	cfg := testConfig()
	cfg.PagePermissions = []PagePermission{
		{RoleID: "hr.business_partner", PageID: diagnosticsPageID, View: true},
		{RoleID: "hr.business_partner", PageID: "journeys", View: true, Update: true},
		{RoleID: "hr.business_partner", PageID: "work", View: true, Update: true},
	}
	return cfg
}

func TestTodo_UXAUDIT_006_I18N_RecoveryNotices(t *testing.T) {
	notices := []*journey.Notice{
		keyedNotice(toneWarning, "journey.refusal_busy_title", "journey.refusal_busy_detail"),
		keyedNotice(toneDanger, "journey.refusal_unknown_action_title", "journey.refusal_unknown_action_detail"),
		keyedNotice(toneWarning, "journey.refusal_worker_title", "journey.refusal_worker_detail"),
		keyedNotice(toneWarning, "journey.refusal_target_title", "journey.refusal_target_detail"),
		keyedNotice(toneWarning, "journey.refusal_stale_title", "journey.refusal_stale_detail"),
		keyedNotice(toneWarning, "journey.refusal_reason_title", "journey.refusal_reason_detail"),
		routeReadNotice(nil),
		routeReadNotice(status.Error(codes.PermissionDenied, "secret journey")),
		keyedNotice(toneWarning, "journey.watch_stopped_title", "journey.watch_stopped_detail"),
	}
	for _, language := range productui.SupportedProductLocales() {
		cfg := testConfig()
		cfg.Locale = language
		copy := productui.ResolveProductLocale(language)
		for _, notice := range notices {
			if notice == nil {
				continue
			}
			page := chrome(cfg, "Promotion", notice, nil, true)
			if page.Notice == nil || page.Notice.Title != copy.Text(notice.TitleKey) || page.Notice.Detail != copy.Text(notice.MessageKey) {
				t.Errorf("%s notice %q did not resolve from locale catalog: %+v", language, notice.TitleKey, page.Notice)
			}
			if notice.Title != productui.ResolveProductLocale("").Text(notice.TitleKey) {
				t.Errorf("%s source notice was mutated: %+v", language, notice)
			}
		}
	}
}

func TestDetailPageDoesNotOfferAnotherApproversDecision(t *testing.T) {
	cfg := testConfig()
	cfg.Subject = "avery.okafor@northwind.example"
	detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	detail.CanDecide = false
	p := DetailPage(cfg, detail, nil, nil)
	for _, action := range p.Detail.Actions {
		if action.ID == ActionApprove || action.ID == ActionReject {
			t.Fatalf("non-assignee received approval control: %+v", action)
		}
	}
}

func testJourney(t *testing.T, stage journeyv1.JourneyStage) *journeyv1.Journey {
	t.Helper()
	j := &journeyv1.Journey{
		IntentId:      testIntentID,
		CorrelationId: "cor_01JX6Y8B2C7D9EFG",
		WorkerRef:     "worker:NW-40118",
		WorkerName:    "Omar Reyes",
		Current: &journeyv1.Placement{
			JobCode: "OPS-HRBP2", Grade: "P2", PositionId: "POS-4471",
			OrgUnit: "People Operations · EMEA", PayZone: "ZONE-2",
		},
		Target: &journeyv1.Placement{
			JobCode: "OPS-HRBP3", Grade: "P3", PositionId: "POS-4471",
			OrgUnit: "People Operations · EMEA", PayZone: "ZONE-2",
		},
		CurrentBase:        "93000.00",
		ProposedBase:       "98000.00",
		Currency:           "USD",
		EffectiveDate:      "2026-06-01",
		BusinessReason:     "Runs the EMEA HRBP portfolio single-handed.",
		Stage:              stage,
		ProposalRevisionId: "rev_01JX6Y8B3H5J7KLM",
		MaterialDigest:     "sha256:6f1c9e2a7b40d38f",
		CreatedAt:          stamp(t, "2026-05-12T08:58:00Z"),
		UpdatedAt:          stamp(t, "2026-05-12T09:12:00Z"),
	}
	if stage == journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL ||
		stage == journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED ||
		stage == journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED {
		j.InstanceId = testInstanceID
		j.InstanceVersion = 7
	}
	return j
}

func testDetail(t *testing.T, stage journeyv1.JourneyStage) *journeyv1.JourneyDetail {
	t.Helper()
	detail := &journeyv1.JourneyDetail{
		Journey: testJourney(t, stage),
		Findings: []*journeyv1.Finding{
			{Severity: "SUCCESS", Code: "budget.envelope_within_limit", Message: "Fits the FY26 merit envelope."},
			{Severity: "WARNING", Code: "compa.above_zone_midpoint", Message: "4.2% above the P3 midpoint."},
			{Severity: "BLOCKING", Code: "policy.two_step", Message: "Two grade steps route a second approval."},
			{Severity: "chatty", Code: "note.unknown_severity", Message: "An unrecognised severity."},
		},
		PlannedWrites:        []string{"worker:NW-40118/promotion"},
		EvidenceIds:          []string{"evd_cap_01_worker_read", "evd_gate_01_p1b"},
		Approver:             testApprover,
		DetailDigest:         "sha256:aaaa1111",
		CanDecide:            true,
		DiagnosticsAvailable: true,
		Timeline: []*journeyv1.TimelineEvent{
			{At: stamp(t, "2026-05-12T08:58:00Z"), Kind: eventIntentCreated, Title: "Promotion proposed", Ref: testIntentID},
			{At: stamp(t, "2026-05-12T08:58:30Z"), Kind: eventSimulated, Title: "Proposal simulated", Ref: "rev_01"},
		},
	}
	if stage == journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED || stage == journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED {
		return detail
	}

	detail.Instance = &journeyv1.Instance{
		InstanceId: testInstanceID, InstanceVersion: 7,
		WorkflowId: "promotion.approval", WorkflowVersion: 3,
		PlanDigest: "sha256:a41d0be8c37f5219", Status: "RUNNING",
		CurrentNodeIds: []string{"approval"}, CorrelationId: "cor_01JX6Y8B2C7D9EFG",
		CreatedAt: stamp(t, "2026-05-12T09:12:00Z"), StartedAt: stamp(t, "2026-05-12T09:12:00Z"),
	}
	// Durable stage fixtures represent successive workflow snapshots. Give
	// them the ordering metadata a real engine publishes so response-order
	// tests do not accidentally model distinct stages with equal timestamps.
	if ordinal := fixtureStageOrdinal(stage); ordinal > 0 {
		detail.Journey.UpdatedAt = timestamppb.New(stamp(t, "2026-05-12T09:12:00Z").AsTime().Add(time.Duration(ordinal) * time.Second).UTC())
	}
	detail.Nodes = []*journeyv1.NodeExecution{
		{NodeId: "gate.p1b", StepType: "GATE", Status: "COMPLETED", Attempt: 1,
			StartedAt: stamp(t, "2026-05-12T09:12:00Z"), CompletedAt: stamp(t, "2026-05-12T09:12:00Z")},
		{NodeId: "approval", StepType: "HUMAN_TASK", Status: "RUNNING", Attempt: 1,
			StartedAt: stamp(t, "2026-05-12T09:12:00Z")},
	}
	detail.WorkItems = []*journeyv1.WorkItem{{
		WorkItemId: "wi_01JX7Q2M6P1T4UVW", Kind: "APPROVAL", Status: "OPEN",
		OwnerRef: "role:compensation_approver", ChosenOwner: testApprover,
		NodeId: "approval", DeadlineAt: stamp(t, "2026-05-15T17:00:00Z"),
	}}
	detail.Timeline = append(detail.Timeline,
		&journeyv1.TimelineEvent{At: stamp(t, "2026-05-12T09:12:00Z"), Kind: eventInstanceStarted,
			Title: "Execution admitted and instance started", Detail: "RUNNING", Ref: testInstanceID},
		&journeyv1.TimelineEvent{At: stamp(t, "2026-05-12T09:12:01Z"), Kind: eventNode,
			Title: "gate.p1b", Detail: "COMPLETED", Ref: "gate.p1b"},
		&journeyv1.TimelineEvent{At: stamp(t, "2026-05-12T09:12:02Z"), Kind: eventWorkItem,
			Actor: "workflow", Title: "OPEN", Detail: "Routed to the compensation approver.", Ref: "wi_01JX7Q2M6P1T4UVW"},
	)

	if stage != journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED {
		return detail
	}
	detail.Instance.Status = "COMPLETED"
	detail.Instance.CompletedAt = stamp(t, "2026-05-12T10:04:00Z")
	detail.WorkItems[0].Status = "COMPLETED"
	detail.WorkItems[0].ClaimedBy = testApprover
	detail.WorkItems[0].ClaimedAt = stamp(t, "2026-05-12T09:58:00Z")
	detail.WorkItems[0].CompletedBy = testApprover
	detail.WorkItems[0].CompletedAt = stamp(t, "2026-05-12T10:02:00Z")
	detail.Ledger = &journeyv1.LedgerEvent{
		StreamKey: "worker:NW-40118/promotion", Sequence: 4,
		SchemaRef: "hcm.promotion.recorded.v1", Digest: "sha256:0d7e4c9a15b8f632",
		IdempotencyKey: "idem_01_record",
		OccurredAt:     stamp(t, "2026-05-12T10:04:00Z"),
		EffectiveAt:    stamp(t, "2026-06-01T00:00:00Z"),
		RecordedAt:     stamp(t, "2026-05-12T10:04:00Z"),
	}
	detail.Timeline = append(detail.Timeline,
		&journeyv1.TimelineEvent{At: stamp(t, "2026-05-12T10:02:00Z"), Kind: eventWorkItem,
			Actor: testApprover, Title: "COMPLETED", Detail: "Approved.", Ref: "wi_01JX7Q2M6P1T4UVW"},
		&journeyv1.TimelineEvent{At: stamp(t, "2026-05-12T10:04:00Z"), Kind: eventLedgerRecorded,
			Actor: "workflow", Title: "Promotion outcome recorded", Detail: "hcm.promotion.recorded.v1",
			Ref: "worker:NW-40118/promotion"},
	)
	return detail
}

func fixtureStageOrdinal(stage journeyv1.JourneyStage) int {
	switch stage {
	case journeyv1.JourneyStage_JOURNEY_STAGE_EXECUTED:
		return 1
	case journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL:
		return 2
	case journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL:
		return 3
	case journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL:
		return 4
	case journeyv1.JourneyStage_JOURNEY_STAGE_REAPPROVAL:
		return 5
	case journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE:
		return 6
	case journeyv1.JourneyStage_JOURNEY_STAGE_OBSERVING_EFFECTS:
		return 7
	case journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED:
		return 8
	case journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED:
		return 9
	case journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED, journeyv1.JourneyStage_JOURNEY_STAGE_FAILED:
		return 9
	default:
		return 0
	}
}

// ---------------------------------------------------------------------
// The list
// ---------------------------------------------------------------------

func TestListPageChrome(t *testing.T) {
	cfg := testConfig()
	p := ListPage(cfg, ListData{}, nil, nil)

	if p.Brand != "Human Capital Management Suite" {
		t.Errorf("Brand = %q, want Human Capital Management Suite", p.Brand)
	}
	if p.Title != "Promotion requests · Human Capital Management Suite" {
		t.Errorf("Title = %q", p.Title)
	}
	if p.TenantLabel != cfg.Tenant {
		t.Errorf("TenantLabel = %q, want %q", p.TenantLabel, cfg.Tenant)
	}
	if p.Principal.Subject != cfg.Subject || p.Principal.Purpose != cfg.Purpose || len(p.Principal.Roles) != 2 {
		t.Errorf("Principal = %+v, want it built from the island", p.Principal)
	}
	if len(p.Nav) != 2 {
		t.Fatalf("Nav = %+v, want two links", p.Nav)
	}
	if p.Nav[0].Label != "Workspace" || p.Nav[0].Href != WorkspacePath {
		t.Errorf("first nav link = %+v, want the server-rendered workspace", p.Nav[0])
	}
	if p.Nav[1].Label != "Journeys" || p.Nav[1].Href != ListHref() || !p.Nav[1].Current {
		t.Errorf("second nav link = %+v, want the current journeys route", p.Nav[1])
	}
	if len(p.Footer.Lines) == 0 {
		t.Error("the footer carries no provenance line")
	}
	if p.List == nil || p.Detail != nil {
		t.Fatal("ListPage produced something other than a list view")
	}
	if p.List.Empty == "" {
		t.Error("the list has no empty-state sentence")
	}
	if !p.List.EngineAvailable {
		t.Error("EngineAvailable is false; the client cannot know that before it asks")
	}
}

// TestChromePairsPurposeWithItsLogoutHref proves UXAUDIT-007's exit-action
// clause at the data-wiring layer: whenever the admitted session carries a
// LogoutPath, the chrome both list and detail pages share must forward it
// into Principal.LogoutHref, next to the same Principal.Purpose the
// masthead's explanation reads from -- not merely somewhere on the config,
// unreachable by the component that renders the explanation. cfg.LogoutPath
// was previously dropped entirely by chrome(), which is exactly how a
// masthead could show "Purpose: compensation_review" with no paired way to
// leave it.
func TestChromePairsPurposeWithItsLogoutHref(t *testing.T) {
	cfg := testConfig()
	cfg.LogoutPath = "/workspace/logout"

	list := ListPage(cfg, ListData{}, nil, nil)
	if list.Principal.Purpose != cfg.Purpose || list.Principal.Purpose == "" {
		t.Fatalf("list chrome Principal.Purpose = %q, want the fixture's non-empty purpose", list.Principal.Purpose)
	}
	if list.Principal.LogoutHref != cfg.LogoutPath {
		t.Fatalf("list chrome Principal.LogoutHref = %q, want %q paired with Purpose %q", list.Principal.LogoutHref, cfg.LogoutPath, list.Principal.Purpose)
	}

	detail := DetailPage(cfg, testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED), nil, nil)
	if detail.Principal.Purpose != cfg.Purpose || detail.Principal.Purpose == "" {
		t.Fatalf("detail chrome Principal.Purpose = %q, want the fixture's non-empty purpose", detail.Principal.Purpose)
	}
	if detail.Principal.LogoutHref != cfg.LogoutPath {
		t.Fatalf("detail chrome Principal.LogoutHref = %q, want %q paired with Purpose %q", detail.Principal.LogoutHref, cfg.LogoutPath, detail.Principal.Purpose)
	}

	// A session with no logout destination at all (enterprise identity edge,
	// no dev browser login) still carries its purpose without inventing an
	// exit link nothing backs.
	noLogout := testConfig()
	noLogout.LogoutPath = ""
	if p := ListPage(noLogout, ListData{}, nil, nil); p.Principal.LogoutHref != "" {
		t.Fatalf("chrome invented a LogoutHref of %q with no LogoutPath on the config", p.Principal.LogoutHref)
	}
}

func TestListPageCards(t *testing.T) {
	cfg := testConfig()
	p := ListPage(cfg, ListData{Journeys: []*journeyv1.Journey{
		testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL),
		nil, // a nil entry must not become a blank card
	}}, nil, nil)

	if len(p.List.Journeys) != 1 {
		t.Fatalf("Journeys = %d cards, want 1 (the nil dropped)", len(p.List.Journeys))
	}
	c := p.List.Journeys[0]
	want := journey.JourneyCard{
		IntentID:      testIntentID,
		Href:          "#/journeys/" + testIntentID,
		WorkerName:    "Omar Reyes",
		WorkerRef:     "worker:NW-40118",
		Headline:      "OPS-HRBP2 · P2 → OPS-HRBP3 · P3",
		PayLine:       "USD 93,000.00 → 98,000.00 (+5.4%)",
		EffectiveDate: "1 Jun 2026",
		Stage:         "AWAITING_APPROVAL",
		StageLabel:    "Awaiting approval",
		StageTone:     toneWarning,
		Updated:       "12 May 2026, 09:12 UTC",
		InstanceID:    testInstanceID,
	}
	// JourneyCard carries an OnOpen callback, so the comparison is field by
	// field rather than a struct equality: the projection leaves the
	// callback nil and App.wire binds it.
	if c.OnOpen != nil {
		t.Error("the projection bound a live callback; wiring is App.wire's job")
	}
	c.OnOpen, want.OnOpen = nil, nil
	if got, wantText := describeCard(c), describeCard(want); got != wantText {
		t.Errorf("card =\n%s\nwant\n%s", got, wantText)
	}
}

// describeCard renders one card's plain fields for comparison, since the
// contract's card carries a callback and is therefore not comparable.
func describeCard(c journey.JourneyCard) string {
	return strings.Join([]string{
		"IntentID=" + c.IntentID, "Href=" + c.Href,
		"WorkerName=" + c.WorkerName, "WorkerRef=" + c.WorkerRef,
		"Headline=" + c.Headline, "PayLine=" + c.PayLine,
		"EffectiveDate=" + c.EffectiveDate, "Stage=" + c.Stage,
		"StageLabel=" + c.StageLabel, "StageTone=" + c.StageTone,
		"Updated=" + c.Updated, "InstanceID=" + c.InstanceID,
	}, "\n")
}

func TestStageLabelsAndTones(t *testing.T) {
	cases := []struct {
		stage journeyv1.JourneyStage
		token string
		label string
		tone  string
	}{
		// PROMOUX-012: one vocabulary shared with My Work (StagePresentation).
		{journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED, stageProposed, "Ready to start approval", toneNeutral},
		{journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED, stageBlocked, "Blocked", toneWarning},
		{journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL, stageAwaitingApproval, "Awaiting approval", toneWarning},
		{journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED, stageCompleted, "Completed", toneSuccess},
		{journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED, stageRejected, "Rejected", toneNeutral},
		{journeyv1.JourneyStage_JOURNEY_STAGE_FAILED, stageFailed, "Failed", toneDanger},
		{journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL, stageFinanceApproval, "Finance approval", toneWarning},
		{journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL, stageManagerApproval, "Manager approval", toneWarning},
		{journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE, stageWaitingEffective, "Waiting for effective date", toneNeutral},
		{journeyv1.JourneyStage_JOURNEY_STAGE_REVALIDATION, stageRevalidation, "Final checks", toneNeutral},
		{journeyv1.JourneyStage_JOURNEY_STAGE_REAPPROVAL, stageReapproval, "Approval required again", toneWarning},
		{journeyv1.JourneyStage_JOURNEY_STAGE_EXECUTED, stageExecuted, "Recording promotion", toneNeutral},
		{journeyv1.JourneyStage_JOURNEY_STAGE_OBSERVING_EFFECTS, stageObservingEffects, "Checking downstream effects", toneNeutral},
		{journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED, stageRecorded, "Recorded", toneSuccess},
		{journeyv1.JourneyStage_JOURNEY_STAGE_REPAIR_REQUIRED, stageRepairRequired, "Needs repair", toneDanger},
		{journeyv1.JourneyStage_JOURNEY_STAGE_UNSPECIFIED, "UNSPECIFIED", "Status unavailable", toneWarning},
	}
	for _, c := range cases {
		t.Run(c.token, func(t *testing.T) {
			if got := stageOf(c.stage); got != c.token {
				t.Errorf("stageOf = %q, want %q", got, c.token)
			}
			if got := stageLabel(c.token); got != c.label {
				t.Errorf("stageLabel = %q, want %q", got, c.label)
			}
			if got := stageTone(c.token); got != c.tone {
				t.Errorf("stageTone = %q, want %q", got, c.tone)
			}
		})
	}
}

func TestTodo_UXAUDIT_006_JourneyHeaderLocale(t *testing.T) {
	for _, tc := range []struct {
		locale, title, stage, back string
	}{
		{"en-US", "Promotion journey", "Finance approval", "Back to Omar Reyes's profile"},
		{"de-DE", "Beförderungsantrag", "Finanzprüfung", "Zurück zum Profil von Omar Reyes"},
		{"ar", "طلب الترقية", "مراجعة المالية", "العودة إلى الملف الشخصي لـOmar Reyes"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			cfg := testConfig()
			cfg.Locale = tc.locale
			p := DetailPage(cfg, testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL), nil, nil)
			if !strings.Contains(p.Title, tc.title) || p.Detail.Journey.StageLabel != tc.stage || p.Detail.BackLink.Label != tc.back {
				t.Fatalf("unlocalized detail header: title=%q stage=%q back=%q", p.Title, p.Detail.Journey.StageLabel, p.Detail.BackLink.Label)
			}
			list := ListPage(cfg, ListData{Journeys: []*journeyv1.Journey{testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)}}, nil, nil)
			if got := list.List.Journeys[0].StageLabel; got != tc.stage {
				t.Fatalf("list stage label = %q, want %q", got, tc.stage)
			}
		})
	}
}

func TestProposalFormShape(t *testing.T) {
	form := ProposalForm(map[string]string{FieldEffective: "2026-12-01"}, nil, "")
	for _, field := range form.Fields {
		if field.ID == FieldEffective && (strings.Contains(field.Help, "today") || !strings.Contains(field.Help, "date and final outcome still need review")) {
			t.Fatalf("effective-date guidance promises a wall-clock rule: %q", field.Help)
		}
	}

	if len(form.Hidden) != 0 {
		t.Errorf("Hidden = %v, want empty: this client submits over gRPC and has no CSRF token", form.Hidden)
	}
	if form.Submit == "" {
		t.Error("the form has no submit label")
	}

	want := []struct {
		id, name, kind, value string
		required              bool
	}{
		{FieldWorker, NameWorker, kindSelect, "", true},
		{FieldJobCode, NameJobCode, kindSelect, "", true},
		{FieldGrade, NameGrade, kindSelect, "", true},
		// UXLIVE-011 replaced the free-text target position with the
		// governed picker. The kind is what this line is protecting: a text
		// box here is the defect, not a stylistic choice.
		{FieldPosition, NamePosition, kindPositionPicker, "", false},
		{FieldBase, NameBase, kindNumber, "", true},
		{FieldEffective, NameEffective, kindDate, "2026-12-01", true},
		{FieldReason, NameReason, kindTextarea, "", true},
	}
	if len(form.Fields) != len(want) {
		t.Fatalf("the form has %d fields, want %d", len(form.Fields), len(want))
	}
	for i, w := range want {
		f := form.Fields[i]
		if f.ID != w.id || f.Name != w.name || f.Kind != w.kind || f.Value != w.value || f.Required != w.required {
			t.Errorf("field %d = %+v, want id=%s name=%s kind=%s value=%q required=%v",
				i, f, w.id, w.name, w.kind, w.value, w.required)
		}
	}

	base := form.Fields[4]
	if base.Step != "0.01" || base.Prefix != "" {
		t.Errorf("the pay field = %+v, want step 0.01 and no invented currency", base)
	}

	// With no workforce answer in hand the picker offers nobody: the page
	// lists the employees the cell named and never a population of its own.
	if got := form.Fields[0].Options; len(got) != 1 || got[0].Value != "" || got[0].Label != "Select an employee" {
		t.Errorf("the worker options = %+v, want the empty prompt alone", got)
	}

	var selected string
	for _, o := range form.Fields[2].Options {
		if o.Selected {
			selected = o.Value
		}
	}
	if selected != "" {
		t.Errorf("the selected grade = %q, want the required prompt", selected)
	}
}

// TestProposalFormFallsBackToItsOwnClock covers the projection being called
// with no seeded value at all (a page rendered before App.Start ran).
func TestProposalFormFallsBackToItsOwnClock(t *testing.T) {
	form := ProposalForm(nil, nil, "")
	got := form.Fields[5].Value
	if got != DefaultEffectiveDate(time.Now()) {
		t.Errorf("effective date = %q, want the computed default %q", got, DefaultEffectiveDate(time.Now()))
	}
}

func TestProposalPayUsesTheSelectedWorkersCurrency(t *testing.T) {
	worker := &journeyv1.Worker{WorkerRef: "worker-eu", Currency: "EUR"}
	page := ListPage(testConfig(), ListData{
		Workers: []*journeyv1.Worker{worker}, SelectedRef: worker.GetWorkerRef(),
		Options: &journeyv1.WorkforceOptions{Currency: "USD"},
	}, nil, nil)
	base, ok := fieldByID(page.List.Form.Fields, FieldBase)
	if !ok {
		t.Fatal("the proposal form has no base-pay field")
	}
	if base.Prefix != "EUR" {
		t.Errorf("base-pay prefix = %q, want the selected worker's EUR", base.Prefix)
	}
}

func TestFocusedProposalPayFallsBackToTheWorkforceCurrency(t *testing.T) {
	worker := &journeyv1.Worker{WorkerRef: "worker-eu"}
	page := ProposalPage(testConfig(), ListData{
		Workers: []*journeyv1.Worker{worker}, SelectedRef: worker.GetWorkerRef(),
		Options: &journeyv1.WorkforceOptions{Currency: "EUR"},
	}, nil, nil)
	base, ok := fieldByID(page.Proposal.Form.Fields, FieldBase)
	if !ok {
		t.Fatal("the focused proposal form has no base-pay field")
	}
	if base.Prefix != "EUR" {
		t.Errorf("base-pay prefix = %q, want the live workforce currency EUR", base.Prefix)
	}
}

func TestDefaultEffectiveDate(t *testing.T) {
	cases := map[string]string{
		"2026-09-03T14:05:00Z": "2026-12-01",
		"2026-01-31T23:59:00Z": "2026-04-01",
		"2026-10-15T00:00:00Z": "2027-01-01",
		"2026-12-01T00:00:00Z": "2027-03-01",
	}
	for in, want := range cases {
		now, err := time.Parse(time.RFC3339, in)
		if err != nil {
			t.Fatalf("parsing %q: %v", in, err)
		}
		if got := DefaultEffectiveDate(now); got != want {
			t.Errorf("DefaultEffectiveDate(%s) = %q, want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------
// The detail
// ---------------------------------------------------------------------

func TestDetailPageStepsPerStage(t *testing.T) {
	cases := []struct {
		stage journeyv1.JourneyStage
		want  [5]string
	}{
		{journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED, [5]string{stepDone, stepActive, stepUpcoming, stepUpcoming, stepUpcoming}},
		{journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED, [5]string{stepFailed, stepUpcoming, stepUpcoming, stepUpcoming, stepUpcoming}},
		{journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL, [5]string{stepDone, stepActive, stepUpcoming, stepUpcoming, stepUpcoming}},
		{journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL, [5]string{stepDone, stepActive, stepUpcoming, stepUpcoming, stepUpcoming}},
		{journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL, [5]string{stepDone, stepDone, stepActive, stepUpcoming, stepUpcoming}},
		{journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE, [5]string{stepDone, stepDone, stepDone, stepActive, stepUpcoming}},
		{journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED, [5]string{stepDone, stepDone, stepDone, stepDone, stepDone}},
		{journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED, [5]string{stepDone, stepFailed, stepUpcoming, stepUpcoming, stepUpcoming}},
		{journeyv1.JourneyStage_JOURNEY_STAGE_FAILED, [5]string{stepDone, stepFailed, stepUpcoming, stepUpcoming, stepUpcoming}},
		{journeyv1.JourneyStage_JOURNEY_STAGE_UNSPECIFIED, [5]string{stepUpcoming, stepUpcoming, stepUpcoming, stepUpcoming, stepUpcoming}},
	}
	for _, c := range cases {
		t.Run(stageOf(c.stage), func(t *testing.T) {
			p := DetailPage(testConfig(), testDetail(t, c.stage), nil, nil)
			steps := p.Detail.Steps
			if len(steps) != 5 {
				t.Fatalf("steps = %d, want 5", len(steps))
			}
			for i, want := range c.want {
				if steps[i].State != want {
					t.Errorf("step %q state = %q, want %q", steps[i].ID, steps[i].State, want)
				}
				if steps[i].Label == "" || steps[i].Detail == "" {
					t.Errorf("step %q has no label or explanation", steps[i].ID)
				}
			}
			if got, want := []string{steps[0].ID, steps[1].ID, steps[2].ID, steps[3].ID, steps[4].ID},
				[]string{"proposal", "finance-review", "manager-review", "effective-date", "recorded"}; strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("step ids = %v, want %v", got, want)
			}
		})
	}
}

func TestDetailPageStepTimesComeFromTheTimeline(t *testing.T) {
	p := DetailPage(testConfig(), testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED), nil, nil)
	steps := p.Detail.Steps
	want := []string{
		"12 May 2026, 08:58 UTC", // INTENT_CREATED
		"12 May 2026, 10:02 UTC", // the finance WORK_ITEM transition
		"",                       // the legacy fixture has no separate manager work item
		"1 Jun 2026",             // immutable proposal effective date
		"12 May 2026, 10:04 UTC", // LEDGER_RECORDED
	}
	for i, w := range want {
		if steps[i].At != w {
			t.Errorf("step %q at = %q, want %q", steps[i].ID, steps[i].At, w)
		}
	}
}

// findAction returns the one action carrying id, or fails the test: a test
// that indexed into Actions positionally would silently start asserting on
// the wrong action the moment PROMOUX-013's three interventions were
// inserted at a new position, which is exactly the fragility this helper
// removes.
func findAction(t *testing.T, actions []journey.Action, id string) journey.Action {
	t.Helper()
	for _, a := range actions {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("no action %q among %+v", id, actions)
	return journey.Action{}
}

// wantInterventionIDs is the three ids [interventionActions] always appends
// once a real journey has loaded, present or absent from the count checks
// below by name rather than by number so they read as what they are: a
// fixed, always-offered trio, never accidentally miscounted with the
// lifecycle actions that vary by stage.
var wantInterventionIDs = []string{ActionWithdraw, ActionCancel, ActionEditProposal}

func TestDetailPageActionsPerStage(t *testing.T) {
	cfg := testConfig()

	t.Run("proposed offers execution", func(t *testing.T) {
		p := DetailPage(cfg, testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED), nil, nil)
		actions := p.Detail.Actions
		if len(actions) != 1+len(wantInterventionIDs) {
			t.Fatalf("actions = %d, want %d (execute plus the three interventions)", len(actions), 1+len(wantInterventionIDs))
		}
		a := findAction(t, actions, ActionExecute)
		if a.Variant != "primary" || a.Disabled {
			t.Errorf("action = %+v, want an enabled primary execute", a)
		}
		if a.Label != "Start approval workflow" {
			t.Errorf("label = %q", a.Label)
		}
		if len(a.Hidden) != 0 {
			t.Errorf("Hidden = %v, want empty", a.Hidden)
		}
		if len(a.Confirmation) == 0 || a.ConfirmationNote == "" {
			t.Error("execute bypasses its review-and-confirm summary")
		}
		// An unstarted proposal: Withdraw is offered live, Cancel and Edit
		// are not yet (nothing has started to cancel), Edit is available
		// (an unstarted proposal is still correctable).
		if w := findAction(t, actions, ActionWithdraw); w.Disabled {
			t.Errorf("Withdraw is disabled on an unstarted proposal: %+v", w)
		}
		if c := findAction(t, actions, ActionCancel); !c.Disabled || c.DisabledReason == "" {
			t.Errorf("Cancel is offered (or offers no reason) on an unstarted proposal: %+v", c)
		}
		if e := findAction(t, actions, ActionEditProposal); e.Disabled {
			t.Errorf("EditProposal is disabled on an unstarted (still correctable) proposal: %+v", e)
		}
	})

	t.Run("blocked explains refusal without a fake action", func(t *testing.T) {
		p := DetailPage(cfg, testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED), nil, nil)
		actions := p.Detail.Actions
		if len(actions) != len(wantInterventionIDs) {
			t.Fatalf("blocked journey should show intervention availability, not Start: %+v", actions)
		}
		if p.Detail.PendingOutcome == "" || p.Detail.JourneysLink.Href == "" {
			t.Fatal("blocked journey lacks its explanation or escape route")
		}
	})

	t.Run("awaiting approval offers both decisions", func(t *testing.T) {
		p := DetailPage(cfg, testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL), nil, nil)
		actions := p.Detail.Actions
		if len(actions) != 2+len(wantInterventionIDs) {
			t.Fatalf("actions = %d, want %d (approve, reject, plus the three interventions)", len(actions), 2+len(wantInterventionIDs))
		}
		approve := findAction(t, actions, ActionApprove)
		reject := findAction(t, actions, ActionReject)
		if approve.Variant != "primary" {
			t.Errorf("approve = %+v, want primary", approve)
		}
		if reject.Variant != "danger" {
			t.Errorf("reject = %+v, want danger", reject)
		}
		for _, a := range []journey.Action{approve, reject} {
			if a.ActsAs != testApprover {
				t.Errorf("action %q ActsAs = %q, want the routed approver %q", a.ID, a.ActsAs, testApprover)
			}
			if len(a.Fields) != 1 || a.Fields[0].Name != NameDecisionReason {
				t.Fatalf("action %q fields = %+v, want one reason field", a.ID, a.Fields)
			}
			if a.ActsAsLabel != "Your compensation review" || len(a.Confirmation) == 0 || a.ConfirmationNote == "" {
				t.Errorf("action %q does not explain authority and require confirmation: %+v", a.ID, a)
			}
		}
		if approve.Fields[0].ID != FieldApproveReason || approve.Fields[0].Required {
			t.Errorf("the approval reason = %+v, want %s and optional", approve.Fields[0], FieldApproveReason)
		}
		if reject.Fields[0].ID != FieldRejectReason || !reject.Fields[0].Required {
			t.Errorf("the rejection reason = %+v, want %s and required", reject.Fields[0], FieldRejectReason)
		}
		// Mid-flight, with an approval underway: Withdraw is no longer
		// offered live, Cancel and Edit are (this is exactly the
		// EditInvalidatesAMidFlightApproval scenario PROMOUX-013's own
		// integration test proved end to end against real PostgreSQL).
		if w := findAction(t, actions, ActionWithdraw); !w.Disabled || w.DisabledReason == "" {
			t.Errorf("Withdraw is offered (or offers no reason) once approval has started: %+v", w)
		}
		if c := findAction(t, actions, ActionCancel); c.Disabled {
			t.Errorf("Cancel is disabled during an eligible wait: %+v", c)
		}
		if e := findAction(t, actions, ActionEditProposal); e.Disabled {
			t.Errorf("EditProposal is disabled while Cancel is still available: %+v", e)
		}
	})

	t.Run("awaiting approval waits for the durable work item", func(t *testing.T) {
		detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)
		detail.WorkItems = nil
		actions := DetailPage(cfg, detail, nil, nil).Detail.Actions
		approve := findAction(t, actions, ActionApprove)
		if !approve.Disabled || approve.Label != "Preparing approval" {
			t.Fatalf("approve before durable routing = %+v", approve)
		}
		if len(actions) != 1+len(wantInterventionIDs) {
			t.Fatalf("actions before durable routing = %+v", actions)
		}
	})

	// These stages offer none of the ordinary lifecycle actions (execute,
	// approve, reject): PROMOUX-013 added Withdraw/Cancel/EditProposal,
	// which behave differently across the three groups below, so "offers
	// nothing" is no longer one true sentence for every stage in this list
	// -- it is checked per group instead.
	for _, stage := range []journeyv1.JourneyStage{
		journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE,
		journeyv1.JourneyStage_JOURNEY_STAGE_REVALIDATION,
		journeyv1.JourneyStage_JOURNEY_STAGE_REPAIR_REQUIRED,
	} {
		t.Run(stageOf(stage)+" offers no lifecycle action but an eligible cancel/edit", func(t *testing.T) {
			p := DetailPage(cfg, testDetail(t, stage), nil, nil)
			actions := p.Detail.Actions
			for _, id := range []string{ActionExecute, ActionApprove, ActionReject} {
				for _, a := range actions {
					if a.ID == id {
						t.Errorf("stage %s unexpectedly offers %q", stageOf(stage), id)
					}
				}
			}
			// UXLIVE-006 added a fourth action at REPAIR_REQUIRED, and only
			// there: the governed repair that stage names as its own next
			// step. Before it, this stage's Actions section offered nothing
			// about the repair it was waiting on, which is the finding. The
			// other two stages in this loop still offer exactly three.
			want := len(wantInterventionIDs)
			if stage == journeyv1.JourneyStage_JOURNEY_STAGE_REPAIR_REQUIRED {
				want++
				repair := findAction(t, actions, ActionRepair)
				if !repair.Disabled || repair.DisabledReason == "" {
					t.Errorf("the governed repair is offered (or offers no reason) at %s: %+v", stageOf(stage), repair)
				}
			} else {
				for _, a := range actions {
					if a.ID == ActionRepair {
						t.Errorf("stage %s names a governed repair it is not waiting on", stageOf(stage))
					}
				}
			}
			if len(actions) != want {
				t.Fatalf("actions = %+v, want %d", actions, want)
			}
			if w := findAction(t, actions, ActionWithdraw); !w.Disabled || w.DisabledReason == "" {
				t.Errorf("Withdraw is offered (or offers no reason) at %s: %+v", stageOf(stage), w)
			}
			if c := findAction(t, actions, ActionCancel); c.Disabled {
				t.Errorf("Cancel is disabled during an eligible wait at %s: %+v", stageOf(stage), c)
			}
			if e := findAction(t, actions, ActionEditProposal); e.Disabled {
				t.Errorf("EditProposal is disabled while Cancel is still available at %s: %+v", stageOf(stage), e)
			}
		})
	}

	for _, stage := range []journeyv1.JourneyStage{
		journeyv1.JourneyStage_JOURNEY_STAGE_EXECUTED,
		journeyv1.JourneyStage_JOURNEY_STAGE_OBSERVING_EFFECTS,
	} {
		t.Run(stageOf(stage)+" has already committed: all three interventions explain why", func(t *testing.T) {
			p := DetailPage(cfg, testDetail(t, stage), nil, nil)
			actions := p.Detail.Actions
			if len(actions) != len(wantInterventionIDs) {
				t.Fatalf("actions = %+v, want exactly the three interventions, all disabled", actions)
			}
			for _, id := range wantInterventionIDs {
				a := findAction(t, actions, id)
				if !a.Disabled || a.DisabledReason == "" {
					t.Errorf("%q is offered (or offers no reason) once execution has committed at %s: %+v", id, stageOf(stage), a)
				}
			}
		})
	}

	for _, stage := range []journeyv1.JourneyStage{
		journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED,
		journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED,
		journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED,
		journeyv1.JourneyStage_JOURNEY_STAGE_FAILED,
	} {
		t.Run(stageOf(stage)+" is terminal: all three interventions explain why, identically", func(t *testing.T) {
			p := DetailPage(cfg, testDetail(t, stage), nil, nil)
			actions := p.Detail.Actions
			if len(actions) != len(wantInterventionIDs) {
				t.Fatalf("actions = %+v, want exactly the three interventions, all disabled", actions)
			}
			for _, id := range wantInterventionIDs {
				a := findAction(t, actions, id)
				if !a.Disabled || a.DisabledReason != interventionReasonText(reasonAlreadyTerminal) {
					t.Errorf("%q at a terminal stage %s = %+v, want disabled with the byte-identical already-terminal text", id, stageOf(stage), a)
				}
			}
		})
	}
}

func TestTodo_UXAUDIT_006_I18N_ActionCards(t *testing.T) {
	for _, tc := range []struct {
		locale, start, approve, reject, reason, review string
	}{
		{"en-US", "Start approval workflow", "Approve", "Reject", "Reason for the record", "Your compensation review"},
		{"de-DE", "Genehmigungsablauf starten", "Genehmigen", "Ablehnen", "Begründung für den Verlauf", "Ihre Vergütungsprüfung"},
		{"ar", "بدء مسار الموافقة", "موافقة", "رفض", "سبب القرار في السجل", "مراجعتك للتعويضات"},
	} {
		cfg := testConfig()
		cfg.Locale = tc.locale
		proposed := DetailPage(cfg, testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED), nil, nil).Detail.Actions
		if len(proposed) == 0 || proposed[0].ID != ActionExecute || proposed[0].Label != tc.start || proposed[0].Confirmation[0].Label == "" {
			t.Errorf("%s proposed action = %+v", tc.locale, proposed)
		}
		approval := DetailPage(cfg, testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL), nil, nil).Detail.Actions
		if len(approval) < 2 || approval[0].ID != ActionApprove || approval[1].ID != ActionReject || approval[0].Label != tc.approve || approval[1].Label != tc.reject || approval[0].Fields[0].Label != tc.reason || approval[0].ActsAsLabel != tc.review {
			t.Errorf("%s approval actions = %+v", tc.locale, approval)
		}
	}
}

func TestTodo_UXAUDIT_006_I18N_BusyNotice(t *testing.T) {
	for _, tc := range []struct{ locale, title, detail string }{
		{"en-US", "Working…", "Loading employees and promotion requests."},
		{"de-DE", "Wird bearbeitet…", "Mitarbeitende und Beförderungsanträge werden geladen."},
		{"ar", "جارٍ العمل…", "جارٍ تحميل الموظفين وطلبات الترقية."},
	} {
		cfg := testConfig()
		cfg.Locale = tc.locale
		p := ListPage(cfg, ListData{}, busy("journey.busy_list", "Loading employees and promotion requests."), nil)
		if p.Notice == nil || !p.Notice.Busy || p.Notice.Title != tc.title || p.Notice.Detail != tc.detail {
			t.Errorf("%s busy notice = %+v", tc.locale, p.Notice)
		}
	}
}

func TestTodo_UXAUDIT_006_I18N_ApprovalOutcomeNotice(t *testing.T) {
	for _, tc := range []struct{ locale, title string }{
		{"en-US", "Approvals complete"},
		{"de-DE", "Genehmigungen abgeschlossen"},
		{"ar", "اكتملت الموافقات"},
	} {
		cfg := testConfig()
		cfg.Locale = tc.locale
		notice := &journey.Notice{Tone: toneSuccess, Title: "Approvals complete", Detail: "English fallback", TitleKey: "journey.notice_waiting_title", MessageKey: "journey.notice_waiting_detail"}
		p := ListPage(cfg, ListData{}, notice, nil)
		if p.Notice == nil || p.Notice.Title != tc.title || p.Notice.Detail == "English fallback" || p.Notice.Busy {
			t.Errorf("%s outcome notice = %+v", tc.locale, p.Notice)
		}
	}
}

func TestDetailPageSections(t *testing.T) {
	p := DetailPage(testConfig(), testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED), nil, nil)
	d := p.Detail

	if p.Title != "Omar Reyes · Promotion journey · Human Capital Management Suite" {
		t.Errorf("Title = %q", p.Title)
	}
	if d.BackLink.Label != "Back to Omar Reyes's profile" || d.BackLink.Href != "/workspace/app/person?person=worker%3ANW-40118" || d.JourneysLink.Href != ListHref() {
		t.Errorf("detail context links = back %+v, journeys %+v", d.BackLink, d.JourneysLink)
	}

	facts := map[string]string{}
	for _, f := range d.Proposal {
		facts[f.Label] = f.Value
	}
	for label, want := range map[string]string{
		"Intent":            testIntentID,
		"Correlation":       "cor_01JX6Y8B2C7D9EFG",
		"Proposal revision": "rev_01JX6Y8B3H5J7KLM",
		"Material digest":   "sha256:6f1c9e2a7b40d38f",
		"Planned write":     "worker:NW-40118/promotion",
	} {
		if facts[label] != want {
			t.Errorf("proposal fact %q = %q, want %q", label, facts[label], want)
		}
	}

	rows := map[string]journey.ComparisonRow{}
	for _, r := range d.Comparison {
		rows[r.Label] = r
	}
	if got, want := len(d.Comparison), 6; got != want {
		t.Errorf("comparison rows = %d, want %d", got, want)
	}
	if r := rows["Job code"]; r.Current != "OPS-HRBP2" || r.Proposed != "OPS-HRBP3" || !r.Changed {
		t.Errorf("job code row = %+v", r)
	}
	if r := rows["Position"]; r.Changed {
		t.Errorf("position row marked changed though it did not move: %+v", r)
	}
	if r := rows["Base pay"]; r.Current != "USD 93,000.00" || r.Proposed != "USD 98,000.00" ||
		r.Delta != "+USD 5,000.00 (+5.4%)" || !r.Changed {
		t.Errorf("base pay row = %+v", r)
	}
	if r := rows["Effective date"]; r.Current != emDash || r.Proposed != "1 Jun 2026" {
		t.Errorf("effective date row = %+v", r)
	}

	if len(d.Findings) != 4 {
		t.Fatalf("findings = %d, want 4", len(d.Findings))
	}
	wantSeverities := []string{severitySuccess, severityWarning, severityBlocking, severityInfo}
	for i, want := range wantSeverities {
		if d.Findings[i].Severity != want {
			t.Errorf("finding %d severity = %q, want %q", i, d.Findings[i].Severity, want)
		}
	}

	engine := map[string]journey.Fact{}
	for _, f := range d.Engine {
		engine[f.Label] = f
	}
	for label, want := range map[string]string{
		"Instance":     testInstanceID,
		"Version":      "7",
		"Workflow":     "promotion.approval@3",
		"Plan digest":  "sha256:a41d0be8c37f5219",
		"Status":       "COMPLETED",
		"Current node": "approval",
		"Correlation":  "cor_01JX6Y8B2C7D9EFG",
	} {
		if engine[label].Value != want {
			t.Errorf("engine fact %q = %q, want %q", label, engine[label].Value, want)
		}
	}
	if engine["Instance"].Mono != true || engine["Plan digest"].Mono != true || engine["Correlation"].Mono != true {
		t.Error("identifiers and digests are not rendered monospace")
	}
	if engine["Status"].Tone != toneSuccess {
		t.Errorf("a COMPLETED status tone = %q, want success", engine["Status"].Tone)
	}

	if len(d.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(d.Nodes))
	}
	if d.Nodes[0].Tone != toneSuccess || d.Nodes[1].Tone != toneWarning {
		t.Errorf("node tones = %q, %q; want success then warning", d.Nodes[0].Tone, d.Nodes[1].Tone)
	}
	if d.Nodes[1].Completed != emDash {
		t.Errorf("an unfinished node's completion = %q, want a dash", d.Nodes[1].Completed)
	}

	if len(d.WorkItems) != 1 {
		t.Fatalf("work items = %d, want 1", len(d.WorkItems))
	}
	w := d.WorkItems[0]
	if w.Owner != testApprover {
		t.Errorf("work item owner = %q, want the routed owner %q", w.Owner, testApprover)
	}
	if w.Completed != testApprover+", 12 May 2026, 10:02 UTC" {
		t.Errorf("work item completion = %q", w.Completed)
	}
	if w.Tone != toneSuccess {
		t.Errorf("a completed work item tone = %q, want success", w.Tone)
	}

	if d.Ledger == nil {
		t.Fatal("the ledger fact is missing from a completed journey")
	}
	if d.Ledger.Sequence != "4" || d.Ledger.EffectiveAt != "1 Jun 2026" || d.Ledger.RecordedAt != "12 May 2026, 10:04 UTC" {
		t.Errorf("ledger card = %+v", *d.Ledger)
	}

	if len(d.Evidence) != 2 {
		t.Errorf("evidence = %v, want both identifiers", d.Evidence)
	}

	if d.EffectiveWindow == nil {
		t.Fatal("the effective window is missing")
	}
	if d.EffectiveWindow.Start != "12 May 2026" || d.EffectiveWindow.EffectiveDate != "1 Jun 2026" ||
		d.EffectiveWindow.KnownAt != "12 May 2026, 09:12 UTC" {
		t.Errorf("effective window = %+v", *d.EffectiveWindow)
	}

	// The detail carries no pay band and no budget envelope, so the page
	// draws neither rather than drawing a gauge from numbers it invented.
	if d.PayBand != nil || d.Budget != nil {
		t.Error("a gauge was drawn from data the engine did not send")
	}
}

func TestTodo_UXAUDIT_006_I18N_PromotionDetailBusinessProjection(t *testing.T) {
	for _, tc := range []struct {
		locale, reason, base, date, recorded string
	}{
		{"de-DE", "Geschäftliche Begründung", "Grundgehalt", "01.06.2026", "Beförderung erfasst"},
		{"ar", "مبرر العمل", "الأجر الأساسي", "١ يونيو ٢٠٢٦", "سُجلت الترقية"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			cfg := testConfig()
			cfg.Locale = tc.locale
			page := DetailPage(cfg, testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED), nil, nil)
			facts := map[string]string{}
			for _, fact := range page.Detail.Proposal {
				facts[fact.Label] = fact.Value
			}
			if facts[tc.reason] == "" {
				t.Errorf("business reason not localized: %+v", page.Detail.Proposal)
			}
			rows := map[string]journey.ComparisonRow{}
			for _, r := range page.Detail.Comparison {
				rows[r.Label] = r
			}
			if row := rows[tc.base]; !row.Changed || row.Delta == "" || row.Current == "USD 93,000.00" {
				t.Errorf("base-pay comparison not localized: %+v", row)
			}
			if row := rows[productui.ResolveProductLocale(tc.locale).Text("journey.compare_effective")]; row.Proposed != tc.date {
				t.Errorf("effective-date comparison = %+v, want %q", row, tc.date)
			}
			if got := page.Detail.Timeline[0].Title; got != tc.recorded {
				t.Errorf("recorded history title = %q, want %q", got, tc.recorded)
			}
			if got := page.Detail.Timeline[0].At; got == "12 May 2026, 10:04 UTC" || got == "" {
				t.Errorf("history time not localized: %q", got)
			}
			if page.Detail.Ledger == nil || page.Detail.Ledger.EffectiveAt != tc.date {
				t.Errorf("recorded outcome date not localized: %+v", page.Detail.Ledger)
			}
		})
	}
}

func TestTimelineIsNewestFirstAndToned(t *testing.T) {
	p := DetailPage(testConfig(), testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED), nil, nil)
	events := p.Detail.Timeline
	if len(events) != 6 {
		t.Fatalf("business timeline = %d entries, want 6 without the diagnostic node", len(events))
	}
	if events[0].Title != "Promotion recorded" || events[0].Tone != toneSuccess {
		t.Errorf("newest entry = %+v, want the ledger write in success", events[0])
	}
	if events[len(events)-1].Title != "Promotion requested" {
		t.Errorf("oldest entry = %+v, want the proposal", events[len(events)-1])
	}
	if events[0].At != "12 May 2026, 10:04 UTC" {
		t.Errorf("newest entry time = %q", events[0].At)
	}

	tones := map[string]string{}
	for _, e := range events {
		tones[e.Title] = e.Tone
	}
	for title, want := range map[string]string{
		"Promotion recorded":        toneSuccess,
		"Approval completed":        toneSuccess,
		"Approval assigned":         toneInfo,
		"Approval process started":  toneInfo,
		"Promotion requested":       toneNeutral,
		"Proposal checks completed": toneNeutral,
	} {
		if tones[title] != want {
			t.Errorf("timeline entry %q tone = %q, want %q", title, tones[title], want)
		}
	}
	if _, leaked := tones["gate.p1b"]; leaked {
		t.Fatal("internal workflow node escaped the diagnostics disclosure")
	}
}

func TestLedgerEffectiveDateFallsBackToTheProposal(t *testing.T) {
	detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	detail.Ledger.EffectiveAt = nil

	page := DetailPage(testConfig(), detail, nil, nil)
	if page.Detail.Ledger == nil {
		t.Fatal("the recorded outcome is missing")
	}
	if got := page.Detail.Ledger.EffectiveAt; got != "1 Jun 2026" {
		t.Errorf("effective date = %q, want the proposal's 1 Jun 2026", got)
	}
}

func TestPendingOutcomeMatchesTheDurableBusinessStage(t *testing.T) {
	waiting := pendingOutcome(stageWaitingEffective, "1 Dec 2026")
	if !strings.Contains(waiting, "Approvals are complete") || !strings.Contains(waiting, "1 Dec 2026") {
		t.Fatalf("waiting outcome = %q", waiting)
	}
	finance := pendingOutcome(stageFinanceApproval, "1 Dec 2026")
	if !strings.Contains(finance, "Finance and manager reviews") {
		t.Fatalf("finance outcome = %q", finance)
	}
	blocked := pendingOutcome(stageBlocked, "")
	if !strings.Contains(blocked, "was not changed") {
		t.Fatalf("blocked outcome = %q", blocked)
	}
}

func TestDetailPageApprovalActionsForEveryApprovalStage(t *testing.T) {
	for _, stage := range []journeyv1.JourneyStage{
		journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL,
		journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL,
		journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL,
		journeyv1.JourneyStage_JOURNEY_STAGE_REAPPROVAL,
	} {
		t.Run(stageOf(stage), func(t *testing.T) {
			p := DetailPage(testConfig(), testDetail(t, stage), nil, nil)
			actions := p.Detail.Actions
			if len(actions) != 2+len(wantInterventionIDs) {
				t.Fatalf("actions = %d, want approve, reject, plus the three interventions", len(actions))
			}
			findAction(t, actions, ActionApprove)
			findAction(t, actions, ActionReject)
			for _, id := range wantInterventionIDs {
				findAction(t, actions, id)
			}
		})
	}
}

func TestTimelineTonesARefusal(t *testing.T) {
	events := timeline([]*journeyv1.TimelineEvent{
		{Kind: eventWorkItem, Title: "CANCELLED", Detail: "The gate refused the plan."},
		{Kind: eventNode, Title: "gate.p1b", Detail: "FAILED"},
	})
	if len(events) != 1 {
		t.Fatalf("business events = %d, want only the review refusal", len(events))
	}
	for _, e := range events {
		if e.Tone != toneDanger {
			t.Errorf("entry %q tone = %q, want danger", e.Title, e.Tone)
		}
	}
}

func TestTodo_UXAUDIT_006_I18N_RealReviewHistoryTitles(t *testing.T) {
	for _, tc := range []struct{ locale, finance, manager, system string }{
		{"de-DE", "Finanzprüfung: zugewiesen", "Prüfung durch Führungskraft: abgeschlossen", "System"},
		{"ar", "مراجعة المالية: أُسندت", "مراجعة المدير: اكتملت", "النظام"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			events := timelineLocale(tc.locale, []*journeyv1.TimelineEvent{
				{Kind: eventWorkItem, Actor: "workflow", Title: "Finance review assigned"},
				{Kind: eventWorkItem, Actor: "reviewer@example.test", Title: "Manager review completed"},
				{Kind: eventNode, Title: "approval.manager", Detail: "COMPLETED"},
			})
			if len(events) != 2 {
				t.Fatalf("history = %+v, want only two business events", events)
			}
			if events[0].Title != tc.manager || events[1].Title != tc.finance || events[1].Actor != tc.system {
				t.Errorf("localized history = %+v", events)
			}
			if got := timelineTitleLocale(tc.locale, &journeyv1.TimelineEvent{Kind: eventWorkItem, Title: "UNKNOWN_INTERNAL_STATE"}); strings.Contains(got, "INTERNAL") || got == "" {
				t.Errorf("unrecognized internal status escaped to the reader: %q", got)
			}
		})
	}
}

func TestTodo_UXAUDIT_006_I18N_CodeBackedFinding(t *testing.T) {
	input := []*journeyv1.Finding{
		{Code: "promotion.budget_authority_observation_only", Severity: "INFO", Message: "server English wording must not leak"},
		{Code: "other.finding", Severity: "WARNING", Message: "Unmapped finding"},
	}
	for _, tc := range []struct{ locale, want string }{
		{"en-US", "Finance confirmed the current budget baseline."},
		{"de-DE", "Die Finanzprüfung bestätigte die aktuelle Budgetgrundlage."},
		{"ar", "أكدت المالية أساس الميزانية الحالي."},
	} {
		got := findingsLocale(tc.locale, input)
		if len(got) != 2 || !strings.HasPrefix(got[0].Message, tc.want) || got[0].Code != input[0].Code || got[1].Message != "Unmapped finding" {
			t.Errorf("%s finding projection = %+v", tc.locale, got)
		}
	}
}

// TestDetailPageBeforeExecution keeps the unexecuted journey's absent
// sections absent rather than empty.
func TestDetailPageBeforeExecution(t *testing.T) {
	p := DetailPage(testConfig(), testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED), nil, nil)
	d := p.Detail
	if d.Engine != nil || d.Nodes != nil || d.WorkItems != nil || d.Ledger != nil {
		t.Error("a journey with no workflow instance rendered engine sections")
	}
	if len(d.Comparison) == 0 || len(d.Proposal) == 0 {
		t.Error("the proposal itself did not render before execution")
	}
}

// TestDetailPageOfNothing covers the state the client is in between a route
// change and its answer: the chrome and the notice must still render.
func TestDetailPageOfNothing(t *testing.T) {
	notice := &journey.Notice{Tone: toneDanger, Title: "No such journey"}
	p := DetailPage(testConfig(), nil, notice, nil)
	if p.Detail == nil {
		t.Fatal("DetailPage(nil) produced no view at all")
	}
	if p.Notice != notice {
		t.Error("the notice was dropped")
	}
	if !p.Detail.Unavailable || len(p.Detail.Steps) != 0 || p.Detail.Journey.StageLabel != "" {
		t.Error("the refused route rendered an invented journey state")
	}
	if p.Detail.BackLink.Href != "" || p.Detail.JourneysLink.Href == "" {
		t.Error("the refused route did not retain only its safe recovery link")
	}
	markup, err := journey.RenderToString(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Unknown stage", "Stages", "Recorded outcome", "No employee record has changed yet"} {
		if strings.Contains(markup, forbidden) {
			t.Errorf("the refused route rendered misleading content %q", forbidden)
		}
	}
	if len(p.Nav) != 2 {
		t.Error("the navigation was dropped, leaving no way back")
	}
}

func TestStatusTone(t *testing.T) {
	cases := map[string]string{
		"COMPLETED": toneSuccess, "Completed": toneSuccess, "APPROVED": toneSuccess,
		"RUNNING": toneWarning, "OPEN": toneWarning, "CLAIMED": toneWarning,
		"FAILED": toneDanger, "CANCELLED": toneDanger, "EXPIRED": toneDanger, "REJECTED": toneDanger,
		"PENDING": toneNeutral, "": toneNeutral,
		"SOMETHING_ELSE": toneInfo,
	}
	for in, want := range cases {
		if got := statusTone(in); got != want {
			t.Errorf("statusTone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSeverityOf(t *testing.T) {
	cases := map[string]string{
		"BLOCKING": severityBlocking, "error": severityBlocking, "Fatal": severityBlocking,
		"WARNING": severityWarning, "warn": severityWarning,
		"NEEDS_DATA": "needs-data", "needs-data": "needs-data",
		"SUCCESS": severitySuccess, "ok": severitySuccess,
		"INFO": severityInfo, "anything else": severityInfo, "": severityInfo,
	}
	for in, want := range cases {
		if got := severityOf(in); got != want {
			t.Errorf("severityOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFinanceStageCopyDistinguishesUnstartedFromAssignedReview(t *testing.T) {
	proposed := steps(stageProposed, nil, "2026-12-01")
	assigned := steps(stageFinanceApproval, nil, "2026-12-01")
	if !strings.Contains(proposed[1].Detail, "has not begun") {
		t.Fatalf("unstarted proposal implies finance assignment: %q", proposed[1].Detail)
	}
	if assigned[1].Detail != "Waiting for the assigned finance reviewer." {
		t.Fatalf("started approval still asks to start the workflow: %q", assigned[1].Detail)
	}
}

func TestTodo_UXAUDIT_006_PromotionStagesLocale(t *testing.T) {
	for _, tc := range []struct {
		locale, proposal, finance, waiting string
	}{
		{"en-US", "Proposal", "Finance review", "Waiting for the assigned finance reviewer."},
		{"de-DE", "Antrag", "Finanzprüfung", "Die zuständige Person in der Finanzabteilung muss entscheiden."},
		{"ar", "الطلب", "مراجعة المالية", "بانتظار قرار المراجع المالي المكلّف."},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			stages := stepsLocale(tc.locale, stageFinanceApproval, nil, "2026-12-01")
			if len(stages) != 5 || stages[0].Label != tc.proposal || stages[1].Label != tc.finance || stages[1].Detail != tc.waiting {
				t.Fatalf("unlocalized stages: %+v", stages)
			}
			for stage := range stepStates {
				for _, step := range stepsLocale(tc.locale, stage, nil, "2026-12-01") {
					if strings.HasPrefix(step.Label, "journey.") || strings.HasPrefix(step.Detail, "journey.") || step.Label == "" || step.Detail == "" {
						t.Fatalf("%s %s has unresolved step: %+v", tc.locale, stage, step)
					}
				}
			}
		})
	}
}

// TestProjectionPassesRawStringsToTheRenderer pins the escaping boundary:
// the projector hands through whatever the engine said, and the renderer is
// what turns "&" into "&amp;". A projection that escaped would show the
// reader "&amp;" on screen.
func TestProjectionPassesRawStringsToTheRenderer(t *testing.T) {
	j := testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	j.WorkerName = `Ola & "Bo" <b>`
	p := ListPage(testConfig(), ListData{Journeys: []*journeyv1.Journey{j}}, nil, nil)

	if got := p.List.Journeys[0].WorkerName; got != `Ola & "Bo" <b>` {
		t.Fatalf("the projection altered the engine's string: %q", got)
	}
	html, err := journey.RenderToString(p)
	if err != nil {
		t.Fatalf("rendering the projected list page: %v", err)
	}
	if strings.Contains(html, "<b>") {
		t.Error("the rendered page carries unescaped markup from a worker name")
	}
	if !strings.Contains(html, "&amp;") {
		t.Error("the renderer did not escape the ampersand it was handed")
	}
}

// TestProjectedPagesRender is the end-to-end shape check: both projections
// go through the real renderer without error and carry their own facts.
func TestProjectedPagesRender(t *testing.T) {
	cfg := testDiagnosticsAuthorizedConfig()

	list, err := journey.RenderToString(ListPage(cfg, ListData{
		Journeys: []*journeyv1.Journey{testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)},
		Workers:  testWorkers(),
		Options:  testWorkforceOptions(),
	}, &journey.Notice{Tone: toneInfo, Title: "Working…", Busy: true}, nil))
	if err != nil {
		t.Fatalf("rendering the list: %v", err)
	}
	for _, want := range []string{"Human Capital Management Suite", "Omar Reyes", "USD 93,000.00", "Awaiting approval", "Review and submit"} {
		if !strings.Contains(list, want) {
			t.Errorf("the rendered list page does not carry %q", want)
		}
	}

	detail, err := journey.RenderToString(DetailPage(cfg, testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED), nil, nil))
	if err != nil {
		t.Fatalf("rendering the detail: %v", err)
	}
	for _, want := range []string{"Omar Reyes", "promotion.approval@3", "hcm.promotion.recorded.v1", "1 Jun 2026"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the rendered detail page does not carry %q", want)
		}
	}
}

// ---------------------------------------------------------------------
// The workforce
//
// The fixture population mirrors the release's corpus (two of the four
// profiles, pay deliberately blank, because a corpus worker's compensation
// baseline is the ported scenario's rather than a fact of their own record)
// plus one employee recorded through this page, so both provenances are
// projected.
// ---------------------------------------------------------------------

const (
	testJaneID     = "11111111-1111-4111-8111-111111111111"
	testCreatedRef = "worker:created-rosa"
)

func testWorkers() []*journeyv1.Worker {
	return []*journeyv1.Worker{
		{
			WorkerRef: testCreatedRef, WorkerId: "99999999-9999-4999-8999-999999999999",
			SubjectRevision: "rewards.package.worker-created-rosa@1",
			LegalName:       "Rosa Iglesias", PreferredName: "Rosa", WorkerNumber: "W-2001",
			JobCode: "OPS-HRBP2", Grade: "P2", OrgUnit: "people-ops", PositionId: "POS-NEW-001",
			Location: "Barcelona, ES", PayZone: "US-EAST",
			BasePay: "72500", Currency: "USD", BonusTarget: "0.0500",
			HireDate: "2026-10-01", Source: "CREATED",
		},
		{
			WorkerRef: "jane-doe", WorkerId: testJaneID,
			SubjectRevision: "rewards.package.jane-doe@1",
			LegalName:       "Jane Doe", PreferredName: "Jane", WorkerNumber: "W-1001",
			JobCode: "ENG-SWE3", Grade: "P3", OrgUnit: "eng-platform", PositionId: "POS-SWE-118",
			Location: "San Francisco, CA", PayZone: "US-WEST",
			HireDate: "2019-03-04", Source: "CORPUS",
		},
		{
			WorkerRef: "omar-reyes", WorkerId: "22222222-2222-4222-8222-222222222222",
			SubjectRevision: "rewards.package.omar-reyes@1",
			LegalName:       "Omar Reyes", WorkerNumber: "W-1002",
			JobCode: "OPS-HRBP2", Grade: "P2", OrgUnit: "people-ops", PositionId: "POS-HRBP-204",
			Location: "Boston, MA", PayZone: "US-EAST",
			HireDate: "2019-07-15", Source: "CORPUS",
		},
	}
}

func testWorkforceOptions() *journeyv1.WorkforceOptions {
	return &journeyv1.WorkforceOptions{
		JobCodes:  []string{"CLN-NURSE4", "ENG-MGR1", "ENG-SWE3", "OPS-HRBP2", "OPS-HRBP3"},
		Grades:    []string{"M1", "N4", "P2", "P3"},
		OrgUnits:  []string{"eng-platform", "people-ops"},
		PayZones:  []string{"US-EAST", "US-WEST"},
		Positions: []string{"POS-HRBP-204", "POS-SWE-118"},
		Currency:  "USD",
		Placements: []*journeyv1.WorkforcePlacementOption{
			{JobCode: "CLN-NURSE4", Grade: "N4", PayZone: "US-EAST", Currency: "USD"},
			{JobCode: "ENG-MGR1", Grade: "M1", PayZone: "US-WEST", Currency: "USD"},
			{JobCode: "ENG-SWE3", Grade: "P3", PayZone: "US-WEST", Currency: "USD"},
			{JobCode: "OPS-HRBP2", Grade: "P2", PayZone: "US-EAST", Currency: "USD"},
			{JobCode: "OPS-HRBP3", Grade: "P3", PayZone: "US-EAST", Currency: "USD"},
		},
		PromotionPaths: []*journeyv1.PromotionPathOption{
			{PathRef: "path-eng-swe3-mgr1", Revision: "2026.1", SourceProfileRef: "profile-eng-swe3", SourceJobCode: "ENG-SWE3", SourceGrade: "P3", TargetProfileRef: "profile-eng-mgr1", TargetJobCode: "ENG-MGR1", TargetGrade: "M1", TargetTitle: "Engineering Manager", Kind: "UPWARD", MinimumBaseIncrease: "0.0500", MaximumBaseIncrease: "0.1800", CompensationPolicyRef: "promotion-rules@2026.1", BenefitRuleRefs: []string{"people-manager-benefit-eligibility@2026.1"}},
			{PathRef: "path-ops-hrbp2-hrbp3", Revision: "2026.1", SourceProfileRef: "profile-ops-hrbp2", SourceJobCode: "OPS-HRBP2", SourceGrade: "P2", TargetProfileRef: "profile-ops-hrbp3", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", TargetTitle: "Senior HR Business Partner", Kind: "UPWARD", MinimumBaseIncrease: "0.0500", MaximumBaseIncrease: "0.1500", CompensationPolicyRef: "promotion-rules@2026.1", BenefitRuleRefs: []string{"professional-benefit-eligibility@2026.1"}},
		},
	}
}

// workerJourneys are three journeys whose subjects are named the two ways
// the engine names them: by the reference the picker submitted, and by the
// entity id the governed read used.
func workerJourneys(t *testing.T) []*journeyv1.Journey {
	t.Helper()
	open := testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	open.WorkerRef = "omar-reyes"
	byID := testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)
	byID.WorkerRef = testJaneID
	done := testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	done.WorkerRef = "omar-reyes"
	// PROMOUX-012: closure is the server's projection, never read off stage.
	done.Viewer = &journeyv1.JourneyViewerProjection{Closed: true, Responsibility: journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_CLOSED}
	return []*journeyv1.Journey{open, byID, done, nil}
}

func testListData(t *testing.T, selected string) ListData {
	t.Helper()
	return ListData{
		Journeys:    workerJourneys(t),
		Workers:     testWorkers(),
		Options:     testWorkforceOptions(),
		SelectedRef: selected,
	}
}

func cardFor(people *journey.PeopleView, ref string) (journey.WorkerCard, bool) {
	for _, c := range people.Workers {
		if c.Ref == ref {
			return c, true
		}
	}
	return journey.WorkerCard{}, false
}

// describeWorker renders one row's plain fields for comparison, since the
// contract's card carries callbacks and is therefore not comparable.
func describeWorker(c journey.WorkerCard) string {
	return strings.Join([]string{
		"Ref=" + c.Ref, "Name=" + c.Name, "Number=" + c.Number,
		"Title=" + c.Title, "JobCode=" + c.JobCode, "Grade=" + c.Grade,
		"OrgUnit=" + c.OrgUnit, "Location=" + c.Location,
		"PayLine=" + c.PayLine, "HireDate=" + c.HireDate,
		"Source=" + c.Source, "SourceLabel=" + c.SourceLabel, "Tone=" + c.Tone,
		"ProposeHref=" + c.ProposeHref,
		"Selected=" + strconv.FormatBool(c.Selected),
		"OpenJourneys=" + strconv.Itoa(c.OpenJourneys),
	}, "\n")
}

func TestListPageProjectsThePeoplePanel(t *testing.T) {
	p := ListPage(testConfig(), testListData(t, "omar-reyes"), nil, nil)

	people := p.List.People
	if people == nil {
		t.Fatal("the list carries no People panel")
	}
	if len(people.Workers) != 3 {
		t.Fatalf("People shows %d employees, want 3", len(people.Workers))
	}
	if people.SelectedRef != "omar-reyes" {
		t.Errorf("SelectedRef = %q", people.SelectedRef)
	}
	if people.Note != PeopleNote || people.Empty != PeopleEmpty {
		t.Error("the panel is missing its note or its empty state")
	}
	if !strings.Contains(people.Note, "directory") {
		t.Errorf("the note = %q, want it to say what adding an employee does", people.Note)
	}

	rosa, ok := cardFor(people, testCreatedRef)
	if !ok {
		t.Fatal("the employee recorded through this page is missing from People")
	}
	want := journey.WorkerCard{
		Ref: testCreatedRef, Name: "Rosa", Number: "W-2001",
		Title: "HR Business Partner", JobCode: "OPS-HRBP2", Grade: "P2",
		OrgUnit: "People Ops", Location: "Barcelona, ES",
		PayLine: "USD 72,500.00", HireDate: "1 Oct 2026",
		Source: "CREATED", SourceLabel: "Created", Tone: toneInfo,
		ProposeHref: "#/journeys/new?worker=worker%3Acreated-rosa",
	}
	if rosa.OnSelect != nil || rosa.OnPropose != nil {
		t.Error("the projection bound a live callback; wiring is App.wire's job")
	}
	if got, wantText := describeWorker(rosa), describeWorker(want); got != wantText {
		t.Errorf("the created employee's card =\n%s\nwant\n%s", got, wantText)
	}

	// A corpus worker carries no pay of their own, and the row says so with
	// a dash rather than with a bare currency or an empty cell.
	jane, _ := cardFor(people, "jane-doe")
	if jane.PayLine != emDash {
		t.Errorf("the corpus worker's pay line = %q, want a dash", jane.PayLine)
	}
	if jane.Name != "Jane" || jane.Title != "Software Engineer III" ||
		jane.Source != "CORPUS" || jane.SourceLabel != "Corpus" || jane.Tone != "" {
		t.Errorf("the corpus worker's card = %+v", jane)
	}
	if jane.HireDate != "4 Mar 2019" {
		t.Errorf("the corpus worker's hire date = %q", jane.HireDate)
	}
	// Omar carries no preferred name, so the row falls back to the legal one.
	omar, _ := cardFor(people, "omar-reyes")
	if omar.Name != "Omar Reyes" {
		t.Errorf("the fallback name = %q, want the legal name", omar.Name)
	}
	if !omar.Selected || jane.Selected {
		t.Error("the selection marked the wrong row")
	}
}

func TestProposalPageKeepsOneWorkerAsImmutableContext(t *testing.T) {
	p := ProposalPage(testConfig(), testListData(t, "jane-doe"), nil, map[string]string{
		FieldWorker: "omar-reyes",
	})
	if p.Proposal == nil || p.List != nil || p.Detail != nil {
		t.Fatalf("focused proposal page shape = %+v", p)
	}
	v := p.Proposal
	if v.Subject == nil || v.Subject.Ref != "jane-doe" || v.Subject.Name != "Jane" || v.Subject.Title != "Software Engineer III" {
		t.Fatalf("proposal subject = %+v", v.Subject)
	}
	if v.BackHref != "/workspace/app/person?person=jane-doe" || v.JourneysLink.Href != ListHref() {
		t.Fatalf("proposal exits = back %q, journeys %+v", v.BackHref, v.JourneysLink)
	}
	if len(v.Form.Fields) == 0 || v.Form.Fields[0].Kind != kindHidden || v.Form.Fields[0].Name != NameWorker || v.Form.Fields[0].Value != "jane-doe" {
		t.Fatalf("worker context is not pinned by the route: %+v", v.Form.Fields)
	}
	for _, field := range v.Form.Fields {
		if field.Name == NameWorker && field.Kind == kindSelect {
			t.Fatal("focused proposal rendered an editable worker selector")
		}
	}
}

func TestProposalPageOffersOnlyPublishedNextRolesAndExplainsTheirRules(t *testing.T) {
	p := ProposalPage(testConfig(), testListData(t, "omar-reyes"), nil, map[string]string{
		FieldJobCode: "OPS-HRBP3", FieldGrade: "P3",
	})
	form := p.Proposal.Form
	jobs, ok := fieldByID(form.Fields, FieldJobCode)
	if !ok {
		t.Fatal("proposal form has no next-role field")
	}
	if len(jobs.Options) != 2 || jobs.Options[1].Value != "OPS-HRBP3" || jobs.Options[1].Label != "OPS-HRBP3 — Senior HR Business Partner" {
		t.Fatalf("Omar's governed next roles = %+v", jobs.Options)
	}
	for _, option := range jobs.Options {
		if option.Value == "CLN-NURSE4" || option.Value == "OPS-HRBP2" {
			t.Fatalf("unrelated or current role leaked into the ladder choices: %+v", jobs.Options)
		}
	}
	base, _ := fieldByID(form.Fields, FieldBase)
	for _, want := range []string{"5.00%", "15.00%", "Benefit eligibility is reviewed separately"} {
		if !strings.Contains(base.Help, want) {
			t.Errorf("base-pay guidance %q does not explain %q", base.Help, want)
		}
	}
	for _, forbidden := range []string{"promotion-rules@", "professional-benefit-eligibility@", "ladder edge"} {
		if strings.Contains(base.Help, forbidden) {
			t.Errorf("base-pay guidance leaks implementation term %q: %q", forbidden, base.Help)
		}
	}
}

func TestTodo_UXAUDIT_006_PromotionFormLocale(t *testing.T) {
	cases := []struct {
		locale, title, role, grade, rule string
	}{
		{"en-US", "Promote Omar Reyes", "Next role", "Target grade", "For this role, base pay must increase"},
		{"de-DE", "Omar Reyes befördern", "Nächste Rolle", "Zielstufe", "Für diese Rolle muss das Grundgehalt"},
		{"ar", "ترقية Omar Reyes", "الوظيفة التالية", "الدرجة المستهدفة", "يجب أن يرتفع الأجر الأساسي"},
	}
	for _, tc := range cases {
		t.Run(tc.locale, func(t *testing.T) {
			cfg := testConfig()
			cfg.Locale = tc.locale
			p := ProposalPage(cfg, testListData(t, "omar-reyes"), nil, map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3"})
			if p.Locale != tc.locale || !strings.Contains(p.Title, tc.title) {
				t.Fatalf("locale/title = %q / %q", p.Locale, p.Title)
			}
			role, _ := fieldByID(p.Proposal.Form.Fields, FieldJobCode)
			grade, _ := fieldByID(p.Proposal.Form.Fields, FieldGrade)
			base, _ := fieldByID(p.Proposal.Form.Fields, FieldBase)
			if role.Label != tc.role || grade.Label != tc.grade || !strings.Contains(base.Help, tc.rule) || !strings.Contains(base.Help, "5.00%") || !strings.Contains(base.Help, "15.00%") {
				t.Fatalf("localized form = role %q, grade %q, rule %q", role.Label, grade.Label, base.Help)
			}
			for _, forbidden := range []string{"promotion-rules@", "professional-benefit-eligibility@", "ladder edge", "⟦"} {
				if strings.Contains(base.Help, forbidden) {
					t.Fatalf("locale %s leaked %q in %q", tc.locale, forbidden, base.Help)
				}
			}
		})
	}
}

func TestTodo_UXAUDIT_006_MissingPromotionRangeIsExplained(t *testing.T) {
	path := &journeyv1.PromotionPathOption{BenefitRuleRefs: []string{"internal-benefit-ref"}}
	for _, tc := range []struct{ locale, expected string }{
		{"en-US", "An exact base-pay range is not available here"},
		{"de-DE", "Hier ist keine genaue Grundgehaltsspanne verfügbar"},
		{"ar", "نطاق الأجر الأساسي الدقيق غير متاح هنا"},
	} {
		result := promotionPathRuleHelpLocale(path, productui.ResolveProductLocale(tc.locale))
		if !strings.Contains(result, tc.expected) || strings.Contains(result, "from  to") || strings.Contains(result, "من إلى") || strings.Contains(result, "internal-benefit-ref") {
			t.Errorf("locale %s missing-range guidance = %q", tc.locale, result)
		}
	}
}

func TestTodo_UXAUDIT_006_PromotionSubjectUsesAuthorizedJobTitle(t *testing.T) {
	worker := &journeyv1.Worker{WorkerRef: "demo-worker", JobCode: "SAL-AE3", JobTitle: "Senior Account Executive"}
	subject := promotionSubject(worker, nil, productui.ResolveProductLocale("en-US"))
	if subject == nil || subject.Title != "Senior Account Executive" {
		t.Fatalf("promotion subject displayed raw code instead of title: %+v", subject)
	}
}

func TestTodo_UXAUDIT_006_PromotionSubjectFormatsPayForLocale(t *testing.T) {
	worker := &journeyv1.Worker{WorkerRef: "demo-worker", JobCode: "SAL-AE3", JobTitle: "Senior Account Executive", Grade: "P4", BasePay: "135000.00", Currency: "USD"}
	for _, tc := range []struct{ locale, want string }{
		{"en-US", "USD\u00a0135,000.00"},
		{"de-DE", "135.000,00\u00a0USD"},
	} {
		subject := promotionSubject(worker, nil, productui.ResolveProductLocale(tc.locale))
		if subject == nil || subject.PayLine != tc.want {
			t.Errorf("%s subject pay = %+v, want %q", tc.locale, subject, tc.want)
		}
	}
}

func TestTodo_UXAUDIT_006_NoPromotionPathUsesLocale(t *testing.T) {
	for _, tc := range []struct{ locale, expected string }{
		{"de-DE", "Für diese Person ist keine nächste Rolle verfügbar"},
		{"ar", "لا توجد وظيفة تالية متاحة لهذا الموظف"},
	} {
		cfg := testConfig()
		cfg.Locale = tc.locale
		p := ProposalPage(cfg, ListData{SelectedRef: "adrian", Workers: []*journeyv1.Worker{{WorkerRef: "adrian", JobCode: "SAL-AE3", Grade: "P4"}}, Options: &journeyv1.WorkforceOptions{}}, nil, nil)
		if !p.Proposal.Form.Disabled || !strings.Contains(p.Proposal.Form.DisabledReason, tc.expected) {
			t.Errorf("locale %s no-path reason = %q", tc.locale, p.Proposal.Form.DisabledReason)
		}
	}
}

func TestPersonHrefEscapesReservedWorkerReferences(t *testing.T) {
	if got, want := personHref("worker/a+b & c"), "/workspace/app/person?person=worker%2Fa%2Bb+%26+c"; got != want {
		t.Fatalf("personHref = %q, want %q", got, want)
	}
}

func TestProposalPageRefusesAnUnreadableSubject(t *testing.T) {
	p := ProposalPage(testConfig(), testListData(t, "not-visible"), nil, nil)
	if p.Proposal.Subject != nil || !p.Proposal.Form.Disabled {
		t.Fatalf("unreadable subject proposal = %+v", p.Proposal)
	}
	if !strings.Contains(p.Proposal.Form.DisabledReason, "not available") {
		t.Fatalf("disabled reason = %q", p.Proposal.Form.DisabledReason)
	}
}

func TestProposalPageDoesNotMountItsFormWhileWorkerContextLoads(t *testing.T) {
	p := ProposalPage(testConfig(), ListData{SelectedRef: "jane-doe"}, busy("journey.busy_employee", "Reading the employee."), nil)
	if p.Proposal == nil || !p.Proposal.Loading {
		t.Fatalf("loading proposal = %+v", p.Proposal)
	}
	out, err := journey.RenderToString(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<form") || strings.Contains(out, "disabled") {
		t.Fatalf("loading projection mounted transient form state: %s", out)
	}
}

// TestPeopleCountsOnlyOpenJourneys covers both halves of the count: a
// terminal journey is not open, and a journey that names its subject by the
// entity id still belongs to that employee.
func TestPeopleCountsOnlyOpenJourneys(t *testing.T) {
	p := ListPage(testConfig(), testListData(t, ""), nil, nil)
	people := p.List.People

	omar, _ := cardFor(people, "omar-reyes")
	if omar.OpenJourneys != 1 {
		t.Errorf("Omar has %d open journeys, want 1 (the completed one is not open)", omar.OpenJourneys)
	}
	jane, _ := cardFor(people, "jane-doe")
	if jane.OpenJourneys != 1 {
		t.Errorf("Jane has %d open journeys, want 1 (hers is named by entity id)", jane.OpenJourneys)
	}
	rosa, _ := cardFor(people, testCreatedRef)
	if rosa.OpenJourneys != 0 {
		t.Errorf("the new employee has %d open journeys, want none", rosa.OpenJourneys)
	}
}

// TestListPageWithoutAWorkforceAnswerHasNoPanel is the absent-rather-than-
// empty rule: a page that could not read the workforce must not claim there
// is nobody in it.
func TestListPageWithoutAWorkforceAnswerHasNoPanel(t *testing.T) {
	p := ListPage(testConfig(), ListData{Journeys: workerJourneys(t)}, nil, nil)
	if p.List.People != nil {
		t.Error("a page with no workforce answer drew a People panel anyway")
	}

	// An answered read of an empty tenant is a different thing: the panel is
	// there, with its empty state and its form.
	empty := ListPage(testConfig(), ListData{Options: testWorkforceOptions()}, nil, nil)
	if empty.List.People == nil {
		t.Fatal("an empty tenant lost its People panel")
	}
	if len(empty.List.People.Workers) != 0 || empty.List.People.Empty == "" {
		t.Error("the empty tenant's panel has no empty state")
	}
	if len(empty.List.People.Form.Fields) == 0 {
		t.Error("the empty tenant's panel offers no way to record the first employee")
	}
}

func TestWorkerFormShape(t *testing.T) {
	form := WorkerForm(testWorkforceOptions(), nil, nil)

	if len(form.Hidden) != 0 {
		t.Errorf("Hidden = %v, want empty: this client submits over gRPC", form.Hidden)
	}
	if form.Submit == "" || form.Action != ListHref() {
		t.Errorf("form = %+v, want a submit label and the page's own address", form)
	}

	want := []struct {
		id, name, kind, value string
		required              bool
	}{
		{FieldWorkerLegalName, NameLegalName, kindText, "", true},
		{FieldWorkerPreferredName, NamePreferredName, kindText, "", false},
		{FieldWorkerJobCode, NameWorkerJobCode, kindSelect, "CLN-NURSE4", true},
		{FieldWorkerGrade, NameWorkerGrade, kindSelect, "M1", true},
		{FieldWorkerOrgUnit, NameOrgUnit, kindSelect, "eng-platform", true},
		{FieldWorkerPosition, NameWorkerPosition, kindText, "POS-HRBP-204", false},
		{FieldWorkerLocation, NameLocation, kindText, "", false},
		{FieldWorkerPayZone, NamePayZone, kindSelect, "US-EAST", true},
		{FieldWorkerBasePay, NameWorkerBasePay, kindNumber, "", true},
		{FieldWorkerCurrency, NameCurrency, kindHidden, "USD", false},
		{FieldWorkerBonusTarget, NameBonusTarget, kindNumber, DefaultBonusTarget, false},
		{FieldWorkerHireDate, NameHireDate, kindDate, DefaultHireDate(time.Now()), true},
		{FieldWorkerManager, NameManagerRef, kindText, "", false},
	}
	if len(form.Fields) != len(want) {
		t.Fatalf("the form has %d fields, want %d", len(form.Fields), len(want))
	}
	for i, w := range want {
		f := form.Fields[i]
		if f.ID != w.id || f.Name != w.name || f.Kind != w.kind || f.Value != w.value || f.Required != w.required {
			t.Errorf("field %d = %+v, want id=%s name=%s kind=%s value=%q required=%v",
				i, f, w.id, w.name, w.kind, w.value, w.required)
		}
	}

	base := form.Fields[8]
	if base.Step != "0.01" || base.Prefix != "USD" {
		t.Errorf("the base pay field = %+v, want step 0.01 and the catalog's currency", base)
	}
	bonus := form.Fields[10]
	if bonus.Step != "0.0001" || bonus.Suffix != "ratio" {
		t.Errorf("the bonus field = %+v, want a ratio stepped to 0.0001", bonus)
	}

	// Every closed set is the cell's, and the job codes are named as well as
	// coded so the picker can be used without the code list memorised.
	jobs := form.Fields[2]
	if len(jobs.Options) != 5 {
		t.Fatalf("the job codes = %+v, want the cell's five", jobs.Options)
	}
	found := ""
	for _, o := range jobs.Options {
		if o.Value == "OPS-HRBP3" {
			found = o.Label
		}
	}
	if found != "OPS-HRBP3 — Senior HR Business Partner" {
		t.Errorf("the OPS-HRBP3 option reads %q", found)
	}
	if !jobs.Options[0].Selected || jobs.Options[1].Selected {
		t.Error("the job code select marks no option, or the wrong one")
	}
}

func TestWorkerFormKeepsWhatTheReaderTyped(t *testing.T) {
	values := map[string]string{
		FieldWorkerLegalName: "Rosa Iglesias",
		FieldWorkerJobCode:   "OPS-HRBP3",
		FieldWorkerBasePay:   "104000",
		FieldWorkerHireDate:  "2027-01-01",
	}
	form := WorkerForm(testWorkforceOptions(), values, map[string]string{NameWorkerBasePay: "outside every band for this placement"})

	if form.Fields[0].Value != "Rosa Iglesias" || form.Fields[8].Value != "104000" ||
		form.Fields[12].Value != "" || form.Fields[11].Value != "2027-01-01" {
		t.Error("the form did not carry the reader's own values back")
	}
	if form.Fields[2].Value != "OPS-HRBP3" {
		t.Errorf("the job code select = %q, want the reader's choice", form.Fields[2].Value)
	}
	for _, o := range form.Fields[2].Options {
		if o.Selected != (o.Value == "OPS-HRBP3") {
			t.Errorf("option %q selected=%v", o.Value, o.Selected)
		}
	}
	if form.Fields[8].Error == "" {
		t.Error("the violation the engine named did not land on its field")
	}
	if form.Fields[0].Error != "" {
		t.Error("a violation landed on a field it did not name")
	}
}

// TestWorkerFormAsksForACurrencyWhenTheCellDeclaresNone covers the one
// control whose kind depends on the cell.
func TestWorkerFormAsksForACurrencyWhenTheCellDeclaresNone(t *testing.T) {
	options := testWorkforceOptions()
	options.Currency = ""
	form := WorkerForm(options, nil, nil)

	currency := form.Fields[9]
	if currency.Kind != kindText || !currency.Required || currency.Value != DefaultCurrency {
		t.Errorf("the currency field = %+v, want a required text box seeded with %s", currency, DefaultCurrency)
	}
	if form.Fields[8].Prefix != DefaultCurrency {
		t.Errorf("the base pay prefix = %q", form.Fields[8].Prefix)
	}
}

// TestWorkerFormWithoutOptionsStillOffersAPosition covers a cell that
// published no closed sets at all: the selects are empty, and the defaults
// this client owns are still there.
func TestWorkerFormWithoutOptionsStillOffersAPosition(t *testing.T) {
	form := WorkerForm(nil, nil, nil)
	if form.Fields[5].Value != DefaultPositionID {
		t.Errorf("the position = %q, want %q", form.Fields[5].Value, DefaultPositionID)
	}
	if len(form.Fields[2].Options) != 0 || form.Fields[2].Value != "" {
		t.Error("the job code select invented options")
	}
	if form.Fields[9].Kind != kindText {
		t.Error("a cell with no declared currency did not ask for one")
	}
}

func TestProposalFormListsEveryWorkerIncludingTheCreatedOnes(t *testing.T) {
	form := ProposalForm(nil, testWorkers(), testCreatedRef)

	options := form.Fields[0].Options
	if len(options) != 4 {
		t.Fatalf("the worker select has %d options, want the prompt plus three employees", len(options))
	}
	if options[0].Value != "" || options[0].Selected {
		t.Errorf("the first option = %+v, want an unselected prompt", options[0])
	}
	labels := map[string]string{}
	for _, o := range options[1:] {
		labels[o.Value] = o.Label
	}
	if labels[testCreatedRef] != "Rosa — OPS-HRBP2 · P2" {
		t.Errorf("the created employee's option = %q", labels[testCreatedRef])
	}
	if labels["jane-doe"] != "Jane — ENG-SWE3 · P3" {
		t.Errorf("the corpus employee's option = %q", labels["jane-doe"])
	}
	if form.Fields[0].Value != testCreatedRef {
		t.Errorf("the select's value = %q, want the selected employee", form.Fields[0].Value)
	}
	for _, o := range options {
		if o.Selected != (o.Value == testCreatedRef) {
			t.Errorf("option %q selected=%v", o.Value, o.Selected)
		}
	}

	// A value the reader chose outranks the table's selection: they are
	// looking at the select they just used.
	typed := ProposalForm(map[string]string{FieldWorker: "jane-doe"}, testWorkers(), testCreatedRef)
	if typed.Fields[0].Value != "jane-doe" {
		t.Errorf("the reader's own choice = %q", typed.Fields[0].Value)
	}
}

func TestJobTitle(t *testing.T) {
	cases := map[string]string{
		"OPS-HRBP2":  "HR Business Partner",
		"OPS-HRBP3":  "Senior HR Business Partner",
		"ENG-SWE3":   "Software Engineer III",
		"ENG-MGR1":   "Engineering Manager",
		"CLN-NURSE4": "Registered Nurse IV",
		// An unknown code is its own title: inventing one from the code's
		// shape would put a fact on the page no system asserted.
		"XYZ-NEW9": "XYZ-NEW9",
		"":         "",
	}
	for code, want := range cases {
		if got := JobTitle(code); got != want {
			t.Errorf("JobTitle(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestDefaultHireDate(t *testing.T) {
	cases := map[string]string{
		"2026-09-03T14:05:00Z": "2026-10-01",
		"2026-12-31T23:59:00Z": "2027-01-01",
		"2026-01-01T00:00:00Z": "2026-02-01",
	}
	for now, want := range cases {
		parsed, err := time.Parse(time.RFC3339, now)
		if err != nil {
			t.Fatalf("parsing %q: %v", now, err)
		}
		if got := DefaultHireDate(parsed); got != want {
			t.Errorf("DefaultHireDate(%s) = %q, want %q", now, got, want)
		}
	}
}

// TestThePeoplePanelRenders is the end-to-end shape check for the workforce
// half of the list page.
func TestThePeoplePanelRenders(t *testing.T) {
	html, err := journey.RenderToString(ListPage(testConfig(), testListData(t, "omar-reyes"), nil, nil))
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	for _, want := range []string{
		"People", "Rosa", "Jane", "Omar Reyes",
		"Senior HR Business Partner", "USD 72,500.00", "Created", "Corpus",
		"New employee", "Add employee", "Selected",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the rendered People panel does not carry %q", want)
		}
	}
}

func TestTodo_UXAUDIT_006_PromotionRuleHelpHidesPolicyIdentifiers(t *testing.T) {
	path := &journeyv1.PromotionPathOption{
		MinimumBaseIncrease: "0.0500", MaximumBaseIncrease: "0.1800",
		CompensationPolicyRef: "promotion-rules@2026.1",
		BenefitRuleRefs:       []string{"people-manager-benefit-eligibility@2026.1"},
	}
	help := promotionPathRuleHelp(path)
	for _, want := range []string{"5.00%", "18.00%", "Benefit eligibility is reviewed separately"} {
		if !strings.Contains(help, want) {
			t.Errorf("promotion help missing %q: %q", want, help)
		}
	}
	for _, forbidden := range []string{"ladder edge", "promotion-rules@", "benefit-eligibility@"} {
		if strings.Contains(help, forbidden) {
			t.Errorf("promotion help exposed %q: %q", forbidden, help)
		}
	}
}
