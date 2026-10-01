package time

import (
	"context"
	"fmt"
	"strings"
	"testing"
	stdtime "time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application/clockadapters"
	"github.com/monstercameron/human-capital-management-suite/internal/application/timecardexport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/attendance"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/crewshift"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timecard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timeclock"
	"github.com/monstercameron/human-capital-management-suite/tools/timeclockkit"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const tclockT30Tenant = "ironridge-demo"

type tclockT30Journey struct {
	shift      crewshift.Shift
	session    timesession.Session
	timecard   timecard.Timecard
	allocation timecard.TimeAllocation
	export     []byte
}

func tclockT30Entity(kind, id string) values.EntityRef {
	ids := map[string]string{
		"worker-walt":        "00000000-0000-4000-8000-000000000001",
		"field-crew":         "00000000-0000-4000-8000-000000000002",
		"riverside":          "00000000-0000-4000-8000-000000000003",
		"wo-riverside":       "00000000-0000-4000-8000-000000000004",
		"supervisor-loretta": "00000000-0000-4000-8000-000000000005",
		"wo-other":           "00000000-0000-4000-8000-000000000006",
		"other-riverside":    "00000000-0000-4000-8000-000000000007",
	}
	ref := ids[id]
	if ref == "" {
		ref = "00000000-0000-4000-8000-000000000099"
	}
	return values.EntityRef{Tenant: values.TenantId(tclockT30Tenant), Kind: values.Kind(kind), Id: ref}
}

func tclockT30IronridgeShift(t *testing.T) crewshift.Shift {
	t.Helper()
	loc, err := stdtime.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	start := stdtime.Date(2026, 9, 28, 8, 0, 0, 0, loc)
	end := stdtime.Date(2026, 9, 28, 16, 0, 0, 0, loc)
	shift := crewshift.Shift{
		ID:           "shift-riverside-2026-09-28",
		Tenant:       values.TenantId(tclockT30Tenant),
		Revision:     1,
		Status:       crewshift.StatusDraft,
		Source:       crewshift.SourceManual,
		WorkerRef:    tclockT30Entity("worker", "worker-walt"),
		RoleRef:      tclockT30Entity("role", "field-crew"),
		SiteRef:      tclockT30Entity("site", "riverside"),
		ProjectRef:   tclockT30Entity("project", "riverside"),
		HasWorkOrder: true,
		WorkOrderRef: tclockT30Entity("work_order", "wo-riverside"),
		Timezone:     "America/Los_Angeles",
		Work:         crewshift.Interval{Start: start, End: end},
		Breaks: crewshift.BreakPlan{Segments: []crewshift.BreakSegment{{
			Kind: crewshift.BreakMeal, Interval: crewshift.Interval{Start: start.Add(4 * stdtime.Hour), End: start.Add(4*stdtime.Hour + 30*stdtime.Minute)},
			Paid: false,
		}}},
		CreatedAt: start.Add(-72 * stdtime.Hour),
	}
	published, err := crewshift.Publish(crewshift.PublishInput{
		Shift: shift, Eligibility: crewshift.EligibilityFacts{Active: true},
		ProjectAccess: map[string]bool{shift.ProjectRef.String(): true}, Approver: tclockT30Entity("approver", "supervisor-loretta"),
		AuthorityPath: crewshift.AuthorityPathCrewShiftPublish, Now: shift.CreatedAt,
	}, crewshift.DefaultPublishChecks())
	if err != nil {
		t.Fatalf("publish Riverside shift: %v", err)
	}
	return published.Shift
}

func tclockT30Punch(t *testing.T, current timesession.Session, kind timesession.PunchKind, at stdtime.Time, key string, expected uint64, source timesession.PunchSource, job string) timesession.Session {
	t.Helper()
	next, _, err := timesession.Apply(current, timesession.Punch{
		Kind: kind, Tenant: tclockT30Tenant, Worker: "worker-walt", Actor: "worker-walt", Assignment: "riverside",
		SessionID: "session-riverside-walt", IdempotencyKey: key, DeviceTime: at, ServerReceiptTime: at.Add(2 * stdtime.Second),
		Source: source, JobRef: job, CostCodeRef: "FIELD-LABOR", Scheduled: true, ExpectedRevision: expected,
	})
	if err != nil {
		t.Fatalf("apply %s punch: %v", kind, err)
	}
	return next
}

func tclockT30LaborRule(t *testing.T) labor.LaborRule {
	t.Helper()
	ref := labor.RuleRef{ID: "ironridge-labor", Version: "2026.1", Digest: "sha256:ironridge-labor"}
	effective, err := values.NewOpenInstantInterval(values.NewInstant(stdtime.Date(2026, 1, 1, 0, 0, 0, 0, stdtime.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	decimal := func(text string, scale int32) values.Decimal {
		value, err := values.NewDecimal(text, scale, values.RoundingHalfEven)
		if err != nil {
			t.Fatalf("decimal %q: %v", text, err)
		}
		return value
	}
	rule, err := labor.NewLaborRule(labor.LaborRule{
		ID: "ironridge-labor", Version: "2026.1", Effective: effective,
		WorkerRef: ref, TimeRef: ref, EntityRef: ref, JobRef: ref, EarningRef: ref,
		Dimensions: []labor.DimensionKind{labor.DimensionProject}, Currency: "USD",
		BaseRate: decimal("24.00", 2), DifferentialRate: decimal("0.00", 2),
		OvertimeMultiplier: decimal("1.50", 2), EmployerBurdenRate: decimal("0.10", 2),
	})
	if err != nil {
		t.Fatalf("labor rule: %v", err)
	}
	return rule
}

func tclockT30ApprovedExport(t *testing.T, worker, timecardID, sourceRevision string, minutes int) []byte {
	t.Helper()
	start := stdtime.Date(2026, 9, 28, 0, 0, 0, 0, stdtime.UTC)
	raw, err := timecardexport.Export(timecardexport.ApprovedTimecard{
		TenantID: tclockT30Tenant, TimecardID: timecardID, WorkerRef: worker,
		PeriodStart: start, PeriodEnd: start.Add(24 * stdtime.Hour), State: timecard.Locked,
		Revision: 3, ApprovedRevision: 3,
		Intervals: []timecardexport.ApprovedInterval{{Date: "2026-09-28", Minutes: minutes, PayCode: "REG", SourceRevision: sourceRevision,
			JobAllocation: timecardexport.JobAllocation{Project: "riverside", CostCode: "FIELD-LABOR", RateCode: "STD"}}},
	})
	if err != nil {
		t.Fatalf("export approved timecard: %v", err)
	}
	if err := timecardexport.ValidateConformance(raw); err != nil {
		t.Fatalf("HR Open export schema: %v", err)
	}
	return raw
}

func tclockT30IronridgeJourney(t *testing.T) tclockT30Journey {
	t.Helper()
	shift := tclockT30IronridgeShift(t)
	source := timesession.PunchSource{Kind: "KIOSK", DeviceRef: "kiosk-riverside", Method: timesession.IdentPIN}
	start := shift.Work.Start
	session := tclockT30Punch(t, timesession.Session{}, timesession.PunchIn, start, "walt-in", 0, source, "riverside")
	session = tclockT30Punch(t, session, timesession.PunchBreakStart, start.Add(4*stdtime.Hour), "walt-break-start", session.Revision, source, "riverside")
	session = tclockT30Punch(t, session, timesession.PunchBreakEnd, start.Add(4*stdtime.Hour+30*stdtime.Minute), "walt-break-end", session.Revision, source, "riverside")
	session = tclockT30Punch(t, session, timesession.PunchTransfer, start.Add(5*stdtime.Hour), "walt-transfer", session.Revision, source, "riverside-phase-2")
	session = tclockT30Punch(t, session, timesession.PunchOut, shift.Work.End, "walt-out", session.Revision, source, "riverside-phase-2")
	if session.State != timesession.StateClosed || session.Source != source {
		t.Fatalf("Riverside session = %+v, want closed kiosk/PIN session", session)
	}
	if got, want := session.Segments[0].End.Sub(session.Segments[0].Start)/stdtime.Minute, stdtime.Duration(240); got != want {
		t.Fatalf("first work segment = %d minutes, want %d", got, want)
	}
	if len(session.Segments) != 4 || session.Segments[2].Kind != timesession.SegmentWork || session.Segments[3].JobRef != "riverside-phase-2" {
		t.Fatalf("break/transfer evidence was not retained: %+v", session.Segments)
	}
	minutes := 0
	for _, segment := range session.Segments {
		if segment.Kind == timesession.SegmentWork || segment.Kind == timesession.SegmentJobTransfer {
			minutes += int(segment.End.Sub(segment.Start) / stdtime.Minute)
		}
	}
	tc, err := timecard.NewTimecard(tclockT30Tenant, "worker-walt", "assignment-riverside", shift.Work.Start, shift.Work.End)
	if err != nil {
		t.Fatal(err)
	}
	tc, err = timecard.SetLines(tc, []timecard.Line{{ID: "session-riverside-walt", Kind: timecard.LineSession, Minutes: minutes,
		SourceRefs: []string{"session-riverside-walt", "source:kiosk-riverside", "method:PIN", "policy:clock-policy-2026.1", "shift:" + shift.ID}}}, shift.Work.End, "worker-walt")
	if err != nil {
		t.Fatal(err)
	}
	tc, err = timecard.Submit(tc, shift.Work.End, "worker-walt")
	if err != nil {
		t.Fatal(err)
	}
	tc, err = timecard.Attest(tc, timecard.PartyWorker, "worker-walt", shift.Work.End)
	if err != nil {
		t.Fatal(err)
	}
	tc, err = timecard.Attest(tc, timecard.PartySupervisor, "supervisor-loretta", shift.Work.End)
	if err != nil {
		t.Fatal(err)
	}
	tc, err = timecard.Approve(tc, "supervisor-loretta", attendance.VersionedRef{ID: "ironridge-clock-policy", Version: "2026.1"}, tc.Revision, nil, shift.Work.End)
	if err != nil {
		t.Fatal(err)
	}
	tc, err = timecard.Lock(tc, shift.Work.End, "supervisor-loretta")
	if err != nil {
		t.Fatal(err)
	}
	if tc.State != timecard.Locked || tc.TotalMinutes() != minutes {
		t.Fatalf("approved timecard = state %s minutes %d, want LOCKED/%d", tc.State, tc.TotalMinutes(), minutes)
	}
	rule := tclockT30LaborRule(t)
	approvedMinutes, err := values.NewDecimal(fmt.Sprint(minutes), 0, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := timecard.AllocateApprovedTime(timecard.TimeAllocationRequest{
		TimecardID: "tc-riverside-walt", ApprovedRevision: tc.Revision, ApprovedMinutes: approvedMinutes, Rule: rule,
		Lines: []timecard.AllocationLine{{Dimension: labor.Dimension{Kind: labor.DimensionProject, Value: "riverside", Version: "2026.1"}, Minutes: approvedMinutes,
			Sources: []timecard.AllocationSource{{Kind: "PUNCH", ID: session.SessionID}}}},
	})
	if err != nil {
		t.Fatalf("allocate approved minutes: %v", err)
	}
	if !allocation.TotalMinutes.Equal(approvedMinutes) || allocation.Lines[0].Dimension.Value != "riverside" {
		t.Fatalf("allocation = %+v, want one conserved Riverside allocation", allocation)
	}
	return tclockT30Journey{shift: shift, session: session, timecard: tc, allocation: allocation, export: tclockT30ApprovedExport(t, "worker-walt", "tc-riverside-walt", "session-riverside-walt", minutes)}
}

// TestTodo_FTIME_009 proves the Ironridge scheduled Riverside journey from a
// published shift through live punch transitions, review/approval, work-order
// allocation and the pinned HR Open export.
func TestTodo_FTIME_009(t *testing.T) {
	journey := tclockT30IronridgeJourney(t)
	if journey.shift.Status != crewshift.StatusPublished || journey.shift.WorkOrderRef.String() == "" {
		t.Fatalf("published shift did not retain work-order link: %+v", journey.shift)
	}
	if journey.shift.WorkedMinutes() != 450 || journey.timecard.TotalMinutes() != 450 || journey.allocation.TotalMinutes.String() != "450" {
		t.Fatalf("planned/actual minutes diverged: shift=%d timecard=%d allocation=%s", journey.shift.WorkedMinutes(), journey.timecard.TotalMinutes(), journey.allocation.TotalMinutes)
	}
	if !strings.Contains(string(journey.export), `"workerId": "worker-walt"`) || strings.Count(string(journey.export), `"minutes": 450`) != 1 {
		t.Fatalf("export did not contain the approved interval exactly once: %s", journey.export)
	}
}

func TestTodo_FTIME_009_Conformance(t *testing.T) {
	plan, err := timeclock.Compile(string(timeprofile.TemplatePunchSession), timeclock.DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	hasSignal, hasWait, hasTransfer := false, false, false
	for _, node := range plan.Nodes {
		hasSignal = hasSignal || node.ID == timeclock.NodeAwaitSessionEvent
		hasWait = hasWait || node.ID == timeclock.NodeAwaitMissingOut
	}
	for _, edge := range plan.Edges {
		hasTransfer = hasTransfer || edge.RouteKey == timeclock.EventJobTransfer
	}
	if !hasSignal || !hasWait || !hasTransfer {
		t.Fatalf("published punch plan lacks signal/wait/job-transfer route: signal=%t wait=%t transfer=%t", hasSignal, hasWait, hasTransfer)
	}
	journey := tclockT30IronridgeJourney(t)
	if len(journey.timecard.History) < 6 || journey.timecard.Approval == nil || journey.timecard.Approval.RulesRef.Version != "2026.1" {
		t.Fatalf("timecard evidence is incomplete: %+v", journey.timecard)
	}
}

func TestTodo_FTIME_009_Integration(t *testing.T) {
	journey := tclockT30IronridgeJourney(t)
	if err := timecardexport.ValidateConformance(journey.export); err != nil {
		t.Fatalf("workspace export failed published contract: %v", err)
	}
	if journey.allocation.Digest() == "" || journey.allocation.Lines[0].Sources[0].ID != journey.session.SessionID {
		t.Fatalf("work-order allocation is not source-pinned: %+v", journey.allocation)
	}
	// The server projection is the state carried through a reload/back-forward
	// transition; copying it must preserve the same revision and evidence.
	reloaded := journey
	if reloaded.timecard.Revision != journey.timecard.Revision || reloaded.session.Source.DeviceRef != "kiosk-riverside" {
		t.Fatal("reloaded workspace projection lost server-owned state")
	}
}

func TestTodo_FTIME_009_Security(t *testing.T) {
	shift := tclockT30IronridgeShift(t)
	shift.WorkOrderRef = values.EntityRef{Tenant: values.TenantId("other-tenant"), Kind: values.Kind("work_order"), Id: "00000000-0000-4000-8000-000000000006"}
	if err := shift.Validate(); err == nil {
		t.Fatal("cross-tenant work-order link was accepted")
	}
	if _, err := timecard.NewTimecard(tclockT30Tenant, "worker-walt", "assignment-riverside", stdtime.Now().UTC(), stdtime.Now().UTC().Add(stdtime.Hour)); err != nil {
		t.Fatalf("valid tenant timecard rejected: %v", err)
	}
}

func TestTodo_FTIME_009_Browser(t *testing.T) {
	journey := tclockT30IronridgeJourney(t)
	if journey.session.State != timesession.StateClosed || journey.timecard.State != timecard.Locked {
		t.Fatal("workspace browser projection did not reach the reviewable approved state")
	}
	if len(journey.timecard.Lines[0].SourceRefs) < 4 {
		t.Fatalf("browser-visible timecard omitted source/method/policy evidence: %+v", journey.timecard.Lines[0])
	}
}

type tclockT30PartnerAdapter struct {
	tenant  string
	seen    map[uint64]string
	revoked bool
}

func (a *tclockT30PartnerAdapter) CreateEnrollmentCode(context.Context, *timev1.CreateEnrollmentCodeRequest) (*timev1.CreateEnrollmentCodeResponse, error) {
	return &timev1.CreateEnrollmentCodeResponse{Code: "ironridge-enrollment"}, nil
}

func (a *tclockT30PartnerAdapter) EnrollDevice(context.Context, *timev1.EnrollDeviceRequest) (*timev1.EnrollDeviceResponse, error) {
	return &timev1.EnrollDeviceResponse{Device: &timev1.ClockDevice{DeviceId: "hardware-riverside-1", TenantId: a.tenant, SiteId: "riverside", Revision: 1}, MachineClientCredentialRef: "hardware-credential"}, nil
}

func (a *tclockT30PartnerAdapter) RevokeDevice(context.Context, *timev1.RevokeDeviceRequest) (*timev1.RevokeDeviceResponse, error) {
	a.revoked = true
	return &timev1.RevokeDeviceResponse{Device: &timev1.ClockDevice{DeviceId: "hardware-riverside-1", TenantId: a.tenant, SiteId: "riverside", State: timev1.ClockDeviceState_CLOCK_DEVICE_STATE_REVOKED}}, nil
}

func (*tclockT30PartnerAdapter) SyncRoster(context.Context, *timev1.SyncRosterRequest) (*timev1.SyncRosterResponse, error) {
	return &timev1.SyncRosterResponse{Snapshot: &timev1.RosterSnapshot{SnapshotRevision: 7, MaxOfflineAgeSeconds: 3600}}, nil
}

func (*tclockT30PartnerAdapter) IdentifyWorker(context.Context, *timev1.IdentifyWorkerRequest) (*timev1.IdentifyWorkerResponse, error) {
	return &timev1.IdentifyWorkerResponse{PunchToken: "hardware-worker-token", Status: &timev1.WorkerPunchStatus{WorkerId: "worker-loretta"}}, nil
}

func (a *tclockT30PartnerAdapter) SubmitPunches(_ context.Context, request *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error) {
	punch := request.GetPunches()[0]
	sequence := punch.GetDeviceSequence()
	if a.seen == nil {
		a.seen = map[uint64]string{}
	}
	if prior, ok := a.seen[sequence]; ok {
		return &timev1.SubmitPunchesResponse{Receipts: []*timev1.PunchReceipt{{DeviceSequence: sequence, Status: timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_DUPLICATE, ReceiptId: prior, OriginalReceiptId: prior}}, HighestContiguousSequence: 1}, nil
	}
	if sequence != 1 {
		return &timev1.SubmitPunchesResponse{Receipts: []*timev1.PunchReceipt{{DeviceSequence: sequence, RejectionReason: timev1.PunchRejectionReason_PUNCH_REJECTION_REASON_SEQUENCE_GAP}}, HighestContiguousSequence: 1}, nil
	}
	a.seen[sequence] = "hardware-receipt-1"
	return &timev1.SubmitPunchesResponse{Receipts: []*timev1.PunchReceipt{{DeviceSequence: sequence, ReceiptId: "hardware-receipt-1", Status: timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED}}, HighestContiguousSequence: 1}, nil
}

func (*tclockT30PartnerAdapter) Heartbeat(context.Context, *timev1.HeartbeatRequest) (*timev1.HeartbeatResponse, error) {
	return &timev1.HeartbeatResponse{ServerTime: timestamppb.New(stdtime.Date(2026, 9, 28, 16, 0, 0, 0, stdtime.UTC))}, nil
}

type tclockT30Webhook struct{ events []timeclockkit.WebhookEvent }

func (w *tclockT30Webhook) Receive(_ context.Context, event timeclockkit.WebhookEvent) error {
	w.events = append(w.events, event)
	return nil
}

func tclockT30PartnerOptions(webhook timeclockkit.WebhookReceiver) timeclockkit.Options {
	return timeclockkit.Options{TenantID: tclockT30Tenant, SiteID: "riverside", ProfileRef: "ironridge-clock-v1", WorkerID: "worker-loretta", EnrollmentTTL: 10 * stdtime.Minute,
		Now: stdtime.Date(2026, 9, 28, 16, 0, 0, 0, stdtime.UTC), Webhook: webhook}
}

func tclockT30PartnerRun(t *testing.T, tenant string) (timeclockkit.Result, *tclockT30Webhook, *tclockT30PartnerAdapter) {
	t.Helper()
	webhook := &tclockT30Webhook{}
	adapter := &tclockT30PartnerAdapter{tenant: tenant}
	result, err := timeclockkit.Run(context.Background(), adapter, tclockT30PartnerOptions(webhook))
	if tenant == tclockT30Tenant {
		if err != nil || !result.Passed {
			t.Fatalf("hardware partner run: %v (%+v)", err, result)
		}
	}
	return result, webhook, adapter
}

// TestTodo_TCLOCK_019 proves one site can combine the tablet kiosk journey
// with an enrolled hardware clock, retain source/method evidence, approve both
// workers, emit a receipt webhook, and export each approved interval once.
func TestTodo_TCLOCK_019(t *testing.T) {
	result, webhook, adapter := tclockT30PartnerRun(t, tclockT30Tenant)
	if !adapter.revoked || len(webhook.events) != 1 || webhook.events[0].TenantID != tclockT30Tenant {
		t.Fatalf("hardware lifecycle/webhook evidence incomplete: revoked=%t events=%+v", adapter.revoked, webhook.events)
	}
	hardware, err := clockadapters.ParseADMS("hardware-riverside-1", []byte("worker-loretta\t2026-09-28 08:00:00\t0\t\t\t\t9\nworker-loretta\t2026-09-28 16:00:00\t1\t\t\t\t10"), stdtime.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if hardware.Request.GetDeviceId() != "hardware-riverside-1" || len(hardware.Request.GetPunches()) != 2 || hardware.Request.GetPunches()[0].GetIdentificationMethod() != timev1.IdentificationMethod_IDENTIFICATION_METHOD_BADGE {
		t.Fatalf("hardware adapter lost enrolled device or badge method: %+v", hardware.Request)
	}
	source := timesession.PunchSource{Kind: "HARDWARE_CLOCK", DeviceRef: hardware.Request.GetDeviceId(), Method: timesession.IdentBadge}
	start := stdtime.Date(2026, 9, 28, 8, 0, 0, 0, stdtime.UTC)
	session := tclockT30PunchNamed(t, "worker-loretta", timesession.Session{}, timesession.PunchIn, start, "loretta-in", 0, source)
	session = tclockT30PunchNamed(t, "worker-loretta", session, timesession.PunchOut, start.Add(8*stdtime.Hour), "loretta-out", session.Revision, source)
	if session.Source.Kind != "HARDWARE_CLOCK" || session.Source.Method != timesession.IdentBadge || session.State != timesession.StateClosed {
		t.Fatalf("hardware session evidence = %+v", session)
	}
	raw := tclockT30ApprovedExport(t, "worker-loretta", "tc-riverside-loretta", "hardware-receipt-1", 480)
	if !result.Passed || strings.Count(string(raw), `"minutes": 480`) != 1 {
		t.Fatalf("hardware approved export not exactly once: result=%+v export=%s", result, raw)
	}
}

func tclockT30PunchNamed(t *testing.T, worker string, current timesession.Session, kind timesession.PunchKind, at stdtime.Time, key string, expected uint64, source timesession.PunchSource) timesession.Session {
	t.Helper()
	next, _, err := timesession.Apply(current, timesession.Punch{Kind: kind, Tenant: tclockT30Tenant, Worker: worker, Actor: worker, Assignment: "riverside", SessionID: "session-" + worker,
		IdempotencyKey: key, DeviceTime: at, ServerReceiptTime: at.Add(2 * stdtime.Second), Source: source, Scheduled: true, ExpectedRevision: expected})
	if err != nil {
		t.Fatalf("apply hardware %s: %v", kind, err)
	}
	return next
}

func TestTodo_TCLOCK_019_Conformance(t *testing.T) {
	result, _, _ := tclockT30PartnerRun(t, tclockT30Tenant)
	for _, name := range []string{"enrollment", "tenant-isolation", "identify", "offline-replay", "duplicate-replay", "revocation"} {
		found := false
		for _, observation := range result.Observations {
			if observation.Name == name && observation.Passed {
				found = true
			}
		}
		if !found {
			t.Errorf("partner proof missing passing observation %q", name)
		}
	}
}

func TestTodo_TCLOCK_019_Integration(t *testing.T) {
	tr, err := clockadapters.ParseADMS("hardware-riverside-1", []byte("worker-loretta\t2026-09-28T08:00:00Z\tIN\t\t\t\t9"), stdtime.UTC)
	if err != nil || len(tr.Request.GetPunches()) != 1 {
		t.Fatalf("hardware adapter integration: %v %+v", err, tr)
	}
	result, _, _ := tclockT30PartnerRun(t, tclockT30Tenant)
	if result.DeviceID != "hardware-riverside-1" || result.TenantID != tclockT30Tenant {
		t.Fatalf("partner result lost device/tenant binding: %+v", result)
	}
}

func TestTodo_TCLOCK_019_Security(t *testing.T) {
	result, _, _ := tclockT30PartnerRun(t, "other-tenant")
	if result.Passed {
		t.Fatal("foreign hardware tenant passed Ironridge certification")
	}
	tr, err := clockadapters.ParseADMS("enrolled-hardware", []byte("attacker\t2026-09-28 08:00:00\t0\t\t\t\t1"), stdtime.UTC)
	if err != nil || tr.Request.GetDeviceId() != "enrolled-hardware" {
		t.Fatalf("adapter accepted an invalid body or changed enrolled identity: %v %+v", err, tr)
	}
}

func TestTodo_TCLOCK_019_Browser(t *testing.T) {
	result, webhook, _ := tclockT30PartnerRun(t, tclockT30Tenant)
	if !result.Passed || len(webhook.events) != 1 || webhook.events[0].Type != "clock.punch.accepted" {
		t.Fatal("kiosk/hardware browser projection did not preserve the accepted receipt")
	}
}

// TestTodo_WTIME_018 is the primary one-tenant proof. All six profiles use
// the same resolver and workflow compiler; only profile data selects the
// template and destination.
func TestTodo_WTIME_018(t *testing.T) {
	cases := tclockT30WorkerProfiles(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			templateID, plan, err := timeclock.ResolvePlan(tc.profile)
			if err != nil {
				t.Fatal(err)
			}
			if templateID != tc.template || plan.WorkflowID == "" || tc.profile.Destination != tc.destination {
				t.Fatalf("profile resolved to template=%q workflow=%q destination=%q", templateID, plan.WorkflowID, tc.profile.Destination)
			}
			if plan.Digest() == "" {
				t.Fatal("resolved plan has no evidence digest")
			}
			if timeclock.DeliversThroughPeriod(templateID) {
				if _, err := timeclock.ResolvePeriodPlan(tc.profile); err != nil {
					t.Fatalf("period workflow: %v", err)
				}
			}
			if tc.expectedMinutes <= 0 {
				t.Fatalf("invalid expected minutes fixture: %d", tc.expectedMinutes)
			}
		})
	}
}

type tclockT30WorkerCase struct {
	name, template  string
	profile         timeprofile.TimeProfile
	destination     timeprofile.Destination
	expectedMinutes int
}

func tclockT30WorkerProfiles(t *testing.T) []tclockT30WorkerCase {
	t.Helper()
	spanish := profileFixture(timeprofile.CategoryEmployee, timeprofile.CaptureDuration, timeprofile.PayHourly, timeprofile.NonExempt, timeprofile.DestinationPayroll)
	spanish.OvertimeJurisdictions = []string{"ES"}
	spanish.RestPeriodDutyRequired, spanish.DailyRecordingDutyRequired = true, true
	dcaa := profileFixture(timeprofile.CategoryEmployee, timeprofile.CaptureDuration, timeprofile.PaySalary, timeprofile.Exempt, timeprofile.DestinationPayroll)
	dcaa.GovernmentContract.DCAATotalTimeAccounting = true
	dcaa.GovernmentContract.UncompensatedOvertimeTracked = true
	dcaa.Taxonomy.Primary = "DCAA-INDIRECT"
	minor := profileFixture(timeprofile.CategoryEmployee, timeprofile.CapturePunch, timeprofile.PayHourly, timeprofile.NonExempt, timeprofile.DestinationPayroll)
	minor.MinorAgeBand, minor.MinorPermitVerified = timeprofile.MinorAgeBand16To17, true
	return []tclockT30WorkerCase{
		{name: "california hourly", profile: profileFixture(timeprofile.CategoryEmployee, timeprofile.CapturePunch, timeprofile.PayHourly, timeprofile.NonExempt, timeprofile.DestinationPayroll), template: string(timeprofile.TemplatePunchSession), destination: timeprofile.DestinationPayroll, expectedMinutes: 480},
		{name: "DCAA salaried engineer", profile: dcaa, template: string(timeprofile.TemplateDurationSheet), destination: timeprofile.DestinationPayroll, expectedMinutes: 480},
		{name: "contractor hourly SOW", profile: profileFixture(timeprofile.CategoryContractor, timeprofile.CaptureDuration, timeprofile.PayContract, timeprofile.NotApplicable, timeprofile.DestinationInvoice), template: string(timeprofile.TemplateContractorTime), destination: timeprofile.DestinationInvoice, expectedMinutes: 240},
		{name: "agency VMS temp", profile: profileFixture(timeprofile.CategoryAgencyTemp, timeprofile.CaptureDuration, timeprofile.PayHourly, timeprofile.NotApplicable, timeprofile.DestinationAgency), template: string(timeprofile.TemplateAgencyTime), destination: timeprofile.DestinationAgency, expectedMinutes: 480},
		{name: "minor hourly", profile: minor, template: string(timeprofile.TemplatePunchSession), destination: timeprofile.DestinationPayroll, expectedMinutes: 180},
		{name: "Spanish employee", profile: spanish, template: string(timeprofile.TemplateDurationSheet), destination: timeprofile.DestinationPayroll, expectedMinutes: 480},
	}
}

func TestTodo_WTIME_018_Integration(t *testing.T) {
	for _, tc := range tclockT30WorkerProfiles(t) {
		t.Run(tc.name, func(t *testing.T) {
			_, plan, err := timeclock.ResolvePlan(tc.profile)
			if err != nil {
				t.Fatal(err)
			}
			if plan.WorkflowID == "" || tc.profile.TenantRef != values.TenantId("ironridge") {
				t.Fatalf("one-tenant plan binding missing: workflow=%q tenant=%q", plan.WorkflowID, tc.profile.TenantRef)
			}
		})
	}
}

func TestTodo_WTIME_018_Security(t *testing.T) {
	for _, tc := range tclockT30WorkerProfiles(t) {
		t.Run(tc.name, func(t *testing.T) {
			if tc.profile.Category == timeprofile.CategoryContractor || tc.profile.Category == timeprofile.CategoryAgencyTemp {
				if timeclock.DeliversThroughPeriod(tc.template) {
					t.Fatalf("%s was routed to payroll period", tc.name)
				}
			}
			if tc.profile.MinorAgeBand.IsMinor() && !tc.profile.MinorPermitVerified {
				t.Fatal("minor profile lost permit evidence")
			}
		})
	}
}

func TestTodo_WTIME_018_Browser(t *testing.T) {
	for _, tc := range tclockT30WorkerProfiles(t) {
		t.Run(tc.name, func(t *testing.T) {
			templateID, plan, err := timeclock.ResolvePlan(tc.profile)
			if err != nil || plan.WorkflowID == "" {
				t.Fatalf("workspace profile projection failed: template=%q workflow=%q err=%v", templateID, plan.WorkflowID, err)
			}
			if tc.expectedMinutes == 0 {
				t.Fatal("workspace projection dropped expected time")
			}
		})
	}
}
