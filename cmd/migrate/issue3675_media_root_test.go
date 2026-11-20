package main

import (
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
)

// Issue 3675: migrate chat seed wrote the demo media where serve was not looking
// for it, because the seed ignored the environment variables serve reads.
func TestIssue3675_SeedMediaRootAgreesWithServe(t *testing.T) {
	mediaEnv, artifactEnv := t.TempDir(), t.TempDir()
	explicit := filepath.Join(t.TempDir(), "explicit")
	cases := []struct {
		name, flag, media, artifact string
	}{
		{"nothing set", "", "", ""},
		{"media root variable", "", mediaEnv, ""},
		{"artifact root variable", "", "", artifactEnv},
		{"both variables", "", mediaEnv, artifactEnv},
		{"explicit flag", explicit, mediaEnv, artifactEnv},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(application.EnvChatMediaRoot, c.media)
			t.Setenv(application.EnvArtifactRoot, c.artifact)
			serve := application.DefaultChatMediaArtifactRoot(c.flag, "")
			if c.flag == "" {
				serve = application.DefaultChatMediaArtifactRoot(c.media, c.artifact)
			}
			if got := chatSeedMediaRoot(c.flag); got != serve {
				t.Fatalf("seed writes demo media to %q but serve reads %q", got, serve)
			}
		})
	}
}

func TestIssue3675_SeedMediaRootFlagReadsTheServeVariable(t *testing.T) {
	for _, field := range migrateConfigFields() {
		if field.Name != fieldChatSeedMediaRoot {
			continue
		}
		if field.Env != application.EnvChatMediaRoot {
			t.Errorf("-%s reads %q, serve reads %q", field.Name, field.Env, application.EnvChatMediaRoot)
		}
		if field.Default != "" {
			t.Errorf("-%s defaults to %q, which hides the artifact-root fallback serve applies", field.Name, field.Default)
		}
		return
	}
	t.Fatalf("no -%s flag", fieldChatSeedMediaRoot)
}

func TestIssue3675_SeedReportsWhereItWroteMedia(t *testing.T) {
	root := filepath.Join(t.TempDir(), "media")
	line := chatSeedMediaReport(root)
	abs, _ := filepath.Abs(root)
	if line == "" || !containsPath(line, abs) {
		t.Fatalf("report %q does not name the media root %q", line, abs)
	}
}

func containsPath(text, path string) bool {
	for i := 0; i+len(path) <= len(text); i++ {
		if text[i:i+len(path)] == path {
			return true
		}
	}
	return false
}
