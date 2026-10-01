package timecardexport

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/timecardservice"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timecard"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func tclockT11Model() ApprovedTimecard {
	return ApprovedTimecard{
		TenantID: "tenant-a", TimecardID: "tc-1", WorkerRef: "worker-1",
		PeriodStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC),
		State:       timecard.Approved, Revision: 7, ApprovedRevision: 7, PreviousApprovedRevision: 6,
		Intervals: []ApprovedInterval{
			{Date: "2026-09-01", Minutes: 240, PayCode: "REG", JobAllocation: JobAllocation{Project: "proj-a", CostCode: "cc-1", RateCode: "std"}, SourceRevision: "line-1"},
			{Date: "2026-09-01", Minutes: 240, PayCode: "REG", JobAllocation: JobAllocation{Project: "proj-b", CostCode: "cc-2", RateCode: "std"}, SourceRevision: "line-2"},
		},
		Allowances: []Allowance{{Code: "MEAL", Amount: "12.50", Currency: "USD"}},
	}
}

func TestTodo_TCLOCK_015(t *testing.T) {
	raw, err := Export(tclockT11Model())
	if err != nil {
		t.Fatal(err)
	}
	card, report, err := Import(raw)
	if err != nil {
		t.Fatal(err)
	}
	if report.HasUnmapped() || card.WorkerRef != "worker-1" || card.Revision != "7" || card.PreviousRevision != "6" {
		t.Fatalf("round trip = %#v, report = %#v", card, report)
	}
	if len(card.Intervals) != 2 || card.Intervals[1].Project != "proj-b" || len(card.Allowances) != 1 || card.Allowances[0].Code != "MEAL" {
		t.Fatalf("round trip dropped allocation or allowance: %#v", card)
	}
}

func TestTodo_TCLOCK_015_Golden(t *testing.T) {
	got, err := Export(tclockT11Model())
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "workerId": "worker-1",
  "period": {
    "start": "2026-09-01",
    "end": "2026-09-15"
  },
  "revision": "7",
  "previousRevision": "6",
  "intervals": [
    {
      "date": "2026-09-01",
      "minutes": 240,
      "payCode": "REG",
      "jobAllocation": {
        "project": "proj-a",
        "costCode": "cc-1",
        "rateCode": "std"
      },
      "sourceRevision": "line-1"
    },
    {
      "date": "2026-09-01",
      "minutes": 240,
      "payCode": "REG",
      "jobAllocation": {
        "project": "proj-b",
        "costCode": "cc-2",
        "rateCode": "std"
      },
      "sourceRevision": "line-2"
    }
  ],
  "allowances": [
    {
      "code": "MEAL",
      "amount": "12.50",
      "currency": "USD"
    }
  ]
}`

	if string(got) != want {
		t.Fatalf("golden mismatch:\n got: %s\nwant: %s", got, want)
	}
}

type tclockT11Reader struct {
	model  ApprovedTimecard
	tenant string
	id     string
}

func (r *tclockT11Reader) ReadApprovedTimecard(_ context.Context, tenant, id string) (ApprovedTimecard, error) {
	r.tenant, r.id = tenant, id
	return r.model, nil
}

type tclockT11Authorizer struct {
	tenant string
	worker string
	cap    timecardservice.Capability
}

func (a *tclockT11Authorizer) Authorize(_ context.Context, _ *trust.Principal, tenant, worker string, cap timecardservice.Capability) error {
	a.tenant, a.worker, a.cap = tenant, worker, cap
	return nil
}

func tclockT11Principal(t *testing.T) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "payroll-operator", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-1", IssuedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), CredentialDigest: "digest",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTodo_TCLOCK_015_Integration(t *testing.T) {
	reader := &tclockT11Reader{model: tclockT11Model()}
	authorizer := &tclockT11Authorizer{}
	service := New(reader, authorizer)
	raw, err := service.Export(context.Background(), tclockT11Principal(t), ExportRequest{TimecardID: "tc-1"})
	if err != nil {
		t.Fatal(err)
	}
	if reader.tenant != "tenant-a" || reader.id != "tc-1" || authorizer.tenant != "tenant-a" || authorizer.worker != "worker-1" || authorizer.cap != timecardservice.CapRouteTime {
		t.Fatalf("scoping = reader(%q,%q), auth(%q,%q,%q)", reader.tenant, reader.id, authorizer.tenant, authorizer.worker, authorizer.cap)
	}
	if !strings.Contains(string(raw), `"revision": "7"`) {
		t.Fatalf("export did not pin approved revision: %s", raw)
	}
}

func TestTodo_TCLOCK_015_Conformance(t *testing.T) {
	raw, err := Export(tclockT11Model())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateConformance(raw); err != nil {
		t.Fatalf("valid output failed schema: %v", err)
	}
	if err := ValidateConformance([]byte(`{"workerId":"worker-1","period":{"start":"2026-09-01","end":"2026-09-15"},"revision":"7","intervals":[],"unknown":true}`)); err == nil {
		t.Fatal("unknown schema field was accepted")
	}
}

func TestTodo_TCLOCK_015_RejectsUnapprovedAndReportsUnmapped(t *testing.T) {
	model := tclockT11Model()
	model.State = timecard.Submitted
	if _, err := Export(model); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("unapproved export = %v, want ErrNotApproved", err)
	}
	raw := []byte(`{"workerId":"worker-1","period":{"start":"2026-09-01","end":"2026-09-15"},"revision":"7","intervals":[{"date":"2026-09-01","minutes":60,"payCode":"REG","jobAllocation":{"project":"p","costCode":"c","rateCode":"r"},"sourceRevision":"line-1","extraJobField":"kept-in-report"}],"allowances":[{"code":"MEAL","amount":"12.50","currency":"USD","extraAllowanceField":"kept-in-report"}],"extraTopLevel":true}`)
	_, report, err := Import(raw)
	if err != nil || !report.HasUnmapped() || len(report.TopLevel) != 1 || len(report.Intervals[0]) != 1 || len(report.Allowances[0]) != 1 {
		t.Fatalf("unmapped report = %#v, err = %v", report, err)
	}
}

func TestTodo_TCLOCK_015_ServiceRequiresPorts(t *testing.T) {
	if _, err := (Service{}).Export(context.Background(), tclockT11Principal(t), ExportRequest{TimecardID: "tc-1"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing ports = %v, want ErrUnavailable", err)
	}
}
