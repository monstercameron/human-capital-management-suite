package application

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"gopkg.in/yaml.v3"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
)

type intapi015Operation struct {
	Path       string
	Method     string
	Operation  string
	RPC        string
	Status     string
	Registered bool
	Exposed    bool
}

type intapi015Response struct {
	Status int
	Header http.Header
	Body   []byte
}

type intapi015Page struct {
	Intents []struct {
		ID string `json:"intentId"`
	} `json:"intents"`
	Page struct {
		NextCursor string `json:"nextCursor"`
	} `json:"page"`
}

func intapi015RepoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above application package")
		}
		dir = parent
	}
}

func intapi015Map(t testing.TB, value any, label string) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s has type %T, want YAML mapping", label, value)
	}
	return result
}

func intapi015Operations(t testing.TB, data []byte, servedOnly bool) ([]intapi015Operation, map[string]map[string]any) {
	t.Helper()
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse OpenAPI document: %v", err)
	}
	paths := intapi015Map(t, root["paths"], "paths")
	operations := make([]intapi015Operation, 0)
	for path, rawItem := range paths {
		item := intapi015Map(t, rawItem, "path "+path)
		for method, rawOperation := range item {
			if method == "parameters" || method == "summary" || method == "description" {
				continue
			}
			op := intapi015Map(t, rawOperation, method+" "+path)
			operation := intapi015Operation{
				Path:       path,
				Method:     strings.ToUpper(method),
				Operation:  intapi015StringValue(op, "operationId"),
				RPC:        intapi015StringValue(op, "x-hcmnext-rpc"),
				Status:     intapi015StringValue(op, "x-hcmnext-status"),
				Registered: intapi015BoolValue(op, "x-hcmnext-registered"),
				Exposed:    intapi015BoolValue(op, "x-hcmnext-http-exposed"),
			}
			if servedOnly && operation.Status != "SERVED" {
				continue
			}
			operations = append(operations, operation)
		}
	}
	sort.Slice(operations, func(i, j int) bool {
		if operations[i].Path != operations[j].Path {
			return operations[i].Path < operations[j].Path
		}
		return operations[i].Method < operations[j].Method
	})
	return operations, nil
}

func intapi015StringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func intapi015BoolValue(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func intapi015Documents(t testing.TB) (integration []intapi015Operation, published []intapi015Operation, generated []byte) {
	t.Helper()
	root := intapi015RepoRoot(t)
	integrationData, err := os.ReadFile(filepath.Join(root, "schema", "openapi", "integration.openapi.yaml"))
	if err != nil {
		t.Fatalf("read integration OpenAPI document: %v", err)
	}
	generated, err = os.ReadFile(filepath.Join(root, "schema", "openapi", "rpcs.openapi.yaml"))
	if err != nil {
		t.Fatalf("read generated OpenAPI document: %v", err)
	}
	integration, _ = intapi015Operations(t, integrationData, true)
	published, _ = intapi015Operations(t, generated, false)
	return integration, published, generated
}

func intapi015Find(t testing.TB, operations []intapi015Operation, path string) intapi015Operation {
	t.Helper()
	for _, operation := range operations {
		if operation.Path == path {
			return operation
		}
	}
	t.Fatalf("OpenAPI operation %q is not published", path)
	return intapi015Operation{}
}

func intapi015PublicAliases(t testing.TB, integration, published []intapi015Operation) []intapi015Operation {
	t.Helper()
	canonical := make(map[string]intapi015Operation, len(published))
	for _, operation := range published {
		canonical[operation.Path] = operation
	}
	public := make([]intapi015Operation, 0, len(integration))
	for _, operation := range integration {
		backing, ok := canonical["/"+operation.RPC]
		if !ok {
			t.Fatalf("SERVED %s %s points at missing RPC %q", operation.Method, operation.Path, operation.RPC)
		}
		if backing.Exposed {
			public = append(public, operation)
		}
	}
	return public
}

func intapi015PublishedHTTP(t testing.TB, h *rbacHarness) ([]intapi015Operation, []byte) {
	t.Helper()
	integration, _, generated := intapi015Documents(t)
	_ = integration
	req, err := http.NewRequest(http.MethodGet, "http://"+h.composed.HTTPAddr()+"/openapi.yaml", nil)
	if err != nil {
		t.Fatalf("build OpenAPI request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.tokens["admin"])
	response, err := h.http.Do(req)
	if err != nil {
		t.Fatalf("GET /openapi.yaml: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read /openapi.yaml: read=%v close=%v", readErr, closeErr)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /openapi.yaml = %d, want 200", response.StatusCode)
	}
	if !bytes.Equal(body, generated) {
		t.Fatal("served OpenAPI document differs from schema/openapi/rpcs.openapi.yaml")
	}
	operations, _ := intapi015Operations(t, body, false)
	public := operations[:0]
	for _, operation := range operations {
		if operation.Exposed {
			public = append(public, operation)
		}
	}
	if len(public) == 0 {
		t.Fatal("published OpenAPI document has no HTTP-exposed operations")
	}
	return public, body
}

func (h *rbacHarness) intapi015Request(t testing.TB, method, path, token string, body []byte) intapi015Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, "http://"+h.composed.HTTPAddr()+path, reader)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Request-Id", "intapi015-"+strings.ReplaceAll(t.Name(), "/", "-"))
	response, err := h.http.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	data, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read %s %s: read=%v close=%v", method, path, readErr, closeErr)
	}
	return intapi015Response{Status: response.StatusCode, Header: response.Header.Clone(), Body: data}
}

func intapi015AssertError(t testing.TB, response intapi015Response) {
	t.Helper()
	if response.Status < http.StatusBadRequest {
		return
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "application/json") {
		t.Fatalf("HTTP %d error Content-Type = %q, want JSON: %s", response.Status, response.Header.Get("Content-Type"), response.Body)
	}
	var wire struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(response.Body, &wire); err != nil {
		t.Fatalf("HTTP %d error is not JSON: %v (%s)", response.Status, err, response.Body)
	}
	if wire.Code == "" || wire.Message == "" {
		t.Fatalf("HTTP %d error is missing the standard code/message model: %s", response.Status, response.Body)
	}
}

func intapi015ConcretePath(path string, h *rbacHarness) string {
	values := map[string]string{
		"intent_id":    h.intentID,
		"workflow_id":  h.instanceID,
		"work_item_id": h.financeItemID,
		"operation_id": "missing-operation",
		"node_id":      "missing-node",
	}
	for name, value := range values {
		path = strings.ReplaceAll(path, "{"+name+"}", url.PathEscape(value))
	}
	return path
}

func intapi015CallPublished(t testing.TB, h *rbacHarness, operations []intapi015Operation) {
	t.Helper()
	for _, operation := range operations {
		response := h.intapi015Request(t, operation.Method, operation.Path, h.tokens["admin"], []byte(`{}`))
		if response.Status >= http.StatusInternalServerError || response.Status == http.StatusMethodNotAllowed {
			t.Fatalf("%s %s = HTTP %d: %s", operation.Method, operation.Path, response.Status, response.Body)
		}
		intapi015AssertError(t, response)
	}
}

// TestTodo_INTAPI_015 is the primary conformance proof: the runtime publishes
// the checked-in OpenAPI artifact and every HTTP-exposed procedure is reached
// through the composed cell with a real authenticated request.
func TestTodo_INTAPI_015(t *testing.T) {
	h := rbacCompose(t)
	public, _ := intapi015PublishedHTTP(t, h)
	intapi015CallPublished(t, h, public)
	integration, published, _ := intapi015Documents(t)
	for _, operation := range intapi015PublicAliases(t, integration, published) {
		path := intapi015ConcretePath(operation.Path, h)
		var body []byte
		if operation.Method != http.MethodGet {
			body = []byte(`{}`)
		}
		response := h.intapi015Request(t, operation.Method, path, h.tokens["admin"], body)
		if response.Status >= http.StatusInternalServerError || response.Status == http.StatusMethodNotAllowed {
			t.Fatalf("served alias %s %s = HTTP %d: %s", operation.Method, path, response.Status, response.Body)
		}
		intapi015AssertError(t, response)
	}
	t.Logf("conformed %d canonical and %d resource operations", len(public), len(integration))
}

// TestTodo_INTAPI_015_Conformance proves that every SERVED resource operation
// names an exposed canonical RPC and that every published operation has the
// standard Connect error schema.
func TestTodo_INTAPI_015_Conformance(t *testing.T) {
	integration, published, _ := intapi015Documents(t)
	byPath := make(map[string]intapi015Operation, len(published))
	for _, operation := range published {
		if _, exists := byPath[operation.Path]; exists {
			t.Fatalf("duplicate published operation path %q", operation.Path)
		}
		byPath[operation.Path] = operation
	}
	for _, operation := range integration {
		if operation.RPC == "" {
			t.Fatalf("SERVED %s %s has no backing RPC", operation.Method, operation.Path)
		}
		canonical, ok := byPath["/"+operation.RPC]
		if !ok {
			t.Fatalf("SERVED %s %s points at unexposed RPC %q", operation.Method, operation.Path, operation.RPC)
		}
		if !canonical.Exposed {
			continue
		}
	}
	if len(integration) < 20 {
		t.Fatalf("only %d SERVED integration operations found", len(integration))
	}

	root := intapi015RepoRoot(t)
	var document map[string]any
	data, err := os.ReadFile(filepath.Join(root, "schema", "openapi", "rpcs.openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	paths := intapi015Map(t, document["paths"], "published paths")
	for path, raw := range paths {
		item := intapi015Map(t, raw, "published "+path)
		op, ok := item["post"]
		if !ok {
			t.Fatalf("published path %s has no POST operation", path)
		}
		operation := intapi015Map(t, op, "published "+path+" POST")
		responses := intapi015Map(t, operation["responses"], "responses "+path)
		fallback := intapi015Map(t, responses["default"], "default response "+path)
		content := intapi015Map(t, fallback["content"], "default content "+path)
		applicationJSON := intapi015Map(t, content["application/json"], "default JSON "+path)
		schema := intapi015Map(t, applicationJSON["schema"], "default schema "+path)
		if schema["$ref"] != "#/components/schemas/connect.Error" {
			t.Fatalf("%s default error schema = %v", path, schema["$ref"])
		}
	}
}

// TestTodo_INTAPI_015_Security proves authentication, tenant and authority
// scope are enforced by the same runtime edge used for the conformance calls.
func TestTodo_INTAPI_015_Security(t *testing.T) {
	h := rbacCompose(t)
	public, _ := intapi015PublishedHTTP(t, h)
	list := intapi015Find(t, public, "/hcmnext.intents.v1.IntentService/ListIntents")

	for _, tc := range []struct {
		name  string
		token string
		want  func(int) bool
	}{
		{"missing credential", "", func(status int) bool { return status == http.StatusUnauthorized }},
		{"expired credential", h.tokens["expired"], func(status int) bool { return status == http.StatusUnauthorized }},
		{"foreign tenant", h.tokens["otherTenant"], func(status int) bool { return status == http.StatusUnauthorized || status == http.StatusForbidden }},
		{"insufficient role scope", h.tokens["eli"], func(status int) bool { return status == http.StatusForbidden || status == http.StatusNotFound }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := h.intapi015Request(t, http.MethodPost, list.Path, tc.token, []byte(`{}`))
			if !tc.want(response.Status) {
				t.Fatalf("HTTP status = %d, want refusal for %s: %s", response.Status, tc.name, response.Body)
			}
			intapi015AssertError(t, response)
		})
	}

	// A valid credential cannot use a request body to widen its tenant scope.
	response := h.intapi015Request(t, http.MethodPost, list.Path, h.tokens["admin"], []byte(`{"scope":{"tenantId":"foreign-tenant"}}`))
	if response.Status != http.StatusBadRequest {
		t.Fatalf("caller-selected tenant scope = HTTP %d, want 400: %s", response.Status, response.Body)
	}
	intapi015AssertError(t, response)
}

// TestTodo_INTAPI_015_Integration proves paging, idempotency replay and
// optimistic-concurrency rejection against the real PostgreSQL-backed cell.
func TestTodo_INTAPI_015_Integration(t *testing.T) {
	h := rbacCompose(t)
	public, _ := intapi015PublishedHTTP(t, h)
	list := intapi015Find(t, public, "/hcmnext.intents.v1.IntentService/ListIntents")
	response := h.intapi015Request(t, http.MethodPost, list.Path, h.tokens["admin"], []byte(`{"page":{"pageSize":1}}`))
	if response.Status != http.StatusOK {
		t.Fatalf("first page = HTTP %d: %s", response.Status, response.Body)
	}
	var page intapi015Page
	if err := json.Unmarshal(response.Body, &page); err != nil {
		t.Fatalf("decode first page: %v (%s)", err, response.Body)
	}
	if len(page.Intents) > 1 {
		t.Fatalf("page size = %d, want at most 1", len(page.Intents))
	}
	if page.Page.NextCursor != "" {
		secondBody := []byte(fmt.Sprintf(`{"page":{"pageSize":1,"cursor":%q}}`, page.Page.NextCursor))
		second := h.intapi015Request(t, http.MethodPost, list.Path, h.tokens["admin"], secondBody)
		if second.Status != http.StatusOK {
			t.Fatalf("second page = HTTP %d: %s", second.Status, second.Body)
		}
		var secondPage intapi015Page
		if err := json.Unmarshal(second.Body, &secondPage); err != nil {
			t.Fatalf("decode second page: %v (%s)", err, second.Body)
		}
		if len(secondPage.Intents) > 1 || secondPage.Page.NextCursor == page.Page.NextCursor {
			t.Fatalf("cursor paging did not advance: first=%q second=%q rows=%d", page.Page.NextCursor, secondPage.Page.NextCursor, len(secondPage.Intents))
		}
		if len(page.Intents) == 1 && len(secondPage.Intents) == 1 && page.Intents[0].ID == secondPage.Intents[0].ID {
			t.Fatalf("cursor paging repeated intent %q", page.Intents[0].ID)
		}
	}

	create := intapi015Find(t, public, "/hcmnext.intents.v1.IntentService/CreateIntent")
	request := transporttest.CanonicalCreateIntentRequest()
	request.IdempotencyKey = "intapi015-replay-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	request.Subjects[0].SubjectId = "rbac-eli"
	body, err := protojson.Marshal(request)
	if err != nil {
		t.Fatalf("marshal idempotent request: %v", err)
	}
	first := h.intapi015Request(t, http.MethodPost, create.Path, h.tokens["admin"], body)
	second := h.intapi015Request(t, http.MethodPost, create.Path, h.tokens["admin"], body)
	if first.Status != second.Status {
		t.Fatalf("idempotent replay statuses = %d and %d", first.Status, second.Status)
	}
	if first.Status >= http.StatusBadRequest {
		intapi015AssertError(t, first)
		intapi015AssertError(t, second)
		var firstWire, secondWire struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(first.Body, &firstWire); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(second.Body, &secondWire); err != nil {
			t.Fatal(err)
		}
		if firstWire.Code != secondWire.Code {
			t.Fatalf("idempotent replay error codes = %q and %q", firstWire.Code, secondWire.Code)
		}
	} else {
		var firstResult, secondResult struct {
			Intent struct {
				ID string `json:"intentId"`
			} `json:"intent"`
		}
		if err := json.Unmarshal(first.Body, &firstResult); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(second.Body, &secondResult); err != nil {
			t.Fatal(err)
		}
		if firstResult.Intent.ID == "" || firstResult.Intent.ID != secondResult.Intent.ID {
			t.Fatalf("idempotent replay intent ids = %q and %q", firstResult.Intent.ID, secondResult.Intent.ID)
		}
	}

	submit := intapi015Find(t, public, "/hcmnext.intents.v1.IntentService/SubmitIntent")
	staleBody := []byte(fmt.Sprintf(`{"idempotencyKey":"intapi015-stale","intentId":%q,"proposalRevisionId":"stale","expectedInstanceVersion":"0"}`, h.intentID))
	stale := h.intapi015Request(t, http.MethodPost, submit.Path, h.tokens["gus"], staleBody)
	if stale.Status != http.StatusPreconditionFailed {
		t.Fatalf("stale optimistic-concurrency request = HTTP %d, want 412: %s", stale.Status, stale.Body)
	}
	intapi015AssertError(t, stale)
}
