package openapi

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoregistry"
	"gopkg.in/yaml.v3"
)

// repoRoot walks up from the package directory to the directory holding
// go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the package directory")
		}
		dir = parent
	}
}

// parsed generates the document and parses it back as generic YAML.
func parsed(t *testing.T) (map[string]any, []byte) {
	t.Helper()
	data, err := Generate(repoRoot(t))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("generated document is not valid YAML: %v", err)
	}
	return doc, data
}

func mapAt(t *testing.T, v any, keys ...string) map[string]any {
	t.Helper()
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("%s: not a mapping (%T)", k, v)
		}
		v = m[k]
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%v: not a mapping (%T)", keys, v)
	}
	return m
}

// collectRefs gathers every $ref value in the tree.
func collectRefs(v any, out *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if s, ok := val.(string); ok && k == "$ref" {
				*out = append(*out, s)
			}
			collectRefs(val, out)
		}
	case []any:
		for _, e := range t {
			collectRefs(e, out)
		}
	}
}

// TestTodo_INTAPI_008 is the primary INTAPI-008 test: the generator produces
// a valid OpenAPI 3.1.0 document with one POST operation per declared RPC,
// correct flags and kinds, every $ref resolving, and byte-identical output
// across runs.
func TestTodo_INTAPI_008(t *testing.T) {
	doc, data := parsed(t)
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi = %v, want 3.1.0", doc["openapi"])
	}
	info := mapAt(t, doc, "info")
	if info["title"] != "HCM Next RPC API" || info["version"] != DocumentVersion ||
		!strings.Contains(info["description"].(string), "Do not edit") ||
		!strings.Contains(info["description"].(string), "INTAPI-001") {
		t.Fatalf("info = %v", info)
	}
	bearer := mapAt(t, doc, "components", "securitySchemes", "bearer")
	if bearer["type"] != "http" || bearer["scheme"] != "bearer" {
		t.Fatalf("bearer scheme = %v", bearer)
	}

	paths := mapAt(t, doc, "paths")
	schemas := mapAt(t, doc, "components", "schemas")
	exposed := setOf(httpExposedProcedures())
	registered := setOf(registeredServices())
	opIDs := map[string]bool{}
	declared := 0
	for _, sd := range services(hcmnextFiles(protoregistry.GlobalFiles)) {
		for _, md := range methods(sd) {
			declared++
			path := procedurePath(md)
			item, ok := paths[path]
			if !ok {
				t.Errorf("declared RPC %s has no path %s", md.FullName(), path)
				continue
			}
			op := mapAt(t, item, "post")
			id := op["operationId"].(string)
			if id != string(sd.Name())+"_"+string(md.Name()) || opIDs[id] {
				t.Errorf("%s operationId = %q (duplicate=%v)", path, id, opIDs[id])
			}
			opIDs[id] = true
			if op["x-hcmnext-service"] != string(sd.FullName()) {
				t.Errorf("%s x-hcmnext-service = %v", path, op["x-hcmnext-service"])
			}
			if op["x-hcmnext-registered"] != registered[string(sd.FullName())] {
				t.Errorf("%s x-hcmnext-registered = %v", path, op["x-hcmnext-registered"])
			}
			if op["x-hcmnext-http-exposed"] != exposed[path] {
				t.Errorf("%s x-hcmnext-http-exposed = %v", path, op["x-hcmnext-http-exposed"])
			}
			in := mapAt(t, op, "requestBody", "content", "application/json", "schema")["$ref"]
			out := mapAt(t, op, "responses", "200", "content", "application/json", "schema")["$ref"]
			if in != schemaRefPrefix+string(md.Input().FullName()) || out != schemaRefPrefix+string(md.Output().FullName()) {
				t.Errorf("%s request/response refs = %v / %v", path, in, out)
			}
			if mapAt(t, op, "responses", "default", "content", "application/json", "schema")["$ref"] != schemaRefPrefix+errorSchemaName {
				t.Errorf("%s default response is not the Connect error", path)
			}
			if streamingKind(md) != "" {
				if op["x-hcmnext-streaming"] != streamingKind(md) || op["x-hcmnext-rpc-kind"] != "STREAM" ||
					!strings.Contains(op["description"].(string), "gRPC / gRPC-Web only") {
					t.Errorf("streaming RPC %s is not marked gRPC-only: %v", path, op)
				}
			} else if _, marked := op["x-hcmnext-streaming"]; marked {
				t.Errorf("unary RPC %s is marked streaming", path)
			}
		}
	}
	if declared == 0 || len(paths) != declared {
		t.Fatalf("%d paths for %d declared RPCs", len(paths), declared)
	}
	watch := mapAt(t, paths, "/hcmnext.journey.v1.JourneyService/WatchJourney", "post")
	if watch["x-hcmnext-streaming"] != "server" || watch["x-hcmnext-http-exposed"] != false {
		t.Fatalf("WatchJourney = %v", watch)
	}
	if mapAt(t, paths, "/hcmnext.intents.v1.IntentService/CreateIntent", "post")["x-hcmnext-rpc-kind"] != "CREATE" ||
		mapAt(t, paths, "/hcmnext.dataops.v1.DataOpsService/ExplainFieldHistory", "post")["x-hcmnext-registered"] != false {
		t.Fatal("CreateIntent kind or DataOps registration flag is wrong")
	}

	// Every $ref resolves; the error detail is hcmnext.common.v1.ErrorDetail.
	var refs []string
	collectRefs(doc, &refs)
	params := mapAt(t, doc, "components", "parameters")
	for _, r := range refs {
		switch {
		case strings.HasPrefix(r, schemaRefPrefix):
			if _, ok := schemas[strings.TrimPrefix(r, schemaRefPrefix)]; !ok {
				t.Errorf("unresolved schema ref %s", r)
			}
		case strings.HasPrefix(r, "#/components/parameters/"):
			if _, ok := params[strings.TrimPrefix(r, "#/components/parameters/")]; !ok {
				t.Errorf("unresolved parameter ref %s", r)
			}
		default:
			t.Errorf("unexpected ref %s", r)
		}
	}
	debug := mapAt(t, schemas, errorDetailSchemaName, "properties", "debug")
	if debug["$ref"] != schemaRefPrefix+commonErrorDetail {
		t.Fatalf("error detail debug = %v", debug)
	}
	codes := mapAt(t, schemas, errorSchemaName, "properties", "code")["enum"].([]any)
	if len(codes) != 16 {
		t.Fatalf("connect codes = %v", codes)
	}

	// Every component is a complete schema (none left reserved and empty).
	for name, v := range schemas {
		if m, ok := v.(map[string]any); !ok || len(m) == 0 {
			t.Errorf("schema %s is empty", name)
		}
	}

	again, err := Generate(repoRoot(t))
	if err != nil || !bytes.Equal(data, again) {
		t.Fatalf("generation is not deterministic (err=%v)", err)
	}
}

// TestTodo_INTAPI_008_Golden fails when the checked-in document differs from
// a fresh generation.
func TestTodo_INTAPI_008_Golden(t *testing.T) {
	if err := Check(repoRoot(t), DefaultOutputPath); err != nil {
		if errors.Is(err, ErrDrift) {
			t.Fatalf("%v\nrun from the repository root: %s", err, RegenerateCommand)
		}
		t.Fatal(err)
	}
}
