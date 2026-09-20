package admin

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"time"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config/promotion"
)

func configDependencyKind(name string) (config.DependencyKind, error) {
	switch name {
	case "SCHEMA":
		return config.DependencySchema, nil
	case "RULE":
		return config.DependencyRule, nil
	case "WORKFLOW":
		return config.DependencyWorkflow, nil
	case "MAPPING":
		return config.DependencyMapping, nil
	case "CAPABILITY":
		return config.DependencyCapability, nil
	case "CONNECTOR":
		return config.DependencyConnector, nil
	case "AGENT":
		return config.DependencyAgent, nil
	case "REFERENCE":
		return config.DependencyReference, nil
	case "POLICY":
		return config.DependencyPolicy, nil
	default:
		return config.DependencyUnspecified, errors.New("admin: unknown config dependency kind " + name)
	}
}

func configValueKind(name string) (config.ValueKind, error) {
	switch name {
	case "STRING":
		return config.KindString, nil
	case "INT":
		return config.KindInt, nil
	case "BOOL":
		return config.KindBool, nil
	case "FLOAT":
		return config.KindFloat, nil
	case "DURATION":
		return config.KindDuration, nil
	case "LIST":
		return config.KindList, nil
	case "MAP":
		return config.KindMap, nil
	case "SECRET_REF":
		return config.KindSecretRef, nil
	default:
		return config.KindUnspecified, errors.New("admin: unknown config value kind " + name)
	}
}

func configSemantic(name string) (config.SemanticClass, error) {
	switch name {
	case "", "GENERIC":
		return config.SemanticGeneric, nil
	case "WORKFLOW":
		return config.SemanticWorkflow, nil
	case "SCHEMA":
		return config.SemanticSchema, nil
	case "MAPPING":
		return config.SemanticMapping, nil
	case "POLICY":
		return config.SemanticPolicy, nil
	default:
		return config.SemanticGeneric, errors.New("admin: unknown config semantic class " + name)
	}
}

func configPackageFromProto(pb *adminv1.ConfigPackage) (promotion.Package, error) {
	if pb == nil || pb.GetBundle() == nil || pb.GetBundle().GetBundle() == nil {
		return promotion.Package{}, errors.New("admin: config package with a signed bundle is required")
	}
	sb := pb.GetBundle()
	b := sb.GetBundle()
	deps := make([]config.Dependency, 0, len(b.GetDependencies()))
	for _, dep := range b.GetDependencies() {
		kind, err := configDependencyKind(dep.GetKind())
		if err != nil {
			return promotion.Package{}, err
		}
		deps = append(deps, config.Dependency{Kind: kind, Name: dep.GetName(), Version: dep.GetVersion(), Digest: dep.GetDigest()})
	}
	return promotion.Package{
		ID:          pb.GetId(),
		Environment: pb.GetEnvironment(),
		Bundle: config.SignedBundle{
			Bundle: config.Bundle{
				BundleID:              b.GetBundleId(),
				ManifestVersion:       b.GetManifestVersion(),
				Dependencies:          deps,
				CompatibilityRange:    b.GetCompatibilityRange(),
				Signer:                b.GetSigner(),
				Provenance:            b.GetProvenance(),
				CredentialRefs:        append([]string(nil), b.GetCredentialRefs()...),
				TargetScope:           b.GetTargetScope(),
				MinimumRuntimeVersion: b.GetMinimumRuntimeVersion(),
			},
			Digest:      sb.GetDigest(),
			SignerKeyID: sb.GetSignerKeyId(),
			Signature:   sb.GetSignature(),
		},
		PublicKey: ed25519.PublicKey(append([]byte(nil), pb.GetPublicKey()...)),
	}, nil
}

func configSnapshotFromProto(pb *adminv1.ConfigSnapshot) (config.Snapshot, error) {
	if pb == nil {
		return config.Snapshot{}, errors.New("admin: config snapshot is required")
	}
	entries := make([]config.Entry, 0, len(pb.GetEntries()))
	for _, e := range pb.GetEntries() {
		kind, err := configValueKind(e.GetKind())
		if err != nil {
			return config.Snapshot{}, err
		}
		semantic, err := configSemantic(e.GetSemantic())
		if err != nil {
			return config.Snapshot{}, err
		}
		refs := config.Refs{
			Capabilities: append([]string(nil), e.GetRefCapabilities()...),
			Workflows:    append([]string(nil), e.GetRefWorkflows()...),
			Tenants:      append([]string(nil), e.GetRefTenants()...),
		}
		if kind == config.KindSecretRef {
			if e.GetValue() != "" {
				return config.Snapshot{}, errors.New("admin: secret entry must carry a fingerprint, not a value")
			}
			entry, err := config.NewSecretEntry(e.GetKey(), e.GetSecretFingerprint(), e.GetExplicit(), semantic, refs)
			if err != nil {
				return config.Snapshot{}, err
			}
			entries = append(entries, entry)
			continue
		}
		entry, err := config.NewEntry(e.GetKey(), kind, e.GetValue(), e.GetExplicit(), semantic, refs)
		if err != nil {
			return config.Snapshot{}, err
		}
		entries = append(entries, entry)
	}
	return config.NewSnapshot(pb.GetName(), pb.GetVersion(), entries)
}

func configRecordProfile(rec promotion.Record) *adminv1.ConfigRecordProfile {
	status := string(rec.Status)
	out := &adminv1.ConfigRecordProfile{
		Id:             rec.Package.ID,
		Environment:    rec.Package.Environment,
		Status:         status,
		Digest:         rec.Package.Bundle.Digest,
		RollbackTo:     rec.RollbackTo,
		EvidenceDigest: rec.Evidence.EvidenceDigest,
	}
	switch rec.Status {
	case promotion.StatusApproved, promotion.StatusActive:
		out.Approved = true
		out.Approver = rec.Approval.Approver
		fallthrough
	case promotion.StatusSimulated:
		out.Simulated = true
		fallthrough
	case promotion.StatusValidated:
		out.Validated = true
	}
	return out
}

func configNow(s *server) time.Time {
	if s.deps.Now != nil {
		return s.deps.Now().UTC()
	}
	return time.Now().UTC()
}

// isProductionEnv reports whether the environment name denotes production.
// Promotion and rollback against production require explicit operator
// confirmation: without it the RPC refuses rather than guessing.
func isProductionEnv(environment string) bool {
	return strings.EqualFold(environment, "production") || strings.EqualFold(environment, "prod")
}
