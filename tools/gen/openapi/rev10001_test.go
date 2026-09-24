package openapi

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/reflect/protoregistry"
	"gopkg.in/yaml.v3"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/openapidoc"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
)

// TestTodo_REV_100_01 proves the generated public contract covers every
// compiled RPC and preserves the Protobuf JSON field shape used by clients.
func TestTodo_REV_100_01(t *testing.T) {
	doc, _, err := generatedMap(t)
	if err != nil {
		t.Fatal(err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("OpenAPI version = %v", doc["openapi"])
	}
	securityList, ok := doc["security"].([]any)
	securityRequirement, requirementOK := func() (map[string]any, bool) {
		if !ok || len(securityList) != 1 {
			return nil, false
		}
		m, isMap := securityList[0].(map[string]any)
		return m, isMap
	}()
	if !requirementOK || securityRequirement["bearer"] == nil {
		t.Fatal("the API contract does not require bearer authentication")
	}
	paths := mapAt(t, doc, "paths")
	schemas := mapAt(t, doc, "components", "schemas")
	declared := 0
	for _, sd := range services(hcmnextFiles(protoregistry.GlobalFiles)) {
		for _, md := range methods(sd) {
			declared++
			path := procedurePath(md)
			op, ok := paths[path]
			if !ok {
				t.Errorf("compiled RPC %s is absent from published paths", md.FullName())
				continue
			}
			operation := mapAt(t, op, "post")
			requestRef := mapAt(t, operation, "requestBody", "content", "application/json", "schema")["$ref"]
			responseRef := mapAt(t, operation, "responses", "200", "content", "application/json", "schema")["$ref"]
			if requestRef != schemaRefPrefix+string(md.Input().FullName()) || responseRef != schemaRefPrefix+string(md.Output().FullName()) {
				t.Errorf("%s schema refs = %v / %v", path, requestRef, responseRef)
			}
			if _, ok := schemas[strings.TrimPrefix(requestRef.(string), schemaRefPrefix)]; !ok {
				t.Errorf("%s request schema is unresolved", path)
			}
			if _, ok := schemas[strings.TrimPrefix(responseRef.(string), schemaRefPrefix)]; !ok {
				t.Errorf("%s response schema is unresolved", path)
			}
			if mapAt(t, operation, "responses", "default", "content", "application/json", "schema")["$ref"] != schemaRefPrefix+errorSchemaName {
				t.Errorf("%s has no canonical Connect error response", path)
			}
		}
	}
	if declared == 0 || len(paths) != declared {
		t.Fatalf("published paths = %d, compiled RPCs = %d", len(paths), declared)
	}
	createRequest := mapAt(t, schemas, "hcmnext.intents.v1.CreateIntentRequest", "properties")
	for _, field := range []string{"idempotencyKey", "executionMode", "definition"} {
		if _, ok := createRequest[field]; !ok {
			t.Errorf("CreateIntentRequest omits Protobuf JSON field %q", field)
		}
	}
	if mapAt(t, doc, "components", "securitySchemes", "bearer")["scheme"] != "bearer" {
		t.Fatal("the API contract does not declare bearer authentication")
	}
}

// TestTodo_REV_100_01_Golden proves both the public source artifact and the
// copy embedded in the serving binary match fresh generator output.
func TestTodo_REV_100_01_Golden(t *testing.T) {
	root := repoRoot(t)
	if err := Check(root, DefaultOutputPath); err != nil {
		t.Fatalf("generated OpenAPI drift: %v", err)
	}
	want, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(EmbeddedOutputPath)))
	if err != nil || !bytes.Equal(embedded, want) {
		t.Fatalf("embedded publication differs from generator (read error %v)", err)
	}
}

// TestTodo_REV_100_01_Integration fetches the actual cell publication route,
// checks anonymous access is refused, and verifies an authenticated caller
// receives the exact generated artifact.
func TestTodo_REV_100_01_Integration(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) }
	verifier, err := transporttest.NewVerifier(now)
	if err != nil {
		t.Fatal(err)
	}
	c := &app.Cell{
		Config:    transporttest.Config(verifier, now, "rev-100-01", nil),
		Discovery: &manifest.DiscoveryDocument{},
	}
	h, err := cell.NewEdgeHandlerWithDependencies(c, nil, nil, nil, []byte("rev-100-01-cursor-key"), nil, transporthumanwork.WritePorts{}, nil)
	if err != nil {
		t.Fatalf("compose cell edge: %v", err)
	}
	server := httptest.NewServer(h)
	defer server.Close()

	anonymous, err := server.Client().Get(server.URL + openapidoc.Path)
	if err != nil {
		t.Fatal(err)
	}
	_ = anonymous.Body.Close()
	if anonymous.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous OpenAPI request status = %d", anonymous.StatusCode)
	}

	authorization, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(now()))
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, server.URL+openapidoc.Path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", authorization)
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/yaml") {
		t.Fatalf("OpenAPI response status/content type = %d / %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	got, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(DefaultOutputPath)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("cell did not publish the generated OpenAPI artifact")
	}
}

func generatedMap(t *testing.T) (map[string]any, []byte, error) {
	t.Helper()
	data, err := Generate(repoRoot(t))
	if err != nil {
		return nil, nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, err
	}
	return doc, data, nil
}
