package releasefixture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/testprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// SavedRunFormat identifies the canonical author-run fixture data shape.
const SavedRunFormat = "hcmnext.workflow.saved-test-run/v1"

var (
	ErrSavedRunMalformed   = errors.New("releasefixture: malformed saved test run")
	ErrSavedRunDigest      = errors.New("releasefixture: saved test run digest does not match its content")
	ErrSavedRunMismatch    = errors.New("releasefixture: saved test run is not bound to this compiled version")
	ErrRunDigestMismatch   = errors.New("releasefixture: reproduced test run result differs from the saved result")
	ErrScenarioUnsupported = errors.New("releasefixture: simulator does not record this scenario action yet")
)

// OutcomeChoice is an author-selected branch outcome for a particular node.
type OutcomeChoice struct {
	NodeID  string `json:"node_id"`
	Outcome string `json:"outcome"`
}

// RolePlayAction records an explicit human action selected for a simulated
// work item. The action is scenario data; it does not claim that a real person
// approved or performed the action.
type RolePlayAction struct {
	NodeID    string    `json:"node_id"`
	Principal string    `json:"principal"`
	Action    string    `json:"action"`
	At        time.Time `json:"at,omitempty"`
}

// ClockMove records a requested virtual-clock movement in scenario order.
type ClockMove struct {
	To time.Time `json:"to"`
}

// SavedRun is the data captured from one author-run scenario. The expected
// result digest is minted only by Capture after it verifies a real simulator
// receipt against the exact published plan and compiler version.
type SavedRun struct {
	Format               string                 `json:"format"`
	FixtureRef           string                 `json:"fixture_ref"`
	WorkflowID           string                 `json:"workflow_id"`
	CompiledPlanDigest   string                 `json:"compiled_plan_digest"`
	CompilerVersion      string                 `json:"compiler_version"`
	Profile              testprofile.ProfileRef `json:"profile"`
	ProfileDigest        string                 `json:"profile_digest"`
	Outcomes             []OutcomeChoice        `json:"outcomes,omitempty"`
	RolePlay             []RolePlayAction       `json:"role_play,omitempty"`
	ClockMoves           []ClockMove            `json:"clock_moves,omitempty"`
	CancelAtNode         string                 `json:"cancel_at_node,omitempty"`
	ExpectedResultDigest string                 `json:"expected_result_digest"`
	FixtureDigest        string                 `json:"fixture_digest"`
}

// Capture creates a saved fixture from an actual run receipt. A caller cannot
// turn a claimed digest into a saved run: the receipt's unexported digest must
// verify, and its workflow, compiled plan and compiler must match v.
func Capture(ref string, profile testprofile.Profile, scenario SavedRun, v version.CompiledVersion, result simulate.Receipt) (SavedRun, error) {
	if err := v.Verify(); err != nil {
		return SavedRun{}, fmt.Errorf("%w: compiled version is invalid: %v", ErrSavedRunMismatch, err)
	}
	if err := result.Verify(); err != nil {
		return SavedRun{}, fmt.Errorf("%w: simulator receipt is invalid: %v", ErrSavedRunMalformed, err)
	}
	if err := profile.Validate(); err != nil {
		return SavedRun{}, fmt.Errorf("%w: profile: %v", ErrSavedRunMalformed, err)
	}
	profileDigest := profile.Digest()
	if !isSHA256(profileDigest) {
		return SavedRun{}, fmt.Errorf("%w: profile has no canonical digest", ErrSavedRunMalformed)
	}
	if strings.TrimSpace(ref) == "" || !contains(v.FixtureRefs, ref) {
		return SavedRun{}, fmt.Errorf("%w: fixture reference %q is not declared by the version", ErrSavedRunMismatch, ref)
	}
	if result.WorkflowID != v.WorkflowID || result.PlanDigest != v.CompiledPlanDigest || result.CompilerVersion != v.CompilerVersion {
		return SavedRun{}, fmt.Errorf("%w: receipt is %s@%s compiler %s; version is %s@%s compiler %s",
			ErrSavedRunMismatch, result.WorkflowID, result.PlanDigest, result.CompilerVersion,
			v.WorkflowID, v.CompiledPlanDigest, v.CompilerVersion)
	}
	fixture := scenario
	fixture.Format = SavedRunFormat
	fixture.FixtureRef = ref
	fixture.WorkflowID = v.WorkflowID
	fixture.CompiledPlanDigest = v.CompiledPlanDigest
	fixture.CompilerVersion = v.CompilerVersion
	fixture.Profile = profile.Ref
	fixture.ProfileDigest = profileDigest
	fixture.ExpectedResultDigest = "sha256:" + result.Digest()
	fixture.FixtureDigest = ""
	if err := fixture.validateContent(); err != nil {
		return SavedRun{}, err
	}
	if err := fixture.validateSupportedScenario(); err != nil {
		return SavedRun{}, err
	}
	if err := validateProfileOutcomes(profile, v, result, fixture.Outcomes); err != nil {
		return SavedRun{}, err
	}
	return fixture.seal(), nil
}

func (f SavedRun) seal() SavedRun {
	f.Outcomes = append([]OutcomeChoice(nil), f.Outcomes...)
	f.RolePlay = append([]RolePlayAction(nil), f.RolePlay...)
	f.ClockMoves = append([]ClockMove(nil), f.ClockMoves...)
	f.FixtureDigest = f.ComputeDigest()
	return f
}

func (f SavedRun) ComputeDigest() string {
	f.FixtureDigest = ""
	b, err := json.Marshal(f)
	if err != nil {
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(SavedRunFormat))
	h.Write([]byte{0})
	h.Write(b)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Verify checks fixture structure, content integrity and exact version
// binding. Reproduction is a separate step and must still execute the runner.
func (f SavedRun) Verify(v version.CompiledVersion) error {
	if err := v.Verify(); err != nil {
		return fmt.Errorf("%w: compiled version is invalid: %v", ErrSavedRunMismatch, err)
	}
	if err := f.validateContent(); err != nil {
		return err
	}
	if f.FixtureDigest == "" || f.FixtureDigest != f.ComputeDigest() {
		return ErrSavedRunDigest
	}
	if f.WorkflowID != v.WorkflowID || f.CompiledPlanDigest != v.CompiledPlanDigest ||
		f.CompilerVersion != v.CompilerVersion || !contains(v.FixtureRefs, f.FixtureRef) {
		return ErrSavedRunMismatch
	}
	return nil
}

func (f SavedRun) validateContent() error {
	if f.Format != SavedRunFormat || strings.TrimSpace(f.FixtureRef) == "" ||
		strings.TrimSpace(f.WorkflowID) == "" || strings.TrimSpace(f.CompiledPlanDigest) == "" ||
		strings.TrimSpace(f.CompilerVersion) == "" || !isSHA256(f.ProfileDigest) || !isSHA256(f.ExpectedResultDigest) {
		return fmt.Errorf("%w: format, fixture, version and result digest are required", ErrSavedRunMalformed)
	}
	if err := f.Profile.Validate(); err != nil {
		return fmt.Errorf("%w: profile: %v", ErrSavedRunMalformed, err)
	}
	seen := map[string]bool{}
	for _, choice := range f.Outcomes {
		if strings.TrimSpace(choice.NodeID) == "" || strings.TrimSpace(choice.Outcome) == "" || seen[choice.NodeID] {
			return fmt.Errorf("%w: outcome choices need unique node ids and non-empty outcomes", ErrSavedRunMalformed)
		}
		seen[choice.NodeID] = true
	}
	for _, action := range f.RolePlay {
		if strings.TrimSpace(action.NodeID) == "" || strings.TrimSpace(action.Principal) == "" || strings.TrimSpace(action.Action) == "" {
			return fmt.Errorf("%w: role-play actions need node, principal and action", ErrSavedRunMalformed)
		}
	}
	for i, move := range f.ClockMoves {
		if move.To.IsZero() || (i > 0 && move.To.Before(f.ClockMoves[i-1].To)) {
			return fmt.Errorf("%w: clock moves must have non-zero non-decreasing instants", ErrSavedRunMalformed)
		}
	}
	return nil
}

func (f SavedRun) validateSupportedScenario() error {
	if len(f.RolePlay) > 0 || len(f.ClockMoves) > 0 || strings.TrimSpace(f.CancelAtNode) != "" {
		return ErrScenarioUnsupported
	}
	return nil
}

// Encode serializes the fixture as indented JSON.
func (f SavedRun) Encode() ([]byte, error) {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSavedRunMalformed, err)
	}
	return append(b, '\n'), nil
}

// DecodeSavedRun parses a fixture and refuses unknown or trailing data.
func DecodeSavedRun(b []byte) (SavedRun, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var f SavedRun
	if err := dec.Decode(&f); err != nil {
		return SavedRun{}, fmt.Errorf("%w: %v", ErrSavedRunMalformed, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return SavedRun{}, fmt.Errorf("%w: trailing data", ErrSavedRunMalformed)
	}
	return f, nil
}

// SavedRunRunner replays a saved scenario using the resolved profile and
// returns the simulator's newly minted receipt.
type SavedRunRunner func(context.Context, SavedRun, testprofile.Profile, version.CompiledVersion) (simulate.Receipt, error)

// ReproduceSavedRunWithProfiles resolves the fixture's pinned profile from the
// governed profile registry for tenant, checks its content digest, then runs
// the saved scenario against the exact compiled version.
func ReproduceSavedRunWithProfiles(ctx context.Context, f SavedRun, v version.CompiledVersion, profiles *testprofile.Registry, tenant string, runner SavedRunRunner) (retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.releasefixture.reproduce_saved_run", f)
	defer func() { observe.Done(op, retErr) }()
	if err := f.Verify(v); err != nil {
		return err
	}
	if err := f.validateSupportedScenario(); err != nil {
		return err
	}
	if runner == nil {
		return fmt.Errorf("%w: no saved-run executor", ErrNotReproduced)
	}
	if profiles == nil {
		return fmt.Errorf("%w: no profile registry", ErrNotReproduced)
	}
	profile, err := profiles.ResolveForTenant(f.Profile, tenant)
	if err != nil {
		return fmt.Errorf("%w: profile resolution failed: %v", ErrNotReproduced, err)
	}
	if profile.Digest() != f.ProfileDigest {
		return fmt.Errorf("%w: profile content changed", ErrSavedRunMismatch)
	}
	result, err := runner(ctx, f, profile, v)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotReproduced, err)
	}
	if err := result.Verify(); err != nil {
		return fmt.Errorf("%w: invalid simulator receipt: %v", ErrNotReproduced, err)
	}
	if err := validateProfileOutcomes(profile, v, result, f.Outcomes); err != nil {
		return fmt.Errorf("%w: %v", ErrNotReproduced, err)
	}
	if result.WorkflowID != v.WorkflowID || result.PlanDigest != v.CompiledPlanDigest ||
		result.CompilerVersion != v.CompilerVersion || "sha256:"+result.Digest() != f.ExpectedResultDigest {
		return ErrRunDigestMismatch
	}
	return nil
}

func validateProfileOutcomes(profile testprofile.Profile, v version.CompiledVersion, result simulate.Receipt, outcomes []OutcomeChoice) error {
	if len(outcomes) == 0 {
		return nil
	}
	plan, err := workflow.DecodeCanonicalPlan(v.CanonicalPlanBytes)
	if err != nil || plan.Digest() != v.CompiledPlanDigest {
		return fmt.Errorf("%w: canonical plan cannot validate profile outcomes", ErrSavedRunMismatch)
	}
	for _, choice := range outcomes {
		node, ok := plan.Node(choice.NodeID)
		if !ok || node.Capability == nil {
			return fmt.Errorf("%w: selected outcome node %s has no compiled capability", ErrSavedRunMalformed, choice.NodeID)
		}
		if _, err := profile.ResponseFor(choice.NodeID, node.Capability.ID, node.Capability.Version, workflow.Outcome(choice.Outcome)); err != nil {
			return fmt.Errorf("%w: profile response: %v", ErrSavedRunMalformed, err)
		}
		observed := false
		for _, trace := range result.Trace {
			if trace.NodeID == choice.NodeID && string(trace.Outcome) == choice.Outcome {
				observed = true
				break
			}
		}
		if !observed {
			return fmt.Errorf("%w: selected outcome %s for node %s was not observed in the run", ErrSavedRunMalformed, choice.Outcome, choice.NodeID)
		}
	}
	return nil
}

// SuiteFromSavedRuns adapts persisted data fixtures to the existing report
// path. Each fixture is re-executed from its data by runner; none is represented
// by a hand-authored successful closure.
func SuiteFromSavedRuns(fixtures []SavedRun, profiles *testprofile.Registry, tenant string, runner SavedRunRunner) (Suite, error) {
	if profiles == nil || strings.TrimSpace(tenant) == "" || runner == nil {
		return nil, fmt.Errorf("%w: profile registry, tenant and saved-run executor are required", ErrSavedRunMalformed)
	}
	ordered := append([]SavedRun(nil), fixtures...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].FixtureRef < ordered[j].FixtureRef })
	suite := make(Suite, len(ordered))
	for _, fixture := range ordered {
		if strings.TrimSpace(fixture.FixtureRef) == "" || suite[fixture.FixtureRef] != nil {
			return nil, fmt.Errorf("%w: empty or duplicate fixture reference %q", ErrSavedRunMalformed, fixture.FixtureRef)
		}
		f := fixture
		suite[f.FixtureRef] = func(v version.CompiledVersion) error {
			return ReproduceSavedRunWithProfiles(context.Background(), f, v, profiles, tenant, runner)
		}
	}
	return suite, nil
}

func isSHA256(v string) bool {
	if !strings.HasPrefix(v, "sha256:") || len(v) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(v, "sha256:"))
	return err == nil
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
