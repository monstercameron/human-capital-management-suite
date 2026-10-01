package edge

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTodo_INTAPI_007(t *testing.T) {
	rules := resourceAliasRules()
	if len(rules) == 0 {
		t.Fatal("no resource aliases derived from the endpoint manifest")
	}
	var gotPath, gotMethod string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.WriteHeader(http.StatusNoContent)
	})
	resourceAliasMiddleware(next, rules).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/intents", nil))
	if gotPath != "/hcmnext.intents.v1.IntentService/ListIntents" || gotMethod != http.MethodPost {
		t.Fatalf("GET alias rewrite = %s %s", gotMethod, gotPath)
	}
}

func TestTodo_INTAPI_007_Integration(t *testing.T) {
	var body string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/intents/demo-1?cursor=c1", nil)
	resourceAliasMiddleware(next, []resourceAliasRule{{method: http.MethodGet, template: "/v1/intents/{intent}", procedure: "/rpc/GetIntent"}}).ServeHTTP(httptest.NewRecorder(), req)
	if body != `{"cursor":"c1","intent":"demo-1"}` && body != `{"intent":"demo-1","cursor":"c1"}` {
		t.Fatalf("alias request body = %s", body)
	}
}

func TestTodo_INTAPI_007_Golden(t *testing.T) {
	params, ok := matchResourcePath("/v1/projects/{project}/tasks/{task}", "/v1/projects/p%2F1/tasks/t-2")
	if !ok || params["project"] != "p/1" || params["task"] != "t-2" {
		t.Fatalf("resource path match = %#v, %v", params, ok)
	}
	if toJSONFieldName("intent_definition_id") != "intentDefinitionId" {
		t.Fatalf("field name conversion = %q", toJSONFieldName("intent_definition_id"))
	}
	if _, err := io.ReadAll(strings.NewReader("ok")); err != nil {
		t.Fatal(err)
	}
}
