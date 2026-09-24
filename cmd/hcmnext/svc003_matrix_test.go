package main

import (
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

func TestTodo_SVC_003(t *testing.T) {
	s, err := application.SpecFor(application.RoleServe, []string{"-workspace=false"}, application.WithMigrator(nil))
	if err != nil {
		t.Fatal(err)
	}
	if s.Role != bootstrap.RoleHCMNext || s.Validate == nil || s.Build == nil || s.DBPoolFactory == nil {
		t.Fatalf("serve spec is incomplete: role=%q validate=%t build=%t pool=%t", s.Role, s.Validate != nil, s.Build != nil, s.DBPoolFactory != nil)
	}
	if len(s.Args) != 1 || s.Args[0] != "-workspace=false" {
		t.Fatalf("command arguments = %v", s.Args)
	}
}

func TestTodo_SVC_003_Golden(t *testing.T) {
	fields := application.ServeConfigFields()
	want := []string{"database-url", "dev-hmac-key", "tenant", "grpc-listen", "http-listen", "workspace"}
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		seen[f.Name] = true
	}
	for _, name := range want {
		if !seen[name] {
			t.Errorf("serve config lost golden field %q", name)
		}
	}
	for _, secret := range []string{"dev-hmac-key"} {
		found := false
		for _, f := range fields {
			if f.Name == secret && f.Secret {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is missing its secret redaction marker", secret)
		}
	}
}

func TestTodo_SVC_003_Integration(t *testing.T) {
	// Exercise the same application composition call used by `serve`, and
	// confirm an invalid request fails during config validation before a pool
	// factory or listener can run.
	s, err := application.SpecFor(application.RoleServe, []string{"-database-url=postgres://fake/db", "-dev-hmac-key=too-short"}, application.WithMigrator(nil))
	if err != nil {
		t.Fatal(err)
	}
	v, err := bootstrap.ParseConfig(s.Args, func(string) (string, bool) { return "", false }, s.ConfigFields)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(v); err == nil || !strings.Contains(err.Error(), "32") {
		t.Fatalf("short signing key validation = %v, want minimum length refusal", err)
	}
}

func TestTodo_SVC_003_Mutation(t *testing.T) {
	s, err := application.SpecFor(application.RoleServe, []string{"-workspace=false"}, application.WithMigrator(nil))
	if err != nil {
		t.Fatal(err)
	}
	fields := append([]bootstrap.Field(nil), s.ConfigFields...)
	for i := range fields {
		if fields[i].Name == "dev-hmac-key" {
			fields[i].Secret = false
		}
	}
	for _, f := range s.ConfigFields {
		if f.Name == "dev-hmac-key" && !f.Secret {
			t.Fatal("mutating a caller's config copy changed the shared serve spec")
		}
	}
}

func TestTodo_SVC_003_Security(t *testing.T) {
	s, err := application.SpecFor(application.RoleServe, nil, application.WithMigrator(nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"-dev-hmac-key=short"}} {
		v, err := bootstrap.ParseConfig(args, func(string) (string, bool) { return "", false }, s.ConfigFields)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(v); err == nil {
			t.Errorf("serve accepted unsafe key config %v", args)
		}
	}
}

func TestTodo_SVC_003_Race(t *testing.T) {
	const n = 16
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := application.SpecFor(application.RoleServe, []string{"-workspace=false"}, application.WithMigrator(nil))
			if err != nil {
				t.Error(err)
				return
			}
			if s.Role != bootstrap.RoleHCMNext || s.Build == nil {
				t.Errorf("concurrent serve composition incomplete: %+v", s)
			}
		}()
	}
	wg.Wait()
}
