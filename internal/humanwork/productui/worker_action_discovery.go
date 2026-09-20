// Package productui discovers the actions launchable for a
// worker record. Discovery binds every registered person
// workflow to the worker — launch hrefs resolve against the
// worker ID, static hrefs pass through — and narrows the set
// with the launcher's exact query rule: the trimmed,
// case-folded query matches against the workflow name,
// category, and description. The registry is never mutated;
// an empty registry discovers an empty, non-nil action list.
package productui

import "strings"

// WorkerAction is one workflow launchable for a worker. Href
// is already bound to the worker: the workflow's launch href
// for the worker ID, or the workflow's static href when it
// carries no launcher. It is presentation metadata, not
// action authority.
type WorkerAction struct {
	ID          string
	Name        string
	Category    string
	Description string
	Href        string
}

// DiscoverWorkerActions resolves the actions launchable for
// one worker from a workflow registry and an optional query.
// A blank query discovers every registered workflow. Workflows
// marked WorkforceChange are withheld from a terminated worker,
// whose employment has ended; every other worker keeps them.
func DiscoverWorkerActions(registry []PersonWorkflow, person Person, query string) []WorkerAction {
	actions := make([]WorkerAction, 0, len(registry))
	narrow := strings.ToLower(strings.TrimSpace(query))
	terminated := ParseLifecycleStatus(person.LifecycleStatus) == LifecycleTerminated
	for _, workflow := range registry {
		if terminated && workflow.WorkforceChange {
			continue
		}
		if narrow != "" {
			searchable := workflow.Name + " " + workflow.Category + " " + workflow.Description
			if !strings.Contains(strings.ToLower(searchable), narrow) {
				continue
			}
		}
		href := workflow.Href
		if workflow.LaunchHref != nil {
			href = workflow.LaunchHref(person.ID)
		}
		actions = append(actions, WorkerAction{
			ID: workflow.ID, Name: workflow.Name, Category: workflow.Category,
			Description: workflow.Description, Href: href,
		})
	}
	return actions
}
