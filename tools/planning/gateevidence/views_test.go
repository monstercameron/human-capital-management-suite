package gateevidence

import (
	"strings"
	"testing"
)

func TestManifestViewsShareOneIdentityAndCoverEveryIntent(t *testing.T) {
	m := mustLoadP1AManifest(t)
	digest, err := m.CanonicalDigest()
	if err != nil {
		t.Fatal(err)
	}
	views, err := m.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	identities := []string{views.Release.ManifestDigest, views.Deployment.ManifestDigest, views.Gate.ManifestDigest}
	for _, e := range views.Endpoints {
		identities = append(identities, e.ManifestDigest)
	}
	for i, id := range identities {
		if id != digest {
			t.Errorf("view identity[%d] = %s, want the manifest digest %s", i, id, digest)
		}
	}
	if views.Release.Release != "P1A" || views.Release.Workflow != m.Workflow || views.Release.TodoID != "NEXT-002" {
		t.Errorf("release view = %+v", views.Release)
	}
	if len(views.Endpoints) != len(m.Intents) {
		t.Fatalf("got %d endpoint views, want %d", len(views.Endpoints), len(m.Intents))
	}
	for i, e := range views.Endpoints {
		if e.IntentID != m.Intents[i].ID || e.CapabilityID+"/v1" != e.IntentID || e.EffectClass != "READ_ONLY" {
			t.Errorf("endpoint view[%d] = %+v", i, e)
		}
	}
	if views.Deployment.MigrationCount != len(m.Migrations.Files) || views.Deployment.LatestMigration != m.Migrations.Files[len(m.Migrations.Files)-1].Version {
		t.Errorf("deployment view migration closure = %d/%d", views.Deployment.MigrationCount, views.Deployment.LatestMigration)
	}
	if strings.Join(views.Deployment.CommandPackages, ",") != "cmd/hcmnext,cmd/worker,cmd/projector,cmd/migrate" {
		t.Errorf("deployment commands = %v", views.Deployment.CommandPackages)
	}
	if !equalStrings(views.Gate.ForbiddenEffects, P1AForbiddenEffects) || len(views.Gate.SelectionBindings) != len(RequiredSelectionBindingTodoIDs) || len(views.Gate.EvidenceTodoIDs) != len(m.Evidence) {
		t.Errorf("gate view = %+v", views.Gate)
	}

	// Editing one field of the manifest moves every view's identity together.
	m.Workflow = "promotion.other/v1"
	moved, err := m.Views()
	if err != nil {
		t.Fatal(err)
	}
	if moved.Gate.ManifestDigest == digest || moved.Endpoints[0].ManifestDigest != moved.Release.ManifestDigest {
		t.Fatal("views did not move to the edited manifest's identity together")
	}
}

func TestManifestViewsRefuseAnIntentWithoutAServingCapability(t *testing.T) {
	m := mustLoadP1AManifest(t)
	m.Intents = append(append([]Intent(nil), m.Intents...), Intent{Order: 9, ID: "hcmnext.people.terminate_worker/v1", Disposition: "INCLUDED"})
	if _, err := m.Views(); err == nil || !strings.Contains(err.Error(), "terminate_worker") {
		t.Fatalf("Views() error = %v, want refusal naming terminate_worker", err)
	}
}

func TestValidBindingPath(t *testing.T) {
	for path, want := range map[string]bool{
		"definitions/planning/gates/a.yaml": true,
		"":                                  false,
		"/abs/a.yaml":                       false,
		"C:/a.yaml":                         false,
		`definitions\a.yaml`:                false,
		"definitions/../a.yaml":             false,
		"./a.yaml":                          false,
		"a//b.yaml":                         false,
	} {
		if got := ValidBindingPath(path); got != want {
			t.Errorf("ValidBindingPath(%q) = %v, want %v", path, got, want)
		}
	}
}
