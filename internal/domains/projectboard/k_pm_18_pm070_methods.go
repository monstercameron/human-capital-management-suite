package projectboard

import (
	"errors"
	"strings"
)

type BoardMethod string

const (
	MethodTaskBoard BoardMethod = "TASK_BOARD"
	MethodKanban    BoardMethod = "KANBAN"
	MethodScrum     BoardMethod = "SCRUM"
	MethodScrumban  BoardMethod = "SCRUMBAN"
	MethodFeature   BoardMethod = "FEATURE"
	MethodBug       BoardMethod = "BUG"
	MethodDiscovery BoardMethod = "DISCOVERY"
	MethodMilestone BoardMethod = "MILESTONE"
	MethodRelease   BoardMethod = "RELEASE"
	MethodSupport   BoardMethod = "SUPPORT_OPERATIONS"
	MethodProgram   BoardMethod = "PROGRAM_PORTFOLIO"
)

var (
	ErrUnknownBoardMethod     = errors.New("projectboard: unknown board method")
	ErrUnsupportedBoardMethod = errors.New("projectboard: board method is not enabled")
)

// MethodSemantics records behavior, not marketing labels. A method is served
// only when every required behavior is present in the common project model.
type MethodSemantics struct {
	Columns               bool
	TaskAssignments       bool
	ContinuousFlow        bool
	WIPLimits             bool
	CycleTimeMetrics      bool
	Backlog               bool
	CycleScope            bool
	CycleDates            bool
	Carryover             bool
	CycleReports          bool
	TaskTypesAndFields    bool
	StatusMapping         bool
	Milestones            bool
	ReleaseObject         bool
	VerifiedScope         bool
	DeploymentIntegration bool
	SupportIntake         bool
	QueueOwnership        bool
	SLAPolicy             bool
	CrossProjectHierarchy bool
	PortfolioRollups      bool
}

type BoardMethodSpec struct {
	Method       BoardMethod
	Label        string
	Supported    bool
	Semantics    MethodSemantics
	RequiredText string
}

type MethodSurface string

const (
	SurfaceBrowser MethodSurface = "BROWSER"
	SurfaceRPC     MethodSurface = "RPC"
	SurfaceHTTP    MethodSurface = "HTTP"
)

type MethodPresentation struct {
	Requested BoardMethod
	Effective BoardMethod
	Label     string
	Supported bool
	Surface   MethodSurface
	Notice    string
}

func BoardMethodCatalog() []BoardMethodSpec {
	return []BoardMethodSpec{
		{Method: MethodTaskBoard, Label: "Task board", Supported: true, Semantics: MethodSemantics{Columns: true, TaskAssignments: true}},
		{Method: MethodKanban, Label: "Kanban", Supported: true, Semantics: MethodSemantics{Columns: true, TaskAssignments: true, ContinuousFlow: true, WIPLimits: true, CycleTimeMetrics: true}},
		{Method: MethodScrum, Label: "Scrum", Supported: true, Semantics: MethodSemantics{Backlog: true, CycleScope: true, CycleDates: true, Carryover: true, CycleReports: true}},
		{Method: MethodScrumban, Label: "Scrumban", Supported: true, Semantics: MethodSemantics{Columns: true, ContinuousFlow: true, WIPLimits: true, CycleScope: true, CycleDates: true, Carryover: true, CycleReports: true}},
		{Method: MethodFeature, Label: "Feature", Supported: true, Semantics: MethodSemantics{TaskTypesAndFields: true, StatusMapping: true}},
		{Method: MethodBug, Label: "Bug", Supported: true, Semantics: MethodSemantics{TaskTypesAndFields: true, StatusMapping: true}},
		{Method: MethodDiscovery, Label: "Discovery", Supported: true, Semantics: MethodSemantics{TaskTypesAndFields: true, StatusMapping: true}},
		{Method: MethodMilestone, Label: "Milestone view", Supported: true, Semantics: MethodSemantics{TaskTypesAndFields: true, StatusMapping: true, Milestones: true}},
		{Method: MethodRelease, Label: "Release", RequiredText: "release object, verified scope, and deployment integration"},
		{Method: MethodSupport, Label: "Support/operations", RequiredText: "intake, queue ownership, and SLA policy"},
		{Method: MethodProgram, Label: "Program/portfolio", RequiredText: "cross-project hierarchy and portfolio rollups"},
	}
}

func BoardMethodSpecFor(method BoardMethod) (BoardMethodSpec, error) {
	for _, spec := range BoardMethodCatalog() {
		if spec.Method == method {
			return spec, nil
		}
	}
	return BoardMethodSpec{}, ErrUnknownBoardMethod
}

func ValidateBoardMethod(method BoardMethod) error {
	spec, err := BoardMethodSpecFor(method)
	if err != nil {
		return err
	}
	if !spec.Supported {
		return ErrUnsupportedBoardMethod
	}
	if method == MethodFeature || method == MethodBug || method == MethodDiscovery {
		kind := map[BoardMethod]PresetKind{MethodFeature: PresetFeature, MethodBug: PresetBug, MethodDiscovery: PresetDiscovery}[method]
		if _, err := Preset(kind); err != nil {
			return err
		}
	}
	return nil
}

// PresentBoardMethod is shared by browser, RPC, and HTTP projections. An
// unsupported marketing label is relabeled as the honest common task board;
// callers never receive an enabled-looking Release or Support board.
func PresentBoardMethod(method BoardMethod, surface MethodSurface) (MethodPresentation, error) {
	if strings.TrimSpace(string(method)) == "" || (surface != SurfaceBrowser && surface != SurfaceRPC && surface != SurfaceHTTP) {
		return MethodPresentation{}, ErrUnknownBoardMethod
	}
	spec, err := BoardMethodSpecFor(method)
	if err != nil {
		return MethodPresentation{}, err
	}
	if spec.Supported {
		return MethodPresentation{Requested: method, Effective: method, Label: spec.Label, Supported: true, Surface: surface}, nil
	}
	return MethodPresentation{Requested: method, Effective: MethodTaskBoard, Label: "Task board", Surface: surface, Notice: "This method is not enabled; the project is shown as a task board."}, nil
}
