package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
)

func TestIssue3745_CommandIsRoutedAndDefaultsToTheStandardDemo(t *testing.T) {
	spec := spec(workSurfaceDemoCommand, nil)
	fields := map[string]string{}
	for _, f := range spec.ConfigFields {
		fields[f.Name] = f.Default
	}
	for _, name := range []string{fieldAgentDemoProfile, fieldDocumentDatabaseURL, fieldProjectDatabaseURL, "database-url"} {
		if _, ok := fields[name]; !ok {
			t.Errorf("work-surface-demo lacks -%s", name)
		}
	}
	if fields[fieldTenant] != "harborcare-demo" {
		t.Fatalf("-tenant default = %q, want harborcare-demo", fields[fieldTenant])
	}
}

func TestIssue3745_RefusesBeforeAnyConnectionOutsideLocalDev_Security(t *testing.T) {
	var out bytes.Buffer
	config := workSurfaceConfigForTest("production", "postgres://u@db.example.com/doc")
	err := runWorkSurfaceDemoCommand(context.Background(), config, &out)
	if err == nil || out.Len() != 0 {
		t.Fatalf("err = %v, output %q, want a refusal and no output", err, out.String())
	}
	if !strings.Contains(err.Error(), "local-dev") {
		t.Fatalf("refusal %q does not name the required profile", err)
	}
}

func workSurfaceConfigForTest(profile, documentURL string) application.LocalWorkSurfaceConfig {
	return application.LocalWorkSurfaceConfig{Profile: profile, Tenant: "harborcare-demo", DatabaseURL: "postgres://u@127.0.0.1/core", DocumentDatabaseURL: documentURL, ProjectDatabaseURL: "postgres://u@127.0.0.1/proj"}
}
