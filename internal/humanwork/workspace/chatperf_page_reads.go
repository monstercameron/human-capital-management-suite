package workspace

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// CHATBUG-014: the workspace document took 0.3 to 0.7 s to leave the server on
// the review machine, and none of it was drawing the page (the document itself
// is built in under 10 ms). Before it wrote a byte the handler read, one after
// the other, the role policy, the workflows the viewer may start, the viewer's
// preferences (twice), the agent snapshot and the page's rollout ledger, then
// the message catalog. Only the role policy has to come first. The others do
// not depend on each other, so they are read together and the preferences are
// read once.
//
// What each step cost is reported on the response as Server-Timing, so the
// next slow page can be read off the browser's network panel instead of
// guessed at.

// pageTiming collects the durations of the steps that produced one document.
type pageTiming struct {
	mu    sync.Mutex
	steps []pageStep
}

type pageStep struct {
	name string
	took time.Duration
}

// measure runs step and records how long it took. It is safe to call from the
// goroutines that read together.
func (t *pageTiming) measure(name string, step func()) {
	started := time.Now()
	step()
	took := time.Since(started)
	t.mu.Lock()
	t.steps = append(t.steps, pageStep{name: name, took: took})
	t.mu.Unlock()
}

// header renders the steps as a Server-Timing value, in the order recorded.
func (t *pageTiming) header() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	parts := make([]string, 0, len(t.steps))
	for _, step := range t.steps {
		parts = append(parts, fmt.Sprintf("%s;dur=%.1f", step.name, float64(step.took.Microseconds())/1000))
	}
	return strings.Join(parts, ", ")
}

// productPageReads is what a workspace document needs from the stores besides
// the viewer's access.
type productPageReads struct {
	starts []WorkflowStartConfig
	agents *AgentsConfig

	revision          productui.PageDefinitionRevision
	governed          bool
	servable          bool
	governanceErr     error
	preferences       preferences.Snapshot
	preferencesLoaded bool
	preferencesErr    error
}

// readProductPage makes the reads that follow the access decision, together.
// Each keeps the failure rule it had when they ran in sequence: a workflow
// catalog or agent snapshot that cannot be read yields an empty one, while a
// ledger or preference failure is returned for the caller to refuse the page.
func (h *Handler) readProductPage(ctx context.Context, principal *trust.Principal, access productAccess, tenant string, page productui.PageID, scope string, timing *pageTiming) productPageReads {
	var reads productPageReads
	var group sync.WaitGroup
	run := func(name string, step func()) {
		group.Add(1)
		go func() {
			defer group.Done()
			timing.measure(name, step)
		}()
	}
	run("starts", func() { reads.starts = h.workflowStartEntries(ctx, principal, access) })
	// Only an Agents page reads the agent snapshot (chatperf2_agents.go).
	run("agents", func() { reads.agents = h.resolveAgentsForPage(ctx, principal, access, page) })
	run("rollout", func() {
		reads.revision, reads.governed, reads.servable, _, reads.governanceErr = h.resolveGovernedRevision(ctx, tenant, page, scope, h.now().Unix())
	})
	if h.preferences != nil && principal != nil {
		run("preferences", func() {
			reads.preferences, reads.preferencesErr = h.preferences.Load(ctx, principal.Tenant(), principal.OrganizationScopeID(), principal.Subject())
			reads.preferencesLoaded = reads.preferencesErr == nil
		})
	}
	group.Wait()
	if reads.preferencesLoaded {
		// The viewer's favourites and recent workflows come from the same
		// preference read the document uses.
		reads.starts = applyWorkflowStartPreferences(reads.starts, reads.preferences.User)
	}
	return reads
}

// workflowStartEntries is the viewer's workflow catalog before their own
// favourites and recent use are applied to it.
func (h *Handler) workflowStartEntries(ctx context.Context, principal *trust.Principal, access productAccess) []WorkflowStartConfig {
	if h.workflowStarts == nil || principal == nil {
		return nil
	}
	entries, err := h.workflowStarts(ctx, principal.Tenant(), workflowStartAuthority(access))
	if err != nil {
		return nil
	}
	return entries
}
