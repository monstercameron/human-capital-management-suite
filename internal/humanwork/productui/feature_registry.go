package productui

import "sort"

// FeatureID is a stable identifier local to one page. It is stored together
// with PageID, so labels, translations and page routes may evolve without
// changing authorization identity.
type FeatureID string

const (
	FeatureContent FeatureID = "content"
	FeatureActions FeatureID = "actions"
)

// FeatureDefinition declares one securable page feature and the operations it
// supports. A role grant can only narrow this ceiling; it cannot turn a
// read-only feature into a mutating capability.
type FeatureDefinition struct {
	ID          FeatureID
	Label       string
	Description string
	View        bool
	Create      bool
	Update      bool
	Delete      bool
}

// PageFeatureRegistration is one catalog entry with its containing page ID.
// It is the transport-neutral shape used by composition roots and persistence
// bootstrap code, so callers never need to recover authority identity from a
// label, route, or component type.
type PageFeatureRegistration struct {
	Page    PageID
	Feature FeatureDefinition
}

// specializedPageFeatures names the independently useful regions and actions
// on the currently admitted product pages. Content and Actions are added to
// every page automatically, which keeps adding a safe new page to one registry
// entry while still allowing deliberate finer-grained policy where useful.
func feature(id FeatureID, label, description string, view, create, update, remove bool) FeatureDefinition {
	return FeatureDefinition{ID: id, Label: label, Description: description, View: view, Create: create, Update: update, Delete: remove}
}

func pageFeatureDefinitions(specialized ...FeatureDefinition) []FeatureDefinition {
	features := []FeatureDefinition{
		feature(FeatureContent, "Page content", "Read the page's authorized content", true, false, false, false),
		feature(FeatureActions, "Page actions", "Use the page's supported actions", true, true, true, true),
	}
	features = append(features, specialized...)
	return features
}

// PageFeatureProvider is the optional, stateless feature-catalogue facet of a
// page renderer. The registry snapshots its answer into the immutable module;
// no page-ID map or mutable runtime registration is involved.
type PageFeatureProvider interface {
	PageFeatures() []FeatureDefinition
}

func (homePageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("attention", "Attention", "Work that currently needs the viewer", true, false, false, false),
		feature("drafts", "Drafts", "Resumable work owned by the viewer", true, true, true, true),
		feature("tracked_requests", "Tracked requests", "Requests the viewer is following", true, false, false, false),
		feature("recent_people", "Recent people", "Recently opened authorized worker records", true, false, true, true),
		feature("quick_actions", "Quick actions", "Role-appropriate action shortcuts", true, true, false, false),
	}
}

func (myselfPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("employment_profile", "Employment profile", "The viewer's employment facts", true, false, false, false),
		feature("payroll", "Payroll", "The viewer's payroll summary", true, false, false, false),
		feature("organization_tree", "Organization tree", "Authorized organization context", true, false, false, false),
		feature("workflow_actions", "Workflow actions", "Governed self-service workflow starts", true, true, false, false),
	}
}

func (journeysPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("journey_list", "Journey list", "Authorized workflow instances", true, false, false, false),
		feature("promotion_request", "Promotion request", "Create and manage promotion requests", true, true, true, false),
		feature("journey_detail", "Journey detail", "Track one workflow instance", true, false, true, false),
		feature("approval_decision", "Approval decision", "Complete assigned workflow decisions", true, false, true, false),
	}
}

func (workPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("assigned_queue", "Assigned queue", "Work assigned to the viewer", true, false, false, false),
		feature("approval_decision", "Approval decision", "Complete an assigned decision", true, false, true, false),
	}
}

func (historyPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("workflow_history", "Workflow history", "Search and inspect completed workflow records", true, false, false, false),
		feature("filters", "History filters", "Save and apply history views", true, true, true, true),
	}
}

func (peoplePageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("directory", "People directory", "Search, inspect, and create authorized worker records", true, true, false, false),
		feature("filters", "Directory filters", "Save and apply workforce directory views", true, true, true, true),
		feature("workflow_actions", "Worker workflows", "Start governed workflows for one worker", true, true, false, false),
	}
}

func (personPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("employment_profile", "Employment profile", "Authorized employment facts", true, false, false, false),
		feature("private_data", "Private data", "PII and restricted worker facts", true, false, false, false),
		feature("organization", "Organization", "Worker organization context", true, false, false, false),
		feature("workflow_actions", "Worker workflows", "Start governed workflows for this worker", true, true, false, false),
		feature("history", "Worker history", "Workflow history for this worker", true, false, false, false),
	}
}

func (organizationPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("business_metadata", "Business metadata", "Organization identity and metadata", true, false, true, false),
		feature("workforce_directory", "Workforce directory", "Authorized workers grouped by organization", true, false, false, false),
		feature("organization_tree", "Organization tree", "Authorized ownership and reporting tree", true, false, false, false),
	}
}

func (insightsPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("operational_metrics", "Operational metrics", "Authorized workforce and workflow measures", true, false, false, false),
		feature("metric_explanations", "Metric explanations", "Definitions, scope and freshness for measures", true, false, false, false),
	}
}

func (rolesPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("role_catalog", "Role catalog", "Create and maintain roles", true, true, true, true),
		feature("role_assignments", "Role assignments", "Assign roles to workers", true, true, true, true),
		feature("page_access", "Page access", "Configure role access to pages", true, true, true, true),
		feature("feature_access", "Feature access", "Configure role access to page features", true, true, true, true),
	}
}

func (appearancePageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("theme_editor", "Theme editor", "Configure organization theme tokens", true, true, true, true),
		feature("brand_assets", "Brand assets", "Configure logos and glyphs", true, true, true, true),
		feature("preview", "Theme preview", "Preview the current organization theme", true, false, false, false),
	}
}

func (workflowDesignerPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("workflow_catalog", "Workflow catalog", "Browse authorized published workflows", true, false, false, false),
		feature("workflow_viewer", "Workflow viewer", "Inspect a published definition and authorized live run", true, false, false, false),
		feature("workflow_drafts", "Workflow drafts", "Create and edit governed workflow drafts", true, true, true, true),
		feature("workflow_publication", "Workflow publication", "Submit and approve workflow releases", true, true, true, false),
	}
}

func (settingsPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("locale", "Locale", "Configure personal locale", true, false, true, false),
		feature("accessibility", "Accessibility", "Configure personal accessibility preferences", true, false, true, false),
		feature("personal_preferences", "Personal preferences", "Configure personal workspace behavior", true, true, true, true),
	}
}

// FeatureDefinitions returns the complete immutable feature catalog in stable
// page/feature order. Callers receive copies and cannot mutate the registry.
func FeatureDefinitions() map[PageID][]FeatureDefinition {
	result := make(map[PageID][]FeatureDefinition, len(registeredPages()))
	for _, page := range registeredPages() {
		result[page.ID] = append([]FeatureDefinition(nil), page.Features...)
	}
	return result
}

func FeatureDefinitionsForPage(page PageID) []FeatureDefinition {
	definition, ok := LookupPage(page)
	if !ok {
		return nil
	}
	return append([]FeatureDefinition(nil), definition.Features...)
}

// FlattenFeatureDefinitions converts the catalog to a stable list for server
// composition and durable permission bootstrap.
func FlattenFeatureDefinitions() []PageFeatureRegistration {
	pages := PageDefinitions()
	sort.Slice(pages, func(i, j int) bool { return pages[i].ID < pages[j].ID })
	var result []PageFeatureRegistration
	for _, page := range pages {
		for _, definition := range page.Features {
			result = append(result, PageFeatureRegistration{Page: page.ID, Feature: definition})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Page == result[j].Page {
			return result[i].Feature.ID < result[j].Feature.ID
		}
		return result[i].Page < result[j].Page
	})
	return result
}
