package execution

import (
	"fmt"
	goruntime "runtime"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/hireexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// FixtureHireCompile proves a published New employee hire version is exactly
// the plan this build compiles.
const FixtureHireCompile = "fixture:workflow.new-hire/reproducible-compile@v1"

// PublishReferenceVersions publishes the workflows that are proven through the
// driver but not served: today New employee hire (WF-HIRE-001). It is separate
// from [PublishShippedVersions] on purpose. A shipped version is one the
// served composition can run, and nothing serves a hire yet, so a standard
// deployment never publishes it; the local-development bootstrap does, so an
// author can open and read it beside Promotion. Publication is idempotent and
// approves and activates nothing.
func PublishReferenceVersions(store version.Store, at time.Time) ([]version.CompiledVersion, error) {
	plan, err := hireexec.Compile()
	if err != nil {
		return nil, fmt.Errorf("platform execution: compile the new-hire workflow: %w", err)
	}
	published, err := version.Publish(store, hireexec.Definition(), plan, hireexec.CompileOptions(), version.PublishMeta{
		SemanticVersion: hireexec.SemanticVersion, PublishedAt: at, PublishedBy: versionPublisher,
		ToolVersions: map[string]string{"go": goruntime.Version(), "publisher": "internal/platform/execution"},
		FixtureRefs:  []string{FixtureHireCompile},
	})
	if err != nil {
		return nil, fmt.Errorf("platform execution: publish the new-hire workflow: %w", err)
	}
	return []version.CompiledVersion{published}, nil
}

func hireCompileFixture(v version.CompiledVersion) error {
	return reproducesPlan(v, func() (*workflow.CompiledWorkflow, error) { return hireexec.Compile() })
}
