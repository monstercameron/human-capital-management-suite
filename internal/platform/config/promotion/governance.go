package promotion

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
)

var (
	ErrGovernancePolicy        = errors.New("config promotion: invalid parameter governance policy")
	ErrChangeNotPermitted      = errors.New("config promotion: actor may not change parameter")
	ErrImpactCatalogIncomplete = errors.New("config promotion: impact catalog is incomplete")
	ErrApprovalRequired        = errors.New("config promotion: high-impact change requires approval")
	ErrSelfApproval            = errors.New("config promotion: author cannot approve own high-impact change")
)

// ParameterPermission declares the actors that may change one parameter.
// Callers pass verifier-resolved actor identifiers; transport-provided role
// or actor strings are not permissions.
type ParameterPermission struct {
	Key     string
	Editors []string
}

// ParameterReader is one indexed use of a parameter. Workflow compilation
// and runtime integration own producing this inventory; this package only
// renders a complete, deterministic preview from it.
type ParameterReader struct {
	Key               string
	WorkflowID        string
	PackageID         string
	RunningInstanceID string
	BindingMode       string
}

// ImpactCatalog must be marked complete by its authoritative producer. An
// incomplete or absent reader index cannot be presented as an empty impact.
type ImpactCatalog struct {
	Complete bool
	Readers  []ParameterReader
}

// ChangeRequest is immutable-by-digest evidence for one parameter change
// package. HighImpactKeys are derived from the supplied parameter definitions.
type ChangeRequest struct {
	PackageID        string
	PackageDigest    string
	Author           string
	ChangeDigest     string
	PermissionDigest string
	Keys             []string
	HighImpactKeys   []string
	ApprovalRequired bool
	Digest           string
}

// ParameterImpact is one reader site that may be affected by a request.
type ParameterImpact struct {
	Key               string
	WorkflowID        string
	PackageID         string
	RunningInstanceID string
	BindingMode       string
}

// ImpactPreview is bound to one request and contains the impact sites for
// changed parameter keys. It contains no parameter values.
type ImpactPreview struct {
	PackageID     string
	PackageDigest string
	ChangeDigest  string
	RequestDigest string
	Impacts       []ParameterImpact
	Digest        string
}

// ParameterApproval is immutable evidence approving one exact high-impact
// change request and impact preview.
type ParameterApproval struct {
	PackageID     string
	PackageDigest string
	ChangeDigest  string
	RequestDigest string
	PreviewDigest string
	Author        string
	Approver      string
	ApprovedAt    time.Time
	Digest        string
}

// AuthorizeParameterChanges checks every changed key against its declared
// editor list and creates a digest-bound request. A changed key without a
// permission rule is denied.
func AuthorizeParameterChanges(packageID, packageDigest, author string, diff config.DiffResult, definitions []config.ParameterDefinition, permissions []ParameterPermission) (ChangeRequest, error) {
	if strings.TrimSpace(packageID) == "" || strings.TrimSpace(packageDigest) == "" || strings.TrimSpace(author) == "" || strings.TrimSpace(diff.Digest) == "" {
		return ChangeRequest{}, ErrGovernancePolicy
	}
	rules := make(map[string]map[string]bool, len(permissions))
	for _, permission := range permissions {
		key := strings.TrimSpace(permission.Key)
		if key == "" || len(permission.Editors) == 0 {
			return ChangeRequest{}, ErrGovernancePolicy
		}
		if _, exists := rules[key]; exists {
			return ChangeRequest{}, ErrGovernancePolicy
		}
		editors := make(map[string]bool, len(permission.Editors))
		for _, editor := range permission.Editors {
			if strings.TrimSpace(editor) == "" || editors[editor] {
				return ChangeRequest{}, ErrGovernancePolicy
			}
			editors[editor] = true
		}
		rules[key] = editors
	}
	definitionByKey := make(map[string]config.ParameterDefinition, len(definitions))
	for _, definition := range definitions {
		if err := definition.Validate(); err != nil {
			return ChangeRequest{}, fmt.Errorf("%w: invalid definition for %q", ErrGovernancePolicy, definition.Key)
		}
		if _, duplicate := definitionByKey[definition.Key]; duplicate {
			return ChangeRequest{}, ErrGovernancePolicy
		}
		definitionByKey[definition.Key] = definition
	}
	keys := changedKeys(diff)
	var high []string
	policyParts := []string{"hcmnext.config.parameter-permissions.v1"}
	for _, key := range keys {
		editors, ok := rules[key]
		if !ok || !editors[author] {
			return ChangeRequest{}, fmt.Errorf("%w: %s", ErrChangeNotPermitted, key)
		}
		definition, defined := definitionByKey[key]
		if !defined {
			return ChangeRequest{}, fmt.Errorf("%w: parameter definition missing for %s", ErrGovernancePolicy, key)
		}
		if definition.HighImpact {
			high = append(high, key)
		}
		policyParts = append(policyParts, key)
		editorIDs := make([]string, 0, len(editors))
		for editor := range editors {
			editorIDs = append(editorIDs, editor)
		}
		sort.Strings(editorIDs)
		policyParts = append(policyParts, strings.Join(editorIDs, "\x00"), fmt.Sprint(definition.HighImpact))
	}
	request := ChangeRequest{PackageID: packageID, PackageDigest: packageDigest, Author: author, ChangeDigest: diff.Digest, PermissionDigest: digestStrings(policyParts...), Keys: keys, HighImpactKeys: high, ApprovalRequired: len(high) > 0}
	request.Digest = changeRequestDigest(request)
	return request, nil
}

// PreviewImpact requires a complete reader index, so an absent integration
// cannot be mistaken for a parameter with no consumers.
func PreviewImpact(request ChangeRequest, catalog ImpactCatalog) (ImpactPreview, error) {
	if !catalog.Complete {
		return ImpactPreview{}, ErrImpactCatalogIncomplete
	}
	if !validChangeRequest(request) {
		return ImpactPreview{}, ErrGovernancePolicy
	}
	changed := make(map[string]bool, len(request.Keys))
	for _, key := range request.Keys {
		changed[key] = true
	}
	impacts := make([]ParameterImpact, 0)
	seen := make(map[ParameterReader]bool)
	for _, reader := range catalog.Readers {
		if !changed[reader.Key] {
			continue
		}
		if strings.TrimSpace(reader.BindingMode) == "" || (reader.WorkflowID == "" && reader.PackageID == "" && reader.RunningInstanceID == "") {
			return ImpactPreview{}, ErrGovernancePolicy
		}
		if seen[reader] {
			continue
		}
		seen[reader] = true
		impacts = append(impacts, ParameterImpact{Key: reader.Key, WorkflowID: reader.WorkflowID, PackageID: reader.PackageID, RunningInstanceID: reader.RunningInstanceID, BindingMode: reader.BindingMode})
	}
	sort.Slice(impacts, func(i, j int) bool {
		a, b := impacts[i], impacts[j]
		return strings.Join([]string{a.Key, a.WorkflowID, a.PackageID, a.RunningInstanceID, a.BindingMode}, "\x00") < strings.Join([]string{b.Key, b.WorkflowID, b.PackageID, b.RunningInstanceID, b.BindingMode}, "\x00")
	})
	preview := ImpactPreview{PackageID: request.PackageID, PackageDigest: request.PackageDigest, ChangeDigest: request.ChangeDigest, RequestDigest: request.Digest, Impacts: impacts}
	preview.Digest = impactPreviewDigest(preview)
	return preview, nil
}

// ApproveParameterChange approves a high-impact request only after an impact
// preview for that exact request exists and a distinct actor approves it.
func ApproveParameterChange(request ChangeRequest, preview ImpactPreview, approver string, at time.Time) (ParameterApproval, error) {
	if !validChangeRequest(request) || !validImpactPreview(preview) || preview.RequestDigest != request.Digest || preview.PackageID != request.PackageID || preview.PackageDigest != request.PackageDigest || preview.ChangeDigest != request.ChangeDigest {
		return ParameterApproval{}, ErrStale
	}
	if !request.ApprovalRequired {
		return ParameterApproval{}, ErrApprovalRequired
	}
	if strings.TrimSpace(approver) == "" || approver == request.Author {
		return ParameterApproval{}, ErrSelfApproval
	}
	if at.IsZero() {
		return ParameterApproval{}, ErrGovernancePolicy
	}
	approval := ParameterApproval{PackageID: request.PackageID, PackageDigest: request.PackageDigest, ChangeDigest: request.ChangeDigest, RequestDigest: request.Digest, PreviewDigest: preview.Digest, Author: request.Author, Approver: approver, ApprovedAt: at.UTC()}
	approval.Digest = parameterApprovalDigest(approval)
	return approval, nil
}

func changedKeys(diff config.DiffResult) []string {
	keys := make([]string, 0, len(diff.Added)+len(diff.Removed)+len(diff.Changed))
	for _, group := range [][]config.Change{diff.Added, diff.Removed, diff.Changed} {
		for _, change := range group {
			if strings.TrimSpace(change.Key) != "" {
				keys = append(keys, change.Key)
			}
		}
	}
	sort.Strings(keys)
	return compactStrings(keys)
}

func changeRequestDigest(request ChangeRequest) string {
	return digestStrings("hcmnext.config.parameter-change.v1", request.PackageID, request.PackageDigest, request.Author, request.ChangeDigest, request.PermissionDigest, strings.Join(request.Keys, "\x00"), strings.Join(request.HighImpactKeys, "\x00"), fmt.Sprint(request.ApprovalRequired))
}

func impactPreviewDigest(preview ImpactPreview) string {
	parts := []string{"hcmnext.config.parameter-impact.v1", preview.PackageID, preview.PackageDigest, preview.ChangeDigest, preview.RequestDigest}
	for _, impact := range preview.Impacts {
		parts = append(parts, impact.Key, impact.WorkflowID, impact.PackageID, impact.RunningInstanceID, impact.BindingMode)
	}
	return digestStrings(parts...)
}

func parameterApprovalDigest(approval ParameterApproval) string {
	return digestStrings("hcmnext.config.parameter-approval.v1", approval.PackageID, approval.PackageDigest, approval.ChangeDigest, approval.RequestDigest, approval.PreviewDigest, approval.Author, approval.Approver, approval.ApprovedAt.UTC().Format(time.RFC3339Nano))
}

func validChangeRequest(request ChangeRequest) bool {
	if request.PackageID == "" || request.PackageDigest == "" || request.Author == "" || request.ChangeDigest == "" || len(request.Keys) == 0 || request.Digest == "" {
		return false
	}
	return request.Digest == changeRequestDigest(request)
}

func validImpactPreview(preview ImpactPreview) bool {
	return preview.PackageID != "" && preview.PackageDigest != "" && preview.ChangeDigest != "" && preview.RequestDigest != "" && preview.Digest != "" && preview.Digest == impactPreviewDigest(preview)
}

func digestStrings(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(h, "%d:%s;", len(part), part)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func compactStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}
