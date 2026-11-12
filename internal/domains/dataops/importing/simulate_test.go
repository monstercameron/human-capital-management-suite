package importing_test

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops/importing"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type simulationSigner struct{}

func (simulationSigner) Authority() string { return "authority.test/import-simulation/v1" }
func (simulationSigner) Sign(payload []byte) (string, error) {
	sum := sha256.Sum256(append([]byte("test-key:"), payload...))
	return "test:" + hex.EncodeToString(sum[:]), nil
}
func (s simulationSigner) Verify(payload []byte, signature string) error {
	want, _ := s.Sign(payload)
	if subtle.ConstantTimeCompare([]byte(want), []byte(signature)) != 1 {
		return errors.New("invalid signature")
	}
	return nil
}

func simulationFixture(t *testing.T) (importing.SimulationInput, importing.Signer) {
	t.Helper()
	reg := testRegistry(t)
	mapping, err := importing.Compile(reg, validationMappingSpec())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	b, err := importing.StageBatch(csvSource(t, "s3://simulation"), fixedInstant(t),
		[]string{"id_raw", "amount_raw", "band_raw"}, [][]string{{"alpha", "100", "east"}, {"beta", "200", "east"}})
	if err != nil {
		t.Fatalf("StageBatch: %v", err)
	}
	validation, err := importing.ValidateBatch(b, mapping, reg)
	if err != nil {
		t.Fatalf("ValidateBatch: %v", err)
	}
	signer := simulationSigner{}
	subjects := make(map[string]importing.SimulationSubject)
	resources := make(map[string]map[string]values.ResourceKey)
	revisions := make(map[string]map[string]values.RevisionToken)
	for _, row := range b.Rows() {
		subjects[row.ID()] = importing.SimulationSubject{Kind: "EMPLOYMENT", SubjectID: "employment:" + row.ID(), AuthorityDomain: "PEOPLE"}
		resources[row.ID()] = make(map[string]values.ResourceKey)
		revisions[row.ID()] = make(map[string]values.RevisionToken)
		for _, property := range []string{"job.title", "compensation_component.amount", "position.pay_band_ref"} {
			key, keyErr := values.NewResourceKey(b.Source().Tenant, values.Kind("employment"), row.ID(), property)
			if keyErr != nil {
				t.Fatal(keyErr)
			}
			revision, revisionErr := values.NewSequenceRevision("employment:"+row.ID()+":"+property, 1)
			if revisionErr != nil {
				t.Fatal(revisionErr)
			}
			resources[row.ID()][property] = key
			revisions[row.ID()][property] = revision
		}
	}
	effective, err := values.NewOpenInstantInterval(fixedInstant(t))
	if err != nil {
		t.Fatal(err)
	}
	return importing.SimulationInput{Batch: b, Mapping: mapping, Validation: validation, Registry: reg,
		IntentType: "hcmnext.people.promote_worker/v1", Subjects: subjects, Resources: resources, CurrentRevisions: revisions,
		OrganizationScopeID: "org:acme", LegalEntityID: "legal:acme", EffectiveTime: effective, Signer: signer}, signer
}

func resetSimulationPins(t *testing.T, in *importing.SimulationInput) {
	t.Helper()
	in.Subjects = make(map[string]importing.SimulationSubject)
	in.Resources = make(map[string]map[string]values.ResourceKey)
	in.CurrentRevisions = make(map[string]map[string]values.RevisionToken)
	for _, row := range in.Batch.Rows() {
		in.Subjects[row.ID()] = importing.SimulationSubject{Kind: "EMPLOYMENT", SubjectID: "employment:" + row.ID(), AuthorityDomain: "PEOPLE"}
		in.Resources[row.ID()] = make(map[string]values.ResourceKey)
		in.CurrentRevisions[row.ID()] = make(map[string]values.RevisionToken)
		for _, field := range in.Mapping.Fields {
			property := string(field.Target)
			key, err := values.NewResourceKey(in.Batch.Source().Tenant, values.Kind("employment"), row.ID(), property)
			if err != nil {
				t.Fatal(err)
			}
			revision, err := values.NewSequenceRevision("employment:"+row.ID()+":"+property, 1)
			if err != nil {
				t.Fatal(err)
			}
			in.Resources[row.ID()][property] = key
			in.CurrentRevisions[row.ID()][property] = revision
		}
	}
}

func TestTodo_DATAOPS_005(t *testing.T) {
	in, _ := simulationFixture(t)
	out, err := importing.SimulateImport(in)
	if err != nil {
		t.Fatalf("SimulateImport: %v", err)
	}
	if out.ZeroEffects != true || out.CommittedEffects != 0 {
		t.Fatalf("effects = %v/%d", out.ZeroEffects, out.CommittedEffects)
	}
	if out.Creates != 2 || out.Errors != 0 || len(out.Drafts) != 2 || out.Signature == out.Digest {
		t.Fatalf("unexpected simulation: %+v", out)
	}
	rowID := in.Batch.Rows()[0].ID()
	in.Current = map[string]map[string]string{rowID: {}}
	out, err = importing.SimulateImport(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Changes != 1 || out.Creates != 1 || out.Drafts[0].Writes[0].CurrentPresent {
		t.Fatalf("missing baseline property was not an explicit change: %+v", out)
	}
	rows := in.Batch.Rows()
	in.Current = make(map[string]map[string]string, len(rows))
	for _, row := range rows {
		mapped, applyErr := in.Mapping.Apply(in.Batch.Header(), row)
		if applyErr != nil {
			t.Fatal(applyErr)
		}
		in.Current[row.ID()] = map[string]string{}
		for _, value := range mapped {
			if value.OK {
				in.Current[row.ID()][string(value.Target)] = value.Value
			}
		}
	}
	in.Conflicts = []importing.SimulationConflict{{RowID: rows[0].ID(), Property: "job.title", ExistingIntent: "intent-1", EvidenceRef: "evidence-1"}}
	classified, err := importing.SimulateImport(in)
	if err != nil {
		t.Fatal(err)
	}
	if classified.Conflicts != 1 || classified.NoOps != 1 || classified.AuthorityRequired != 0 || len(classified.PlannedWrites) != 0 {
		t.Fatalf("conflict/no-op classification = %+v", classified)
	}
}

func TestTodo_DATAOPS_005_Golden(t *testing.T) {
	in, _ := simulationFixture(t)
	a, err := importing.SimulateImport(in)
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "sha256:732b4f7e9ff830de0a920539eb8bd4aea677e322dc8e0dbc0b002177bad6f779"
	const wantSignature = "test:f40bef598c25fde6cebcf802d3128e6c16fbaac44e5613bd9576d720cb1ff1c7"
	if a.Digest != wantDigest || a.Signature != wantSignature {
		t.Fatalf("golden bytes changed: digest=%q signature=%q", a.Digest, a.Signature)
	}
}

func FuzzTodo_DATAOPS_005(f *testing.F) {
	f.Add("alpha", "100")
	f.Fuzz(func(t *testing.T, id, amount string) {
		in, _ := simulationFixture(t)
		batch, err := importing.StageBatch(in.Batch.Source(), in.Batch.RetrievedAt(),
			[]string{"id_raw", "amount_raw", "band_raw"}, [][]string{{id, amount, "east"}})
		if err != nil {
			return
		}
		in.Batch = batch
		resetSimulationPins(t, &in)
		validation, err := importing.ValidateBatch(batch, in.Mapping, in.Registry)
		if err != nil {
			return
		}
		in.Validation = validation
		out, err := importing.SimulateImport(in)
		if err == nil && (!out.ZeroEffects || out.CommittedEffects != 0) {
			t.Fatal("simulation created an effect")
		}
	})
}

func TestTodo_DATAOPS_005_Security(t *testing.T) {
	in, signer := simulationFixture(t)
	in.Signer = nil
	if _, err := importing.SimulateImport(in); !errors.Is(err, importing.ErrSimulationSigningRequired) {
		t.Fatalf("err = %v", err)
	}
	in, _ = simulationFixture(t)
	out, err := importing.SimulateImport(in)
	if err != nil {
		t.Fatal(err)
	}
	out.SigningAuthority = "caller-controlled"
	if err := importing.VerifyImportSimulation(out, signer); !errors.Is(err, importing.ErrSimulationSignature) {
		t.Fatalf("forged authority verification error = %v", err)
	}
}

func TestTodo_DATAOPS_005_Recovery(t *testing.T) {
	recovery, signer := simulationFixture(t)
	first, err := importing.SimulateImport(recovery)
	if err != nil {
		t.Fatal(err)
	}
	if err := importing.VerifyImportSimulation(first, signer); err != nil {
		t.Fatalf("authentic recovered simulation did not verify: %v", err)
	}
	tampered := first
	tampered.Drafts[0].Status = "NO_OP"
	if err := importing.VerifyImportSimulation(tampered, signer); !errors.Is(err, importing.ErrSimulationSignature) {
		t.Fatalf("tampered recovered simulation verification error = %v", err)
	}
}

func TestTodo_DATAOPS_005_Mutation(t *testing.T) {
	in, _ := simulationFixture(t)
	first, err := importing.SimulateImport(in)
	if err != nil {
		t.Fatal(err)
	}
	in.EstimatedCost = 1
	second, err := importing.SimulateImport(in)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == second.Digest || first.Signature == second.Signature {
		t.Fatal("mutated governance input did not change signed output")
	}
	in, _ = simulationFixture(t)
	in.Obligations = []string{"approval:bulk"}
	out, err := importing.SimulateImport(in)
	if err != nil {
		t.Fatal(err)
	}
	in.Obligations[0] = "mutated"
	if out.Obligations[0] != "approval:bulk" {
		t.Fatal("simulation aliases caller-owned obligations")
	}
	in, _ = simulationFixture(t)
	in.Mapping.Fields[0].Transform = importing.TransformSpec{Kind: importing.TransformConstant, Constant: "forged"}
	if _, err := importing.SimulateImport(in); !errors.Is(err, importing.ErrSimulationInput) {
		t.Fatalf("forged mapping error = %v", err)
	}
	in, _ = simulationFixture(t)
	in.Validation.Rows = in.Validation.Rows[:1]
	if _, err := importing.SimulateImport(in); !errors.Is(err, importing.ErrSimulationInput) {
		t.Fatalf("forged validation error = %v", err)
	}
}

func TestTodo_IMPL_GAP_004(t *testing.T) {
	in, signer := simulationFixture(t)
	rows := in.Batch.Rows()
	conflicts := []importing.SimulationConflict{
		{RowID: rows[0].ID(), Property: "compensation_component.amount", ExistingIntent: "intent-1", EvidenceRef: "evidence-z"},
		{RowID: rows[0].ID(), Property: "job.title", ExistingIntent: "intent-2", EvidenceRef: "evidence-a"},
	}
	in.Conflicts = conflicts
	out, err := importing.SimulateImport(in)
	if err != nil {
		t.Fatalf("SimulateImport: %v", err)
	}
	if err := importing.VerifyImportSimulation(out, signer); err != nil {
		t.Fatalf("freshly generated multi-conflict simulation did not verify: %v", err)
	}
	reversed := []importing.SimulationConflict{conflicts[1], conflicts[0]}
	in.Conflicts = reversed
	permuted, err := importing.SimulateImport(in)
	if err != nil {
		t.Fatalf("SimulateImport(permuted): %v", err)
	}
	if permuted.Digest != out.Digest || permuted.Signature != out.Signature {
		t.Fatal("permuted conflict input did not yield deterministic signed bytes")
	}
	if err := importing.VerifyImportSimulation(permuted, signer); err != nil {
		t.Fatalf("permuted simulation did not verify: %v", err)
	}
	tampered := cloneForTamper(out)
	tampered.Drafts[0].ConflictRefs[0] = "evidence-x"
	tampered.Drafts[0].Conflicts[0].EvidenceRef = "evidence-x"
	if err := importing.VerifyImportSimulation(tampered, signer); !errors.Is(err, importing.ErrSimulationSignature) {
		t.Fatalf("tampered conflict evidence verification error = %v", err)
	}
}

func cloneForTamper(out importing.ImportSimulation) importing.ImportSimulation {
	drafts := make([]importing.BusinessIntentDraft, len(out.Drafts))
	copy(drafts, out.Drafts)
	for i := range drafts {
		drafts[i].ConflictRefs = append([]string(nil), out.Drafts[i].ConflictRefs...)
		drafts[i].Conflicts = append([]importing.SimulationConflict(nil), out.Drafts[i].Conflicts...)
	}
	out.Drafts = drafts
	return out
}
