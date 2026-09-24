package oidckit_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/oidckit"
)

func root(t *testing.T) string {
	_, f, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	r, e := oidckit.FindRepoRoot(f)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func valid() oidckit.AdapterPlan {
	return oidckit.AdapterPlan{TestOnly: true, OIDCVersion: "v1.2.3", OAuth2Version: "v0.30.0", Issuer: "https://id.example", Audience: "client", Algorithms: []string{"RS256", "ES256", "EdDSA"}, RequirePKCE: true, RequireState: true, RequireNonce: true, VerifyIssuer: true, RefreshOnKeyMiss: true, NormalizeClaims: true, NoAuthorizationFromClaims: true}
}

func TestOIDCBackendQualification(t *testing.T) {
	q, e := oidckit.LoadQualification(filepath.Join(root(t), "definitions", "architecture", "oidc-oauth2-qualification.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	if q.Version != 1 || q.Todo != "LIB-010" || q.OIDCModule != oidckit.OIDCModule || q.OAuth2Module != oidckit.OAuth2Module || q.Verdict != "REJECT" || !q.RuntimeDependencyGraphUnchanged {
		t.Fatalf("decision drifted: %#v", q)
	}
	if e := oidckit.ValidateAdapterPlan(valid()); e != nil {
		t.Fatal(e)
	}
	for _, n := range []string{"go.mod", "go.sum"} {
		b, e := os.ReadFile(filepath.Join(root(t), n))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(b), oidckit.OIDCModule) || strings.Contains(string(b), oidckit.OAuth2Module) {
			t.Fatalf("rejected candidate in %s", n)
		}
	}
}

func TestTodo_LIB_010_Golden(t *testing.T) {
	q, e := oidckit.LoadQualification(filepath.Join(root(t), "definitions", "architecture", "oidc-oauth2-qualification.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(q.Requirements.Algorithms, []string{"RS256", "ES256", "EdDSA"}) || q.Requirements.PKCE != "S256 PKCE is mandatory" || q.Requirements.StateNonce == "" || q.Requirements.KeyRefresh == "" {
		t.Fatalf("requirements drifted: %#v", q.Requirements)
	}
}

func TestTodo_LIB_010_Security(t *testing.T) {
	mut := []func(*oidckit.AdapterPlan){func(p *oidckit.AdapterPlan) { p.VerifyIssuer = false }, func(p *oidckit.AdapterPlan) { p.RequirePKCE = false }, func(p *oidckit.AdapterPlan) { p.RequireState = false }, func(p *oidckit.AdapterPlan) { p.RequireNonce = false }, func(p *oidckit.AdapterPlan) { p.RefreshOnKeyMiss = false }, func(p *oidckit.AdapterPlan) { p.NoAuthorizationFromClaims = false }, func(p *oidckit.AdapterPlan) { p.Algorithms = []string{"none"} }}
	for i, m := range mut {
		p := valid()
		m(&p)
		if e := oidckit.ValidateAdapterPlan(p); e == nil {
			t.Fatalf("mutation %d accepted", i)
		}
	}
}

func TestTodo_LIB_010_Integration(t *testing.T) {
	g, e := oidckit.ReleaseGraph(root(t), "./cmd/hcmnext", "./cmd/migrate", "./cmd/projector", "./cmd/worker")
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range []string{oidckit.OIDCModule, oidckit.OAuth2Module} {
		for _, x := range g {
			if x == m || strings.HasPrefix(x, m+"/") {
				t.Fatalf("release graph imports rejected %s", x)
			}
		}
	}
}

func TestTodo_LIB_010_Conformance(t *testing.T) {
	q, e := oidckit.LoadQualification(filepath.Join(root(t), "definitions", "architecture", "oidc-oauth2-qualification.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	want := map[string]bool{"TestOIDCBackendQualification": false, "TestTodo_LIB_010_Conformance": false, "TestTodo_LIB_010_Golden": false, "TestTodo_LIB_010_Integration": false, "TestTodo_LIB_010_Security": false}
	for _, r := range q.Evidence {
		if _, ok := want[r.Test]; !ok {
			t.Errorf("unexpected evidence %q", r.Test)
		} else {
			if want[r.Test] {
				t.Errorf("duplicate evidence %q", r.Test)
			}
			want[r.Test] = true
			if r.Package != "tools/quality/oidckit" {
				t.Errorf("evidence package %q", r.Package)
			}
		}
	}
	for n, v := range want {
		if !v {
			t.Errorf("missing evidence %s", n)
		}
	}
}

func TestOIDCKit_LoadAndAdapterPlanFailures(t *testing.T) {
	dir := t.TempDir()
	if _, err := oidckit.LoadQualification(filepath.Join(dir, "missing.yaml")); err == nil || !strings.Contains(err.Error(), "read qualification") {
		t.Fatalf("missing qualification error = %v", err)
	}
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("version: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := oidckit.LoadQualification(bad); err == nil || !strings.Contains(err.Error(), "parse qualification") {
		t.Fatalf("malformed qualification error = %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*oidckit.AdapterPlan)
	}{
		{"not test only", func(p *oidckit.AdapterPlan) { p.TestOnly = false }},
		{"unconfirmed oidc version", func(p *oidckit.AdapterPlan) { p.OIDCVersion = "v1" }},
		{"unconfirmed oauth version", func(p *oidckit.AdapterPlan) { p.OAuth2Version = "latest" }},
		{"missing issuer", func(p *oidckit.AdapterPlan) { p.Issuer = "" }},
		{"missing audience", func(p *oidckit.AdapterPlan) { p.Audience = "" }},
		{"issuer not verified", func(p *oidckit.AdapterPlan) { p.VerifyIssuer = false }},
		{"missing pkce", func(p *oidckit.AdapterPlan) { p.RequirePKCE = false }},
		{"missing state", func(p *oidckit.AdapterPlan) { p.RequireState = false }},
		{"missing nonce", func(p *oidckit.AdapterPlan) { p.RequireNonce = false }},
		{"missing key refresh", func(p *oidckit.AdapterPlan) { p.RefreshOnKeyMiss = false }},
		{"claims authorize", func(p *oidckit.AdapterPlan) { p.NoAuthorizationFromClaims = false }},
		{"missing normalization", func(p *oidckit.AdapterPlan) { p.NormalizeClaims = false }},
		{"missing algorithms", func(p *oidckit.AdapterPlan) { p.Algorithms = nil }},
		{"unapproved algorithm", func(p *oidckit.AdapterPlan) { p.Algorithms = []string{"none"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := valid()
			tc.mutate(&plan)
			if err := oidckit.ValidateAdapterPlan(plan); err == nil {
				t.Fatal("invalid adapter plan was accepted")
			}
		})
	}
}

func TestOIDCKit_ReleaseGraphAndRepoRootErrors(t *testing.T) {
	if _, err := oidckit.ReleaseGraph(root(t)); err == nil || !strings.Contains(err.Error(), "requires a target") {
		t.Fatalf("empty ReleaseGraph error = %v", err)
	}
	if _, err := oidckit.ReleaseGraph(filepath.Join(t.TempDir(), "missing"), "./cmd/missing"); err == nil || !strings.Contains(err.Error(), "go list") {
		t.Fatalf("invalid ReleaseGraph error = %v", err)
	}
	if _, err := oidckit.FindRepoRoot(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing path found a repository root")
	}
	// Keep this directory outside the checkout. CI and developer lanes may
	// point TEMP inside the repository, which would make an otherwise rootless
	// directory inherit the repository's go.mod and invalidate this assertion.
	rootDir, err := os.MkdirTemp(filepath.Dir(root(t)), "oidckit-rootless-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(rootDir)
	if _, err := oidckit.FindRepoRoot(rootDir); err == nil || !strings.Contains(err.Error(), "repository root not found") {
		t.Fatalf("rootless directory error = %v", err)
	}
	modFile := filepath.Join(rootDir, "go.mod")
	if err := os.WriteFile(modFile, []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := oidckit.FindRepoRoot(rootDir); err != nil || got != rootDir {
		t.Fatalf("FindRepoRoot directory = %q, %v; want %q", got, err, rootDir)
	}
}
