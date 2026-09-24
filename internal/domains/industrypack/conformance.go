package industrypack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// PACK-007: prove pack composition with representative industries.
//
// [ComposeIndustry] runs one industry pack through every pack stage together
// with its country and customer packs: PACK-004 publication (compatibility,
// dependencies, and through it PACK-002 content and PACK-003 experience
// binding), PACK-002 content binding, PACK-003 experience binding and PACK-006
// layer composition. It returns deterministic configuration, workflow and
// model digests, the mandatory rules and settings the composition preserved,
// and the bundle digest a publisher signs. [ActivateIndustry] accepts only a
// PACK-005 envelope signed over exactly that bundle and persists only then.
//
// Any stage refusal is PACK_007_REJECTED naming the stage-qualified field,
// state and version, with zero effects.

// ConformanceRejectionCode is the stable PACK-007 refusal code.
const ConformanceRejectionCode = "PACK_007_REJECTED"

// Conformance-only refusal states.
const (
	ConformanceContentRefused = "CONTENT_REFUSED"
	ConformanceBundleMismatch = "BUNDLE_MISMATCH"
)

// ErrConformanceRejected is the sentinel every PACK-007 refusal unwraps to.
var ErrConformanceRejected = errors.New("industrypack: industry pack composition rejected")

// ConformanceRejection names the stage and offending field.
type ConformanceRejection struct {
	Code    string
	Stage   string
	Field   string
	State   string
	Version string
	Cause   error
}

func (e *ConformanceRejection) Error() string {
	return fmt.Sprintf("%s: %s.%s %s@%s: %v", e.Code, e.Stage, e.Field, e.State, e.Version, e.Cause)
}

// Unwrap exposes the sentinel.
func (e *ConformanceRejection) Unwrap() error { return ErrConformanceRejected }

// IndustryComposition is one industry pack with its country and customer
// packs.
type IndustryComposition struct {
	Publication PublicationCheck
	Content     BindingSpec
	Experience  ExperienceBindingSpec
	Layers      LayerSpec
}

// IndustryResult is a proven composition.
type IndustryResult struct {
	Industry       Industry `json:"industry"`
	PackID         string   `json:"pack_id"`
	Version        int      `json:"version"`
	PackDigest     string   `json:"pack_digest"`
	ConfigDigest   string   `json:"config_digest"`
	WorkflowDigest string   `json:"workflow_digest"`
	ModelDigest    string   `json:"model_digest"`
	// Mandatory lists the non-overridable rules and mandatory settings the
	// composition preserved, with their composed values.
	Mandatory    []string                   `json:"mandatory"`
	BundleDigest string                     `json:"bundle_digest"`
	Entitlement  IndustryEntitlementBinding `json:"-"`
}

func conformanceReject(stage string, err error) error {
	r := &ConformanceRejection{Code: ConformanceRejectionCode, Stage: stage, Cause: err}
	var (
		exp  *ExperienceRejection
		pub  *PublicationBlock
		lay  *LayerRefusal
		act  *ActivationRefusal
		cont *Refusal
		ent  *EntitlementRejection
	)
	switch {
	case errors.As(err, &pub):
		r.Field, r.State, r.Version = pub.Field, pub.State, pub.Version
	case errors.As(err, &exp):
		r.Field, r.State, r.Version = exp.Field, exp.State, exp.Version
	case errors.As(err, &lay):
		r.Field, r.State, r.Version = lay.Field, lay.State, lay.Version
	case errors.As(err, &act):
		r.Field, r.State, r.Version = act.Field, act.State, act.Version
	case errors.As(err, &cont):
		r.Field, r.State, r.Version = cont.Ref.Key(), ConformanceContentRefused, cont.Ref.Version
	case errors.As(err, &ent):
		r.Field, r.State, r.Version = "commercial_entitlement", ent.State, ""
	default:
		r.Field, r.State = stage, ConformanceContentRefused
	}
	return r
}

// ComposeIndustry proves one industry composition without effects.
func ComposeIndustry(ctx context.Context, authority *IndustryEntitlementAuthority, spec IndustryComposition) (IndustryResult, error) {
	pub, err := CheckPublication(ctx, authority, spec.Publication)
	if err != nil {
		return IndustryResult{}, conformanceReject("publication", err)
	}
	content, err := Bind(spec.Content)
	if err != nil {
		return IndustryResult{}, conformanceReject("content", err)
	}
	experience, err := BindExperience(spec.Experience)
	if err != nil {
		return IndustryResult{}, conformanceReject("experience", err)
	}
	layered, err := ComposeLayers(spec.Layers)
	if err != nil {
		return IndustryResult{}, conformanceReject("layers", err)
	}
	res := IndustryResult{Industry: spec.Publication.Candidate.Industry, PackID: pub.PackID, Version: pub.Version, PackDigest: pub.Digest,
		ConfigDigest: layered.Digest, WorkflowDigest: experience.Digest, ModelDigest: content.CanonicalDigest, Entitlement: pub.Entitlement}
	for _, c := range content.Contents {
		if c.Ref.Kind == ContentRule && !c.Overridable {
			res.Mandatory = append(res.Mandatory, "rule:"+c.Ref.Key()+"="+c.Digest)
		}
	}
	for _, s := range layered.Settings {
		if s.Mandatory {
			res.Mandatory = append(res.Mandatory, "setting:"+s.Class+":"+s.Family+"."+s.Key+"="+strings.Join(s.Values, ","))
		}
	}
	sort.Strings(res.Mandatory)
	sum := sha256.Sum256([]byte(strings.Join([]string{"hcmnext.industrypack.IndustryBundle/v1", res.PackID, strconv.Itoa(res.Version),
		res.PackDigest, res.ConfigDigest, res.WorkflowDigest, res.ModelDigest, strings.Join(res.Mandatory, ";")}, "\n")))
	res.BundleDigest = "sha256:" + hex.EncodeToString(sum[:])
	return res, nil
}

// IndustryActivationStore persists a proven, signed industry activation.
type IndustryActivationStore interface {
	SaveIndustryActivation(ctx context.Context, r IndustryResult, receipt ActivationReceipt) (ActivationEffects, error)
}

// ActivateIndustry proves the composition, requires a signed envelope over
// exactly its bundle, and only then persists.
func ActivateIndustry(ctx context.Context, store IndustryActivationStore, authority *IndustryEntitlementAuthority, spec IndustryComposition, activation ActivationRequest) (IndustryResult, ActivationReceipt, ActivationEffects, error) {
	if store == nil {
		return IndustryResult{}, ActivationReceipt{}, ActivationEffects{}, fmt.Errorf("%w: store is required", ErrConformanceRejected)
	}
	res, err := ComposeIndustry(ctx, authority, spec)
	if err != nil {
		return IndustryResult{}, ActivationReceipt{}, ActivationEffects{}, err
	}
	env := activation.Envelope
	if env.PackID != res.PackID || env.Version != res.Version || env.BundleDigest != res.BundleDigest {
		return IndustryResult{}, ActivationReceipt{}, ActivationEffects{}, &ConformanceRejection{Code: ConformanceRejectionCode, Stage: "activation",
			Field: "bundle_digest", State: ConformanceBundleMismatch, Version: strconv.Itoa(env.Version),
			Cause: fmt.Errorf("envelope %s@%d %s does not sign the proven bundle %s@%d %s", env.PackID, env.Version, env.BundleDigest, res.PackID, res.Version, res.BundleDigest)}
	}
	receipt, err := VerifyActivation(ctx, authority, activation)
	if err != nil {
		return IndustryResult{}, ActivationReceipt{}, ActivationEffects{}, conformanceReject("activation", err)
	}
	if receipt.Entitlement.Fingerprint() != res.Entitlement.Fingerprint() {
		return IndustryResult{}, ActivationReceipt{}, ActivationEffects{}, &ConformanceRejection{Code: ConformanceRejectionCode, Stage: "activation",
			Field: "entitlement_fingerprint", State: EntitlementSnapshotAbsent, Version: strconv.Itoa(env.Version), Cause: ErrEntitlementRejected}
	}
	effects, err := store.SaveIndustryActivation(ctx, res, receipt)
	if err != nil {
		return IndustryResult{}, ActivationReceipt{}, ActivationEffects{}, err
	}
	return res, receipt, effects, nil
}
