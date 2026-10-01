package timeclock

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// TemplatePeriodTimecard names the shared period timecard. It is not a
// timeprofile.Template: no profile resolves to it directly; punch, duration
// and exception-only profiles deliver through it.
const TemplatePeriodTimecard = "time.period_timecard"

var (
	// ErrUnknownTemplate means a template id names no time plan.
	ErrUnknownTemplate = errors.New("time workflow: unknown template")
	// ErrNoPeriodPlan means the profile's template delivers its own time and
	// never runs through the period timecard (contractor, agency).
	ErrNoPeriodPlan = errors.New("time workflow: profile does not deliver through the period timecard")
)

// Plan is one time plan's identity: the template it serves, the workflow it
// compiles to and the intent type a start must carry.
type Plan struct {
	TemplateID string
	WorkflowID string
	IntentType string
	Definition func(Params) workflow.Definition
}

// Plans lists every time plan, the five profile templates followed by the
// period timecard.
func Plans() []Plan {
	return []Plan{
		{string(timeprofile.TemplatePunchSession), PunchWorkflowID, IntentRecordPunch, PunchSessionDefinition},
		{string(timeprofile.TemplateDurationSheet), DurationWorkflowID, IntentSubmitTimesheet, DurationTimesheetDefinition},
		{string(timeprofile.TemplateExceptionOnly), ExceptionWorkflowID, IntentReportExceptions, ExceptionPeriodDefinition},
		{string(timeprofile.TemplateContractorTime), ContractorWorkflowID, IntentContractorTime, ContractorInvoiceDefinition},
		{string(timeprofile.TemplateAgencyTime), AgencyWorkflowID, IntentAgencyTimesheet, AgencyVMSDefinition},
		{TemplatePeriodTimecard, PeriodWorkflowID, IntentSubmitTimecard, PeriodTimecardDefinition},
	}
}

// PlanFor returns the plan serving one template id.
func PlanFor(templateID string) (Plan, error) {
	for _, p := range Plans() {
		if p.TemplateID == templateID {
			return p, nil
		}
	}
	return Plan{}, fmt.Errorf("%w: %q", ErrUnknownTemplate, templateID)
}

// CompileOptions is shared by preview compilation and governed publication.
// Time plans compile under P1B: they use APPROVAL, TASK, WAIT and SIGNAL but
// no structural primitive, because PARALLEL, JOIN and SUBWORKFLOW do not run
// in EXECUTE yet (WF-EXT-019).
func CompileOptions() workflow.Options {
	return workflow.Options{Phase: workflow.PhaseP1B, Capabilities: Capabilities()}
}

// CompileDefinition compiles any time definition, including a tenant overlay
// of one, against the time capability manifests.
func CompileDefinition(def workflow.Definition) (*workflow.CompiledWorkflow, error) {
	return workflow.Compile(def, CompileOptions())
}

// Compile compiles one template with the given parameters.
func Compile(templateID string, p Params) (*workflow.CompiledWorkflow, error) {
	plan, err := PlanFor(templateID)
	if err != nil {
		return nil, err
	}
	return CompileDefinition(plan.Definition(p))
}

// ResolvePlan selects and compiles the one plan a profile's captured time
// runs through (WTIME-002 engine half) and proves the compiled plan against
// the profile's constraints (WTIME-006). It is a function of the profile, not
// of any worker-type branch in a template: timeprofile.TemplateFor decides
// the template and CheckCompiled refuses a plan the profile may not run.
func ResolvePlan(profile timeprofile.TimeProfile) (string, *workflow.CompiledWorkflow, error) {
	return resolveWith(profile, DefaultParams())
}

func resolveWith(profile timeprofile.TimeProfile, p Params) (string, *workflow.CompiledWorkflow, error) {
	tmpl, err := timeprofile.TemplateFor(profile)
	if err != nil {
		return "", nil, err
	}
	plan, err := Compile(string(tmpl), p)
	if err != nil {
		return "", nil, err
	}
	if err := CheckCompiled(profile, plan); err != nil {
		return "", nil, err
	}
	return string(tmpl), plan, nil
}

// DeliversThroughPeriod reports whether a template's captured time is folded
// and delivered by the period timecard.
func DeliversThroughPeriod(templateID string) bool {
	switch timeprofile.Template(templateID) {
	case timeprofile.TemplatePunchSession, timeprofile.TemplateDurationSheet, timeprofile.TemplateExceptionOnly:
		return true
	default:
		return false
	}
}

// ResolvePeriodPlan compiles the period timecard for a profile whose capture
// template delivers through it. The constraint check runs over the capture
// plan and the period plan together, because a required decision may live in
// either and a forbidden class may not appear in either.
func ResolvePeriodPlan(profile timeprofile.TimeProfile) (*workflow.CompiledWorkflow, error) {
	tmpl, err := timeprofile.TemplateFor(profile)
	if err != nil {
		return nil, err
	}
	if !DeliversThroughPeriod(string(tmpl)) {
		return nil, fmt.Errorf("%w: template %s", ErrNoPeriodPlan, tmpl)
	}
	capture, err := Compile(string(tmpl), DefaultParams())
	if err != nil {
		return nil, err
	}
	period, err := Compile(TemplatePeriodTimecard, DefaultParams())
	if err != nil {
		return nil, err
	}
	if err := CheckCompiled(profile, capture, period); err != nil {
		return nil, err
	}
	return period, nil
}
