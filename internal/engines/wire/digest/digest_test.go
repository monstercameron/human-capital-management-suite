package digest_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/canonical"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
)

var update = flag.Bool("update", false, "rewrite the checked-in golden digest vectors")

func goldenBytes(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden %s drifted\n got: %s\nwant: %s", path, got, want)
	}
}

func goldenReference(t *testing.T, name string, ref digest.Reference) {
	t.Helper()
	b, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		t.Fatalf("marshal reference: %v", err)
	}
	goldenBytes(t, name, append(b, '\n'))
}

func loadReference(t *testing.T, name string) digest.Reference {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var ref digest.Reference
	if err := json.Unmarshal(b, &ref); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
	return ref
}

// fixtureProposal is the source object every digest vector in this file is
// bound to. It deliberately populates control_snapshots, created_by and
// created_at so that the exclusions can be proved rather than assumed.
func fixtureProposal() *intentsv1.ProposalRevision {
	return &intentsv1.ProposalRevision{
		ProposalRevisionId: "pr-1000",
		IntentId:           "int-1000",
		Revision:           2,
		Proposal: &intentsv1.TypedPayload{
			Schema: &intentsv1.SchemaReference{
				SchemaId:         "hcmnext.people.v1.HireRequest",
				Version:          1,
				ProtobufFullName: "hcmnext.people.v1.HireRequest",
				DescriptorDigest: "sha256:d0",
			},
			ProtobufWireBytes: []byte{0x08, 0x2a},
			CanonicalDigest: &intentsv1.CanonicalDigestReference{
				ProfileId:          "hcmnext.people.hire",
				ProfileVersion:     1,
				SchemaId:           "hcmnext.people.v1",
				SchemaVersion:      1,
				AlgorithmId:        digest.AlgorithmSHA256,
				CanonicalLength:    64,
				Digest:             strings.Repeat("ab", 32),
				ScopeBindingDigest: strings.Repeat("cd", 32),
			},
		},
		ControlSnapshots: &intentsv1.ControlSnapshotReferences{
			PolicyBundleDigest:           "sha256:policy-v1",
			ClassificationTaxonomyDigest: "sha256:taxonomy-v1",
		},
		CreatedBy: &intentsv1.PrincipalReference{PrincipalId: "user-9"},
		CreatedAt: timestamppb.New(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
	}
}

func fixtureIntent() *intentsv1.IntentInstance {
	return &intentsv1.IntentInstance{
		IntentId:            "int-1000",
		TenantId:            "tenant-a",
		OrganizationScopeId: "org-1",
		Definition:          &intentsv1.DefinitionReference{IntentTypeId: "people.hire", Version: 1},
		Purpose:             "onboarding",
		Subjects: []*intentsv1.SubjectReference{
			{SubjectKind: "worker", SubjectId: "w-2", AuthorityDomain: "people"},
			{SubjectKind: "worker", SubjectId: "w-1", AuthorityDomain: "people"},
		},
		Request: &intentsv1.TypedPayload{
			Schema:            &intentsv1.SchemaReference{SchemaId: "hcmnext.people.v1.HireRequest", Version: 1, ProtobufFullName: "hcmnext.people.v1.HireRequest"},
			ProtobufWireBytes: []byte{0x08, 0x2a},
		},
		IdempotencyKey: "idem-abc",
		ExecutionMode:  intentsv1.ExecutionMode_EXECUTION_MODE_EXECUTE,
		CorrelationId:  "corr-1",
		TraceId:        "trace-1",
		CreatedAt:      timestamppb.New(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
	}
}

// proposalProfileV2 evolves the built-in PROPOSAL profile: it additionally
// binds which published profile produced the payload's own digest.
func proposalProfileV2() canonical.Profile {
	p := digest.ProposalProfileV1()
	p.Version = 2
	p.Material = append(slices.Clone(p.Material), "proposal.canonical_digest.material_profile_ref")
	return p
}

func mustRegistry(t *testing.T) *digest.Registry {
	t.Helper()
	r, err := digest.NewDefaultRegistry()
	if err != nil {
		t.Fatalf("build default registry: %v", err)
	}
	return r
}

func mustCompute(t *testing.T, r *digest.Registry, msg proto.Message, profileID string) (digest.Reference, []byte) {
	t.Helper()
	ref, b, err := r.Compute(msg, profileID)
	if err != nil {
		t.Fatalf("compute %s: %v", profileID, err)
	}
	return ref, b
}

func TestTodo_MODEL_007(t *testing.T) {
	t.Run("the reference round trips through its proto losslessly", func(t *testing.T) {
		r := mustRegistry(t)
		ref, _ := mustCompute(t, r, fixtureProposal(), digest.ProfileProposal)
		// Distinguish an absent optional from one explicitly set to empty:
		// collapsing those would quietly rewrite stored evidence.
		ref.CanonicalBytesArtifactRef = digest.Ptr("")
		ref.MaterialProfileRef = nil

		back, err := digest.FromProto(ref.ToProto())
		if err != nil {
			t.Fatalf("FromProto: %v", err)
		}
		if !reflectEqual(t, ref, back) {
			t.Fatalf("round trip lost data:\n got %+v\nwant %+v", back, ref)
		}
		if got := back.CanonicalBytesArtifactRef; got == nil || *got != "" {
			t.Fatalf("explicit empty optional became %v", got)
		}
		if back.MaterialProfileRef != nil {
			t.Fatalf("absent optional became %q", *back.MaterialProfileRef)
		}
		if _, err := digest.FromProto(nil); !errors.Is(err, digest.ErrInvalidReference) {
			t.Fatalf("want ErrInvalidReference for a nil proto, got %v", err)
		}
	})

	t.Run("the envelope records algorithm, profile, schema and payload digest", func(t *testing.T) {
		r := mustRegistry(t)
		ref, canonicalBytes := mustCompute(t, r, fixtureProposal(), digest.ProfileProposal)
		want := sha256.Sum256(canonicalBytes)
		if ref.Digest != hex.EncodeToString(want[:]) {
			t.Fatalf("digest is not sha256 over the canonical bytes")
		}
		if ref.ProfileID != digest.ProfileProposal || ref.ProfileVersion != 1 {
			t.Fatalf("profile identity not recorded: %s", ref.Key())
		}
		if ref.SchemaID != digest.SchemaIntentsV1 || ref.SchemaVersion != digest.SchemaIntentsV1Version {
			t.Fatalf("schema identity not recorded: %s v%d", ref.SchemaID, ref.SchemaVersion)
		}
		if ref.AlgorithmID != digest.AlgorithmSHA256 {
			t.Fatalf("algorithm not recorded: %q", ref.AlgorithmID)
		}
		if ref.CanonicalLength != uint64(len(canonicalBytes)) {
			t.Fatalf("canonical length %d, encoded %d", ref.CanonicalLength, len(canonicalBytes))
		}
		if ref.ScopeBindingDigest == "" {
			t.Fatal("scope binding not recorded")
		}
		if ref.IntentID == nil || *ref.IntentID != "int-1000" {
			t.Fatalf("intent identity not carried: %v", ref.IntentID)
		}
		if ref.ProposalRevisionID == nil || *ref.ProposalRevisionID != "pr-1000" {
			t.Fatalf("proposal revision identity not carried: %v", ref.ProposalRevisionID)
		}
		if err := r.Verify(fixtureProposal(), ref); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})

	t.Run("unknown algorithms and profiles fail closed", func(t *testing.T) {
		r := mustRegistry(t)
		ref, _ := mustCompute(t, r, fixtureProposal(), digest.ProfileProposal)

		bogusAlg := ref
		bogusAlg.AlgorithmID = "sha3-512"
		if err := r.Verify(fixtureProposal(), bogusAlg); !errors.Is(err, digest.ErrUnknownAlgorithm) {
			t.Fatalf("want ErrUnknownAlgorithm, got %v", err)
		}
		bogusProfile := ref
		bogusProfile.ProfileID = digest.ProfileLedgerEvent
		if err := r.Verify(fixtureProposal(), bogusProfile); !errors.Is(err, digest.ErrUnknownProfile) {
			t.Fatalf("want ErrUnknownProfile, got %v", err)
		}
		unpublishedVersion := ref
		unpublishedVersion.ProfileVersion = 99
		if err := r.Verify(fixtureProposal(), unpublishedVersion); !errors.Is(err, digest.ErrUnknownProfile) {
			t.Fatalf("want ErrUnknownProfile for an unpublished version, got %v", err)
		}
		if _, _, err := r.Compute(fixtureProposal(), digest.ProfileConfigBundle); !errors.Is(err, digest.ErrUnknownProfile) {
			t.Fatalf("want ErrUnknownProfile computing under an unpublished profile, got %v", err)
		}
	})

	t.Run("a nullable or empty digest never verifies", func(t *testing.T) {
		r := mustRegistry(t)
		ref, _ := mustCompute(t, r, fixtureProposal(), digest.ProfileProposal)
		for name, mutate := range map[string]func(*digest.Reference){
			"empty":        func(x *digest.Reference) { x.Digest = "" },
			"all zero":     func(x *digest.Reference) { x.Digest = strings.Repeat("00", 32) },
			"not hex":      func(x *digest.Reference) { x.Digest = "not-a-digest" },
			"no algorithm": func(x *digest.Reference) { x.AlgorithmID = "" },
		} {
			bad := ref
			mutate(&bad)
			err := r.Verify(fixtureProposal(), bad)
			if err == nil {
				t.Errorf("%s digest verified", name)
			}
		}
		empty := ref
		empty.Digest = ""
		if err := r.Verify(fixtureProposal(), empty); !errors.Is(err, digest.ErrEmptyDigest) {
			t.Fatalf("want ErrEmptyDigest, got %v", err)
		}
		zeroed := ref
		zeroed.Digest = strings.Repeat("00", 32)
		if err := r.Verify(fixtureProposal(), zeroed); !errors.Is(err, digest.ErrEmptyDigest) {
			t.Fatalf("want ErrEmptyDigest for an all-zero digest, got %v", err)
		}
	})

	t.Run("profile substitution fails", func(t *testing.T) {
		r := mustRegistry(t)
		if err := r.RegisterProfile(proposalProfileV2(), digest.ProposalScope()); err != nil {
			t.Fatalf("register v2: %v", err)
		}
		ref, _ := mustComputeAt(t, r, fixtureProposal(), digest.Key{ProfileID: digest.ProfileProposal, Version: 1})

		// Presenting a v1 reference where a v2 reference is expected.
		err := r.VerifyAgainst(fixtureProposal(), ref,
			digest.Key{ProfileID: digest.ProfileProposal, Version: 2})
		if !errors.Is(err, digest.ErrProfileSubstitution) {
			t.Fatalf("want ErrProfileSubstitution across versions, got %v", err)
		}
		// Presenting a proposal reference where an idempotent-request
		// reference is expected.
		err = r.VerifyAgainst(fixtureProposal(), ref,
			digest.Key{ProfileID: digest.ProfileIdempotentRequest, Version: 1})
		if !errors.Is(err, digest.ErrProfileSubstitution) {
			t.Fatalf("want ErrProfileSubstitution across profile ids, got %v", err)
		}
		// Relabelling the reference onto another profile version does not make
		// it verify: the recomputation under that profile does not match.
		relabelled := ref
		relabelled.ProfileVersion = 2
		if err := r.Verify(fixtureProposal(), relabelled); !errors.Is(err, digest.ErrDigestMismatch) {
			t.Fatalf("want ErrDigestMismatch for a relabelled reference, got %v", err)
		}
		// Relabelling onto a profile for a different canonical model fails on
		// the schema, not by accidentally hashing the wrong message.
		crossModel := ref
		crossModel.ProfileID = digest.ProfileIdempotentRequest
		if err := r.Verify(fixtureProposal(), crossModel); !errors.Is(err, canonical.ErrSchemaMismatch) {
			t.Fatalf("want ErrSchemaMismatch across canonical models, got %v", err)
		}
		// Claiming a different schema version than the profile publishes.
		reschema := ref
		reschema.SchemaVersion = 2
		if err := r.Verify(fixtureProposal(), reschema); !errors.Is(err, digest.ErrProfileSubstitution) {
			t.Fatalf("want ErrProfileSubstitution for a schema claim, got %v", err)
		}
	})

	t.Run("a profile that omits a required material path cannot be published", func(t *testing.T) {
		r := digest.NewRegistry()
		required, forbidden := digest.MaterialityFloor(digest.ProfileProposal)
		if len(required) == 0 || len(forbidden) == 0 {
			t.Fatal("the proposal profile has no materiality floor")
		}
		for _, drop := range required {
			p := digest.ProposalProfileV1()
			p.Material = slices.DeleteFunc(slices.Clone(p.Material), func(m string) bool {
				return m == drop || strings.HasPrefix(m, drop+".")
			})
			err := r.RegisterProfile(p, digest.ProposalScope())
			if !errors.Is(err, digest.ErrOmittedMaterialPath) {
				t.Errorf("dropping %q: want ErrOmittedMaterialPath, got %v", drop, err)
			}
		}
	})

	t.Run("a profile that binds revalidated context cannot be published", func(t *testing.T) {
		r := digest.NewRegistry()
		p := digest.ProposalProfileV1()
		p.Material = append(slices.Clone(p.Material), "control_snapshots.policy_bundle_digest")
		if err := r.RegisterProfile(p, digest.ProposalScope()); !errors.Is(err, digest.ErrNonMaterialPath) {
			t.Fatalf("want ErrNonMaterialPath for control_snapshots, got %v", err)
		}
		q := digest.ProposalProfileV1()
		q.Material = append(slices.Clone(q.Material), "material_proposal_digest")
		if err := r.RegisterProfile(q, digest.ProposalScope()); !errors.Is(err, digest.ErrNonMaterialPath) {
			t.Fatalf("want ErrNonMaterialPath for a self-referencing digest, got %v", err)
		}
		lenient := digest.ProposalProfileV1()
		lenient.RejectUnknownFields = false
		if err := r.RegisterProfile(lenient, digest.ProposalScope()); !errors.Is(err, digest.ErrNonMaterialPath) {
			t.Fatalf("want a material profile to be forced to reject unknown fields, got %v", err)
		}
	})

	t.Run("published profile versions are immutable", func(t *testing.T) {
		r := mustRegistry(t)
		if err := r.RegisterProfile(digest.ProposalProfileV1(), digest.ProposalScope()); !errors.Is(err, digest.ErrAlreadyRegistered) {
			t.Fatalf("want ErrAlreadyRegistered, got %v", err)
		}
	})

	t.Run("the built-in PROPOSAL profile binds the spec material list and not the snapshots", func(t *testing.T) {
		p := digest.ProposalProfileV1()
		want := []string{
			"intent_id",
			"proposal.canonical_digest.algorithm_id",
			"proposal.canonical_digest.canonical_length",
			"proposal.canonical_digest.digest",
			"proposal.canonical_digest.profile_id",
			"proposal.canonical_digest.profile_version",
			"proposal.canonical_digest.schema_id",
			"proposal.canonical_digest.schema_version",
			"proposal.canonical_digest.scope_binding_digest",
			"proposal.protobuf_wire_bytes",
			"proposal.schema.descriptor_digest",
			"proposal.schema.protobuf_full_name",
			"proposal.schema.schema_id",
			"proposal.schema.version",
			"proposal_revision_id",
			"revision",
			"supersedes_proposal_revision_id",
		}
		if got := p.MaterialPaths(); !slices.Equal(got, want) {
			t.Fatalf("PROPOSAL material list drifted\n got %v\nwant %v", got, want)
		}
		for _, m := range p.Material {
			if strings.HasPrefix(m, "control_snapshots") {
				t.Fatalf("PROPOSAL binds revalidated context: %q", m)
			}
			if m == "material_proposal_digest" {
				t.Fatal("PROPOSAL binds its own digest")
			}
		}
	})

	t.Run("a policy republish keeps the approval, a payload change breaks it", func(t *testing.T) {
		r := mustRegistry(t)
		approved, _ := mustCompute(t, r, fixtureProposal(), digest.ProfileProposal)

		republished := fixtureProposal()
		republished.ControlSnapshots.PolicyBundleDigest = "sha256:policy-v2"
		republished.ControlSnapshots.ClassificationTaxonomyDigest = "sha256:taxonomy-v2"
		republished.CreatedAt = timestamppb.New(time.Date(2027, 5, 5, 5, 5, 5, 0, time.UTC))
		if err := r.Verify(republished, approved); err != nil {
			t.Fatalf("a policy republish invalidated an approval: %v", err)
		}

		rewritten := fixtureProposal()
		rewritten.Proposal.ProtobufWireBytes = []byte{0x08, 0x2b}
		if err := r.Verify(rewritten, approved); !errors.Is(err, digest.ErrDigestMismatch) {
			t.Fatalf("a changed planned write kept its approval: %v", err)
		}
	})

	t.Run("the scope binding pins tenant, intent and proposal identity", func(t *testing.T) {
		r := mustRegistry(t)
		tenantScope := &digest.Scope{TenantID: "tenant-a", IntentID: "int-1000", ProposalRevisionID: "pr-1000"}
		ref, _, err := r.ComputeWith(fixtureProposal(),
			digest.Key{ProfileID: digest.ProfileProposal, Version: 1},
			digest.Options{Scope: tenantScope})
		if err != nil {
			t.Fatalf("compute with scope: %v", err)
		}
		if err := r.VerifyWithScope(fixtureProposal(), ref, tenantScope); err != nil {
			t.Fatalf("verify with the same scope: %v", err)
		}
		otherTenant := &digest.Scope{TenantID: "tenant-b", IntentID: "int-1000", ProposalRevisionID: "pr-1000"}
		if err := r.VerifyWithScope(fixtureProposal(), ref, otherTenant); !errors.Is(err, digest.ErrScopeMismatch) {
			t.Fatalf("want ErrScopeMismatch for a different tenant, got %v", err)
		}
		if err := r.Verify(fixtureProposal(), ref); !errors.Is(err, digest.ErrScopeMismatch) {
			t.Fatalf("want ErrScopeMismatch when the tenant is dropped, got %v", err)
		}
	})

	t.Run("historical references verify after profile evolution", func(t *testing.T) {
		r := mustRegistry(t)
		v1 := digest.Key{ProfileID: digest.ProfileProposal, Version: 1}
		historical, _ := mustComputeAt(t, r, fixtureProposal(), v1)

		if err := r.RegisterProfile(proposalProfileV2(), digest.ProposalScope()); err != nil {
			t.Fatalf("register v2: %v", err)
		}
		if err := r.Verify(fixtureProposal(), historical); err != nil {
			t.Fatalf("a v1 reference stopped verifying after v2 was published: %v", err)
		}
		latest, err := r.LatestKey(digest.ProfileProposal)
		if err != nil {
			t.Fatalf("latest: %v", err)
		}
		if latest.Version != 2 {
			t.Fatalf("latest version is %d, want 2", latest.Version)
		}
		fresh, _ := mustCompute(t, r, fixtureProposal(), digest.ProfileProposal)
		if fresh.Digest == historical.Digest {
			t.Fatal("v2 reproduced the v1 digest; the versions are not distinguishable")
		}
		if err := r.Verify(fixtureProposal(), fresh); err != nil {
			t.Fatalf("verify v2: %v", err)
		}
	})

	t.Run("migration links old and new without overwriting", func(t *testing.T) {
		r := mustRegistry(t)
		v1 := digest.Key{ProfileID: digest.ProfileProposal, Version: 1}
		v2 := digest.Key{ProfileID: digest.ProfileProposal, Version: 2}
		old, _ := mustComputeAt(t, r, fixtureProposal(), v1)
		if err := r.RegisterProfile(proposalProfileV2(), digest.ProposalScope()); err != nil {
			t.Fatalf("register v2: %v", err)
		}

		at := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
		m, err := r.Migrate(fixtureProposal(), old, v2, digest.MigrationOptions{
			Reason:   "profile v2 binds the payload digest profile reference",
			Approver: "migration-approver-1",
			Now:      func() time.Time { return at },
			NewID:    func() string { return "mig-0001" },
		})
		if err != nil {
			t.Fatalf("migrate: %v", err)
		}
		if m.Old.Digest != old.Digest || m.Old.ProfileVersion != 1 {
			t.Fatal("the migration overwrote the old envelope")
		}
		if !m.Comparison.DigestChanged {
			t.Fatal("the migration did not record a digest change")
		}
		if !slices.Equal(m.Comparison.MaterialPathsAdded,
			[]string{"proposal.canonical_digest.material_profile_ref"}) {
			t.Fatalf("material path comparison: %v", m.Comparison.MaterialPathsAdded)
		}
		if m.RecordedAt != at || m.MigrationID != "mig-0001" {
			t.Fatalf("migration record: %v %q", m.RecordedAt, m.MigrationID)
		}
		if m.Implementation == "" {
			t.Fatal("the migration recorded no implementation")
		}
		// Both envelopes keep verifying: that is what dual digests mean.
		if err := r.VerifyDual(fixtureProposal(), m.DualDigests()); err != nil {
			t.Fatalf("dual verification: %v", err)
		}

		if _, err := r.Migrate(fixtureProposal(), old, v2, digest.MigrationOptions{}); err == nil {
			t.Fatal("a migration without a reason or approver was accepted")
		}
		tampered := old
		tampered.Digest = strings.Repeat("11", 32)
		if _, err := r.Migrate(fixtureProposal(), tampered, v2, digest.MigrationOptions{
			Reason: "x", Approver: "y",
		}); !errors.Is(err, digest.ErrDigestMismatch) {
			t.Fatalf("want ErrDigestMismatch migrating an unverifiable digest, got %v", err)
		}
	})

	t.Run("algorithm agility carries dual digests", func(t *testing.T) {
		r := mustRegistry(t)
		if err := r.RegisterAlgorithm(digest.Algorithm{ID: "sha256", New: sha256.New}); !errors.Is(err, digest.ErrAlreadyRegistered) {
			t.Fatalf("want ErrAlreadyRegistered, got %v", err)
		}
		if err := r.RegisterAlgorithm(digest.Algorithm{ID: "sha512-256", New: newSHA512_256}); err != nil {
			t.Fatalf("register algorithm: %v", err)
		}
		v1 := digest.Key{ProfileID: digest.ProfileProposal, Version: 1}
		outgoing, _ := mustComputeAt(t, r, fixtureProposal(), v1)
		incoming, _, err := r.ComputeWith(fixtureProposal(), v1, digest.Options{AlgorithmID: "sha512-256"})
		if err != nil {
			t.Fatalf("compute under the incoming algorithm: %v", err)
		}
		if outgoing.Digest == incoming.Digest {
			t.Fatal("two algorithms produced the same digest")
		}
		if err := r.VerifyDual(fixtureProposal(), []digest.Reference{outgoing, incoming}); err != nil {
			t.Fatalf("dual verification during transition: %v", err)
		}
		// Historical hashes survive the algorithm change.
		if err := r.Verify(fixtureProposal(), outgoing); err != nil {
			t.Fatalf("the outgoing algorithm stopped verifying: %v", err)
		}
	})

	t.Run("the idempotent request profile treats subjects as a set", func(t *testing.T) {
		r := mustRegistry(t)
		ref, _ := mustCompute(t, r, fixtureIntent(), digest.ProfileIdempotentRequest)

		reordered := fixtureIntent()
		slices.Reverse(reordered.Subjects)
		if err := r.Verify(reordered, ref); err != nil {
			t.Fatalf("subject order was material: %v", err)
		}
		noisy := fixtureIntent()
		noisy.TraceId = "trace-2"
		noisy.CorrelationId = "corr-2"
		noisy.CreatedAt = timestamppb.New(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
		if err := r.Verify(noisy, ref); err != nil {
			t.Fatalf("a transient trace was material: %v", err)
		}
		changed := fixtureIntent()
		changed.IdempotencyKey = "idem-xyz"
		if err := r.Verify(changed, ref); !errors.Is(err, digest.ErrDigestMismatch) {
			t.Fatalf("the idempotency key was not material: %v", err)
		}
	})

	t.Run("Explain names the material paths behind a digest", func(t *testing.T) {
		r := mustRegistry(t)
		x, err := r.Explain(fixtureProposal(), digest.ProfileProposal)
		if err != nil {
			t.Fatalf("explain: %v", err)
		}
		if x.ProfileID != digest.ProfileProposal {
			t.Fatalf("explanation names %q", x.ProfileID)
		}
		for _, p := range x.ContributingPaths() {
			if strings.HasPrefix(p, "control_snapshots") || strings.HasPrefix(p, "created_") {
				t.Fatalf("excluded path %q contributed", p)
			}
		}
		if !slices.Contains(x.ContributingPaths(), "proposal.protobuf_wire_bytes") {
			t.Fatal("the planned writes did not contribute")
		}
	})
}

func TestTodo_MODEL_007_Property(t *testing.T) {
	t.Run("compute then verify always round trips", func(t *testing.T) {
		r := mustRegistry(t)
		for i := range 200 {
			p := fixtureProposal()
			p.Revision = uint64(i)
			p.ProposalRevisionId = "pr-" + strings.Repeat("x", i%7)
			ref, _ := mustCompute(t, r, p, digest.ProfileProposal)
			if err := r.Verify(p, ref); err != nil {
				t.Fatalf("iteration %d: %v", i, err)
			}
			// Any single-character change to the digest must fail.
			flipped := ref
			flipped.Digest = flipHex(ref.Digest)
			if err := r.Verify(p, flipped); !errors.Is(err, digest.ErrDigestMismatch) {
				t.Fatalf("iteration %d: a flipped digest verified: %v", i, err)
			}
		}
	})

	t.Run("references are stable across recomputation", func(t *testing.T) {
		r := mustRegistry(t)
		first, _ := mustCompute(t, r, fixtureProposal(), digest.ProfileProposal)
		for i := range 64 {
			again, _ := mustCompute(t, r, fixtureProposal(), digest.ProfileProposal)
			if again.Digest != first.Digest || again.ScopeBindingDigest != first.ScopeBindingDigest {
				t.Fatalf("iteration %d produced a different reference", i)
			}
		}
	})
}

func TestTodo_MODEL_007_Golden(t *testing.T) {
	r := mustRegistry(t)
	v1 := digest.Key{ProfileID: digest.ProfileProposal, Version: 1}

	ref, canonicalBytes := mustComputeAt(t, r, fixtureProposal(), v1)
	goldenBytes(t, "proposal_v1.canonical.bin", canonicalBytes)
	goldenReference(t, "proposal_v1.reference.json", ref)

	intentRef, intentBytes := mustComputeAt(t, r, fixtureIntent(),
		digest.Key{ProfileID: digest.ProfileIdempotentRequest, Version: 1})
	goldenBytes(t, "idempotent_request_v1.canonical.bin", intentBytes)
	goldenReference(t, "idempotent_request_v1.reference.json", intentRef)
	if *update {
		return
	}

	t.Run("the historical vector verifies after profile evolution", func(t *testing.T) {
		fresh := mustRegistry(t)
		historical := loadReference(t, "proposal_v1.reference.json")
		if err := fresh.Verify(fixtureProposal(), historical); err != nil {
			t.Fatalf("the checked-in v1 vector does not verify: %v", err)
		}
		if err := fresh.RegisterProfile(proposalProfileV2(), digest.ProposalScope()); err != nil {
			t.Fatalf("register v2: %v", err)
		}
		if err := fresh.Verify(fixtureProposal(), historical); err != nil {
			t.Fatalf("the checked-in v1 vector stopped verifying after v2: %v", err)
		}
		if err := fresh.Verify(fixtureIntent(), loadReference(t, "idempotent_request_v1.reference.json")); err != nil {
			t.Fatalf("the checked-in idempotent request vector does not verify: %v", err)
		}
	})
}

// FuzzTodo_MODEL_007 drives arbitrary wire bytes through compute and verify.
// No input may panic; anything that computes must verify, and must stop
// verifying once its digest is altered.
func FuzzTodo_MODEL_007(f *testing.F) {
	r, err := digest.NewDefaultRegistry()
	if err != nil {
		f.Fatalf("registry: %v", err)
	}
	for _, seed := range [][]byte{nil, {}, mustMarshalSeed(f, fixtureProposal())} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 8192 {
			t.Skip("oversized input")
		}
		var msg intentsv1.ProposalRevision
		if err := proto.Unmarshal(data, &msg); err != nil {
			return
		}
		ref, canonicalBytes, err := r.Compute(&msg, digest.ProfileProposal)
		if err != nil {
			return
		}
		if ref.CanonicalLength != uint64(len(canonicalBytes)) {
			t.Fatalf("length %d, encoded %d", ref.CanonicalLength, len(canonicalBytes))
		}
		if err := r.Verify(&msg, ref); err != nil {
			t.Fatalf("a freshly computed reference did not verify: %v", err)
		}
		if _, err := digest.FromProto(ref.ToProto()); err != nil {
			t.Fatalf("proto round trip: %v", err)
		}
		flipped := ref
		flipped.Digest = flipHex(ref.Digest)
		if err := r.Verify(&msg, flipped); err == nil {
			t.Fatal("an altered digest verified")
		}
	})
}

func mustMarshalSeed(f *testing.F, m proto.Message) []byte {
	f.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		f.Fatalf("marshal seed: %v", err)
	}
	return b
}
