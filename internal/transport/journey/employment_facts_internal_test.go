package journey

import (
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// fullyPopulatedWorker sets every field on the port type, so that a field the
// conversion forgets shows up as a difference rather than as a zero value
// that happened to match. The file header of convert.go names this exact
// defect: a conversion that drops a field still answers the RPC, the page
// just shows less than the engine knows.
func fullyPopulatedWorker() workspace.WorkerSummary {
	worker := workspace.WorkerSummary{}
	value := reflect.ValueOf(&worker).Elem()
	for i := range value.NumField() {
		field := value.Field(i)
		if field.Kind() == reflect.String {
			field.SetString(value.Type().Field(i).Name + "-value")
		}
	}
	// The two non-string fields, and the one string field whose value has to
	// be a token the conversion recognises rather than free text.
	worker.CreatedAt = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	worker.ManagerDisposition = workspace.ManagerRelationshipVisible
	return worker
}

// Every string field on the port type survives the round trip onto the wire
// and back, including the six employment facts and the pay basis. The check
// is reflective so that a field added later is covered without anybody
// remembering to extend a list.
func TestWorkerConversionCarriesEveryEmploymentFact(t *testing.T) {
	worker := fullyPopulatedWorker()
	got := fromWorker(toWorker(worker))

	value := reflect.ValueOf(worker)
	roundTripped := reflect.ValueOf(got)
	for i := range value.NumField() {
		name := value.Type().Field(i).Name
		if value.Field(i).Kind() != reflect.String {
			continue
		}
		if want, have := value.Field(i).String(), roundTripped.Field(i).String(); want != have {
			t.Errorf("%s was dropped by the conversion: %q became %q", name, want, have)
		}
	}
	if !got.CreatedAt.Equal(worker.CreatedAt) {
		t.Errorf("CreatedAt = %s, want %s", got.CreatedAt, worker.CreatedAt)
	}

	// And named explicitly, because a reflective check that silently stopped
	// covering these would still pass: these are the fields this lane added.
	msg := toWorker(worker)
	for _, tc := range []struct{ name, got string }{
		{"employment_type", msg.GetEmploymentType()},
		{"time_type", msg.GetTimeType()},
		{"company", msg.GetCompany()},
		{"business_unit", msg.GetBusinessUnit()},
		{"cost_center", msg.GetCostCenter()},
		{"work_arrangement", msg.GetWorkArrangement()},
		{"pay_basis", msg.GetPayBasis()},
	} {
		if tc.got == "" {
			t.Errorf("%s is empty on the wire although the port set it", tc.name)
		}
	}
}

// The pay basis is withheld with the amount. A reader told the basis beside a
// redacted figure has been handed the unit the redaction was meant to keep:
// "REDACTED, annually" narrows the salary far more than "REDACTED" does.
func TestPayBasisIsWithheldWithTheAmount(t *testing.T) {
	for _, tc := range []struct {
		name   string
		effect authz.Effect
	}{
		{"redacted", authz.EffectRedacted},
		// The zero Effect is the fail-closed default: no ruling was made.
		{"omitted", authz.Effect(0)},
		{"denied", authz.EffectDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := toWorker(fullyPopulatedWorker())
			applyDirectoryDisclosure(msg, authz.DirectoryDisclosure{
				Pay:            authz.FieldRuling{Effect: tc.effect},
				LegalName:      authz.FieldRuling{Effect: authz.EffectAllow},
				ManagerLinkage: authz.FieldRuling{Effect: authz.EffectAllow},
			})
			if msg.GetPayBasis() != "" {
				t.Errorf("pay basis = %q, want it withheld with the amount", msg.GetPayBasis())
			}
			// The employment facts are placement facts, not compensation, and
			// travel with org_unit and location rather than with pay.
			if msg.GetCostCenter() == "" || msg.GetEmploymentType() == "" {
				t.Error("an employment fact was withheld with the compensation ruling")
			}
		})
	}

	// An allowed disclosure keeps the basis, so the rule above is withholding
	// rather than always clearing the field.
	allowed := toWorker(fullyPopulatedWorker())
	applyDirectoryDisclosure(allowed, authz.DirectoryDisclosure{
		Pay:            authz.FieldRuling{Effect: authz.EffectAllow},
		LegalName:      authz.FieldRuling{Effect: authz.EffectAllow},
		ManagerLinkage: authz.FieldRuling{Effect: authz.EffectAllow},
	})
	if allowed.GetPayBasis() == "" {
		t.Error("an allowed compensation disclosure dropped the pay basis")
	}

	// The fail-closed rendering clears it too.
	masked := toWorker(fullyPopulatedWorker())
	maskWorker(masked)
	if masked.GetPayBasis() != "" {
		t.Errorf("masked pay basis = %q, want empty", masked.GetPayBasis())
	}
}
