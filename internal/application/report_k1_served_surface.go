package application

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/reporting"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/reportrender"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/reportschedule"
)

// ServedReportingSurface exposes the report definition, execution, rendering,
// export and dashboard scheduling contracts through the application boundary
// used by hcmnext serve. The experience packages remain the semantic owners;
// this value only makes their operations reachable from a composed process.
type ServedReportingSurface struct {
	NewDefinitionRegistry func() *reporting.Registry
	PublishDefinition     func(*reporting.Registry, reporting.Definition) (reporting.Definition, error)
	PublishDashboard      func(*reporting.Registry, reporting.Dashboard) (reporting.Dashboard, error)

	NewRenderRegistry       func() *reportrender.Registry
	PublishRenderDefinition func(*reportrender.Registry, reportrender.ReportDefinition) (reportrender.ReportDefinition, error)
	ExecuteReport           func(reportrender.ExecutionRequest) (reportrender.Result, error)
	RenderReport            func(reportrender.Result, string) (reportrender.Rendered, error)
	ExportReport            func(reportrender.Result, string) (reportrender.Rendered, error)

	NewScheduleStore   func(int) *reportschedule.Store
	CreateSchedule     func(*reportschedule.Store, reportschedule.Schedule) (reportschedule.Schedule, error)
	RunScheduledReport func(*reportschedule.Store, string, reportschedule.Authorization, time.Time, string, string, string) (reportschedule.Run, error)
	History            func(*reportschedule.Store, string, string, reportschedule.Authorization) ([]reportschedule.Run, error)
	Reproduce          func(reportschedule.Run, reportschedule.Definition, reportschedule.Data, reportschedule.Control, reportschedule.Locale, string) reportschedule.Reproduction
}

// NewServedReportingSurface returns reporting capabilities available through
// the serving application. It creates no process-wide or tenant-wide state;
// callers supply each registry, schedule store and authorization explicitly.
func NewServedReportingSurface() ServedReportingSurface {
	return ServedReportingSurface{
		NewDefinitionRegistry:   reporting.NewRegistry,
		PublishDefinition:       (*reporting.Registry).Publish,
		PublishDashboard:        (*reporting.Registry).PublishDashboard,
		NewRenderRegistry:       reportrender.NewRegistry,
		PublishRenderDefinition: (*reportrender.Registry).Publish,
		ExecuteReport:           reportrender.Execute,
		RenderReport:            reportrender.Result.Render,
		ExportReport:            reportrender.Result.Export,
		NewScheduleStore:        reportschedule.NewStore,
		CreateSchedule:          (*reportschedule.Store).Create,
		RunScheduledReport:      (*reportschedule.Store).Run,
		History:                 (*reportschedule.Store).History,
		Reproduce:               reportschedule.Reproduce,
	}
}

// Reporting returns the report capabilities exposed by a composed
// application. A nil application deliberately returns no live capabilities.
func (a *App) Reporting() ServedReportingSurface {
	if a == nil {
		return ServedReportingSurface{}
	}
	return NewServedReportingSurface()
}
