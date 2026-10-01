package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

// selfDirectory is an in-memory worker directory keyed by the reference a
// caller presents, resolving several spellings to one canonical worker.
type selfDirectory struct {
	canonical map[string]string
	inactive  map[string]bool
}

func (d selfDirectory) ResolveWorker(_ context.Context, _, ref string) (string, bool, error) {
	worker, ok := d.canonical[ref]
	return worker, ok && !d.inactive[worker], nil
}

func (selfDirectory) ResolveAssignment(context.Context, string, string, string) (string, string, bool, error) {
	return "", "", true, nil
}

func selfAuthorityPrincipal(t *testing.T, tenant, subject string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session",
		IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Minute), CredentialDigest: "digest",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestTodo_UXBLIND_123_SelfClockAuthorityAdmitsOnlyTheWorkersOwnClock proves
// the composed authority grants a punch to the principal's own worker and to
// nobody else, however the target is spelled, and grants no device or
// supervisor authority at all.
func TestTodo_UXBLIND_123_SelfClockAuthorityAdmitsOnlyTheWorkersOwnClock(t *testing.T) {
	directory := selfDirectory{
		canonical: map[string]string{"ir-014-ben": "ir-014-ben", "id-of-ben": "ir-014-ben", "ir-001-walt": "ir-001-walt", "gone": "gone"},
		inactive:  map[string]bool{"gone": true},
	}
	authority := SelfClockAuthority{Workers: directory}
	ben := selfAuthorityPrincipal(t, "ironridge-demo", "ir-014-ben")
	for _, tc := range []struct {
		name, tenant, target string
		principal            *trust.Principal
		allowed              bool
	}{
		{name: "own worker by key", tenant: "ironridge-demo", target: "ir-014-ben", principal: ben, allowed: true},
		{name: "own worker by another spelling", tenant: "ironridge-demo", target: "id-of-ben", principal: ben, allowed: true},
		{name: "another worker", tenant: "ironridge-demo", target: "ir-001-walt", principal: ben},
		{name: "unknown target", tenant: "ironridge-demo", target: "nobody", principal: ben},
		{name: "inactive target", tenant: "ironridge-demo", target: "gone", principal: selfAuthorityPrincipal(t, "ironridge-demo", "gone")},
		{name: "other tenant", tenant: "harborcare-demo", target: "ir-014-ben", principal: ben},
		{name: "principal with no worker", tenant: "ironridge-demo", target: "ir-014-ben", principal: selfAuthorityPrincipal(t, "ironridge-demo", "service-account")},
		{name: "no principal", tenant: "ironridge-demo", target: "ir-014-ben"},
	} {
		delegated, err := authority.AuthorizePunch(context.Background(), tc.principal, tc.tenant, tc.target, "assignment")
		if tc.allowed != (err == nil) || delegated {
			t.Fatalf("%s: delegated=%v err=%v, want allowed=%v and never delegated", tc.name, delegated, err, tc.allowed)
		}
	}
	if _, err := (SelfClockAuthority{}).AuthorizePunch(context.Background(), ben, "ironridge-demo", "ir-014-ben", "assignment"); err == nil {
		t.Fatal("an authority with no directory allowed a punch")
	}
	if err := authority.AuthorizeDeviceAdmin(context.Background(), ben, "ironridge-demo", "site"); err == nil {
		t.Fatal("the self clock granted device administration")
	}
	if err := authority.AuthorizeSupervisorOverride(context.Background(), ben, "ironridge-demo", "site"); err == nil {
		t.Fatal("the self clock granted a supervisor override")
	}
}

func TestTodo_UXBLIND_123_ClockIDsAreStableAndDistinct(t *testing.T) {
	ids := clockIDs{}
	first, again := ids.Deterministic("tenant", "observation", "WEB", "key"), ids.Deterministic("tenant", "observation", "WEB", "key")
	if first != again || len(first) != 36 {
		t.Fatalf("deterministic ids differ or are malformed: %q %q", first, again)
	}
	for _, other := range [][]string{{"tenant", "observation", "WEB", "key2"}, {"tenant2", "observation", "WEB", "key"}, {"tenantobservation", "WEB", "key"}} {
		if ids.Deterministic(other...) == first {
			t.Fatalf("parts %v collided with the original", other)
		}
	}
	if ids.Random() == ids.Random() {
		t.Fatal("random ids repeated")
	}
}

func TestTodo_UXBLIND_123_ClockSignalVerifierAdmitsOnlyTheClocksOwnSignals(t *testing.T) {
	verifier := clockAttestedSignalVerifier{}
	if err := verifier.Verify(stepSignal.Signal{Source: clockSignalSource}); err != nil {
		t.Fatalf("the clock's own signal was refused: %v", err)
	}
	for name, signal := range map[string]stepSignal.Signal{
		"foreign source": {Source: "hcmnext.integrations.hris"},
		"no source":      {},
		"signed":         {Source: clockSignalSource, Signature: []byte("webhook-bytes")},
	} {
		if err := verifier.Verify(signal); err == nil {
			t.Fatalf("%s: a signal that is not the clock's own was admitted", name)
		}
	}
}

func TestTodo_UXBLIND_123_ClockPlanStepsParkAndCompleteOnly(t *testing.T) {
	steps := clockPlanSteps{}
	parked, _, err := steps.Run(context.Background(), execute.StepRequest{Node: workflow.CompiledNode{ID: "await_clock_out", Type: workflow.StepSignal}})
	if err != nil || parked.Await != frontier.AwaitSignal || parked.AwaitRef != "clock_out" || parked.NodeID != "await_clock_out" {
		t.Fatalf("signal node = %+v err=%v", parked, err)
	}
	done, _, err := steps.Run(context.Background(), execute.StepRequest{Node: workflow.CompiledNode{ID: "session_closed", Type: workflow.StepEnd}})
	if err != nil || done.NodeID != "session_closed" || done.Await != frontier.AwaitNone {
		t.Fatalf("end node = %+v err=%v", done, err)
	}
	// A terminal carries an output digest, stable per node and instance: the
	// terminal fact refuses a close with none.
	if !strings.HasPrefix(done.OutputDigest, "sha256:") || len(done.OutputDigest) != len("sha256:")+64 {
		t.Fatalf("terminal output digest = %q", done.OutputDigest)
	}
	again, _, _ := steps.Run(context.Background(), execute.StepRequest{Node: workflow.CompiledNode{ID: "session_closed", Type: workflow.StepEnd}})
	other, _, _ := steps.Run(context.Background(), execute.StepRequest{InstanceID: uuid.New(), Node: workflow.CompiledNode{ID: "session_closed", Type: workflow.StepEnd}})
	if again.OutputDigest != done.OutputDigest || other.OutputDigest == done.OutputDigest {
		t.Fatal("the terminal digest is not stable per node and instance")
	}
	if _, _, err := steps.Run(context.Background(), execute.StepRequest{Node: workflow.CompiledNode{ID: "commit_punch", Type: workflow.StepCapability}}); err == nil {
		t.Fatal("a commit node reached the base runner instead of the request-scoped commit runner")
	}
	if _, err := (clockPlanResolver{}).ResolveWorkflow(context.Background(), runtime.StartRequest{}); err == nil {
		t.Fatal("a resolver with no plan selected a workflow")
	}
}

func TestTodo_UXBLIND_123_ScheduleLabelStatesOnlyWhatIsRecorded(t *testing.T) {
	for _, tc := range []struct {
		title, place, want string
	}{
		{"Journeyman Carpenter", "Aurora, CO jobsite", "Journeyman Carpenter · Aurora, CO jobsite"},
		{"Foreman", "", "Foreman"},
		{"", "Golden, CO jobsite", "Golden, CO jobsite"},
		{"  ", " ", "No shift published"},
	} {
		if got := scheduleLabel(workforce.WorkerRow{JobTitle: tc.title, Location: tc.place}); got != tc.want {
			t.Fatalf("scheduleLabel(%q,%q) = %q, want %q", tc.title, tc.place, got, tc.want)
		}
	}
}

func TestTodo_UXBLIND_123_LocalDevTimeDSNDerivesARoleAndKeepsNothingSecretOnDisk(t *testing.T) {
	dsn, password, err := localDevTimeDSN("postgres://postgres:postgres@127.0.0.1:5432/hcm_dev?sslmode=disable", "a-local-development-key-at-least-32-bytes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dsn, "postgres://"+localDevTimeRole+":"+password+"@127.0.0.1:5432/hcm_dev") || !strings.Contains(dsn, "sslmode=disable") || strings.Contains(dsn, "postgres:postgres") {
		t.Fatalf("dsn = %s", dsn)
	}
	again, samePassword, _ := localDevTimeDSN("postgres://postgres:postgres@127.0.0.1:5432/hcm_dev?sslmode=disable", "a-local-development-key-at-least-32-bytes")
	if again != dsn || samePassword != password || len(password) != 32 {
		t.Fatal("the derived credential is not stable")
	}
	if _, rotated, _ := localDevTimeDSN("postgres://postgres:postgres@127.0.0.1:5432/hcm_dev", "another-development-key-of-enough-length"); rotated == password {
		t.Fatal("a different key derived the same password")
	}
	for name, args := range map[string][2]string{
		"no key":          {"postgres://u:p@h/db", ""},
		"not a url":       {"host=127.0.0.1 dbname=x", "key-key-key-key-key-key-key-key-key"},
		"wrong scheme":    {"mysql://u:p@h/db", "key-key-key-key-key-key-key-key-key"},
		"no host":         {"postgres:///db", "key-key-key-key-key-key-key-key-key"},
		"unparseable url": {"postgres://u:p@h:notaport/db", "key-key-key-key-key-key-key-key-key"},
	} {
		if _, _, err := localDevTimeDSN(args[0], args[1]); err == nil {
			t.Fatalf("%s: a DSN was derived", name)
		}
	}
}
