package project

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
)

// A new ProjectService RPC must be mounted on HTTP as well as gRPC. The
// descriptor is the source of truth, so this test grows with the wire API.
func TestProjectConnectRoutesCoverDescriptor(t *testing.T) {
	service := projectv1.File_hcmnext_project_v1_project_service_proto.Services().ByName("ProjectService")
	if service == nil || service.Methods().Len() == 0 {
		t.Fatal("ProjectService descriptor is empty")
	}
	handler := NewConnectHandler(Dependencies{})
	for i := 0; i < service.Methods().Len(); i++ {
		method := service.Methods().Get(i)
		path := "/" + string(service.FullName()) + "/" + string(method.Name())
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code == http.StatusNotFound {
			t.Errorf("ProjectService method %s is absent from Connect routes", path)
		}
	}
}
