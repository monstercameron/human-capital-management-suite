package openapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
)

// testCell is the minimal application cell the transport composition accepts:
// a trust configuration and an empty discovery document. Every optional port
// is nil, which the composition answers with UNAVAILABLE rather than by
// leaving the surface out, so what is mounted is exactly what production
// mounts.
func testCell(t *testing.T) *app.Cell {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) }
	verifier, err := transporttest.NewVerifier(now)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return &app.Cell{
		Config:    transporttest.Config(verifier, now, "intapi-008", nil),
		Discovery: &manifest.DiscoveryDocument{},
	}
}

// TestRegisteredServicesMatchTheCellGRPCServer proves registeredServices
// against the served composition: the cell's gRPC server registers exactly
// those hcmnext services, and each registered service serves every method
// its descriptor declares.
func TestRegisteredServicesMatchTheCellGRPCServer(t *testing.T) {
	srv, err := cell.NewGRPCServer(testCell(t))
	if err != nil {
		t.Fatalf("NewGRPCServer: %v", err)
	}
	defer srv.Stop()
	info := srv.GetServiceInfo()
	var got []string
	for name := range info {
		if strings.HasPrefix(name, packagePrefix) {
			got = append(got, name)
		}
	}
	sort.Strings(got)
	want := registeredServices()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("cell gRPC server registers %v; registeredServices() = %v", got, want)
	}
	for _, sd := range services(hcmnextFiles(protoregistry.GlobalFiles)) {
		si, ok := info[string(sd.FullName())]
		if !ok {
			continue
		}
		served := map[string]bool{}
		for _, m := range si.Methods {
			served[m.Name] = true
		}
		for _, md := range methods(sd) {
			if !served[string(md.Name())] {
				t.Errorf("%s is declared but not served by the registered service", md.FullName())
			}
		}
	}
}

// mounted reports whether the edge routes path to a handler: an unmounted
// path falls through to http.ServeMux's plain-text 404.
func mounted(t *testing.T, server *httptest.Server, path string) bool {
	t.Helper()
	res, err := server.Client().Post(server.URL+path, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return !(res.StatusCode == http.StatusNotFound && strings.Contains(string(body), "404 page not found"))
}

// TestHTTPExposedProceduresMatchTheCellEdge proves httpExposedProcedures and
// httpAliases against the served composition: of every declared procedure,
// the cell's HTTP edge mounts exactly the listed ones, plus each alias.
func TestHTTPExposedProceduresMatchTheCellEdge(t *testing.T) {
	h, err := cell.NewEdgeHandlerWithDependencies(testCell(t), nil, nil, nil, []byte("intapi-008-cursor-key"), transporthumanwork.WritePorts{})
	if err != nil {
		t.Fatalf("NewEdgeHandlerWithDependencies: %v", err)
	}
	server := httptest.NewServer(h)
	defer server.Close()

	var got []string
	for _, sd := range services(hcmnextFiles(protoregistry.GlobalFiles)) {
		for _, md := range methods(sd) {
			if p := procedurePath(md); mounted(t, server, p) {
				got = append(got, p)
			}
		}
	}
	sort.Strings(got)
	want := httpExposedProcedures()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("cell edge mounts %v; httpExposedProcedures() = %v", got, want)
	}
	if mounted(t, server, "/hcmnext.intents.v1.IntentService/NoSuchMethod") {
		t.Fatal("probe is blind: an undeclared procedure reads as mounted")
	}
	exposed := setOf(want)
	for proc, list := range httpAliases() {
		if !exposed[proc] {
			t.Errorf("alias target %s is not an exposed procedure", proc)
		}
		for _, alias := range list {
			if !mounted(t, server, alias) {
				t.Errorf("alias %s of %s is not mounted", alias, proc)
			}
		}
	}
}

func TestSetOf(t *testing.T) {
	s := setOf([]string{"a", "b"})
	if !s["a"] || !s["b"] || s["c"] || len(s) != 2 {
		t.Fatalf("setOf = %v", s)
	}
}
