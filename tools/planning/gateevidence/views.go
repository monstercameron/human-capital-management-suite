package gateevidence

import (
	"fmt"
	"strings"
)

// ManifestViews are the release, endpoint, deployment and gate projections
// of one signed P1A manifest (NEXT-002 REFACTOR: "generate release,
// endpoint, deployment and gate views from the same manifest identities").
// Every view carries the manifest's CanonicalDigest as its identity, so a
// consumer holding any one view can prove which signed manifest produced it,
// and no view can be edited independently of the others.
type ManifestViews struct {
	Release    ReleaseView
	Endpoints  []EndpointView
	Deployment DeploymentView
	Gate       GateView
}

// ReleaseView is what a release/commercial registry binds to.
type ReleaseView struct {
	ManifestDigest string
	Release        string
	TodoID         string
	Workflow       string
}

// EndpointView is one executable intent and the capability that serves it.
type EndpointView struct {
	ManifestDigest string
	IntentID       string
	Modes          []string
	CapabilityID   string
	EffectClass    string
}

// DeploymentView is the command and migration closure a deployment admits.
type DeploymentView struct {
	ManifestDigest   string
	CommandPackages  []string
	MigrationDialect string
	MigrationCount   int
	LatestMigration  int64
}

// GateView is what Gate A evidence compilation and effect enforcement bind to.
type GateView struct {
	ManifestDigest    string
	EvidenceTodoIDs   []string
	ForbiddenEffects  []string
	SelectionBindings []SelectionBinding
}

// Views derives every ManifestViews projection from m. It fails when an
// intent has no capability in m.Capabilities, because an endpoint view with
// no serving capability would be scope the manifest never granted.
func (m P1AManifest) Views() (ManifestViews, error) {
	digest, err := m.CanonicalDigest()
	if err != nil {
		return ManifestViews{}, err
	}
	capabilities := make(map[string]Capability, len(m.Capabilities))
	for _, c := range m.Capabilities {
		capabilities[c.ID] = c
	}

	views := ManifestViews{
		Release: ReleaseView{ManifestDigest: digest, Release: m.Release, TodoID: m.TodoID, Workflow: m.Workflow},
		Deployment: DeploymentView{
			ManifestDigest:   digest,
			MigrationDialect: m.Migrations.Dialect,
			MigrationCount:   len(m.Migrations.Files),
		},
		Gate: GateView{
			ManifestDigest:    digest,
			ForbiddenEffects:  append([]string(nil), m.ForbiddenEffects...),
			SelectionBindings: append([]SelectionBinding(nil), m.SelectionBindings...),
		},
	}
	for _, intent := range m.Intents {
		capabilityID := strings.TrimSuffix(intent.ID, "/v1")
		c, ok := capabilities[capabilityID]
		if !ok {
			return ManifestViews{}, fmt.Errorf("intent %s has no serving capability %s in the manifest", intent.ID, capabilityID)
		}
		views.Endpoints = append(views.Endpoints, EndpointView{
			ManifestDigest: digest,
			IntentID:       intent.ID,
			Modes:          append([]string(nil), intent.Modes...),
			CapabilityID:   c.ID,
			EffectClass:    c.EffectClass,
		})
	}
	for _, c := range m.Commands {
		views.Deployment.CommandPackages = append(views.Deployment.CommandPackages, c.Package)
	}
	for _, f := range m.Migrations.Files {
		if f.Version > views.Deployment.LatestMigration {
			views.Deployment.LatestMigration = f.Version
		}
	}
	for _, e := range m.Evidence {
		views.Gate.EvidenceTodoIDs = append(views.Gate.EvidenceTodoIDs, e.TodoID)
	}
	return views, nil
}
