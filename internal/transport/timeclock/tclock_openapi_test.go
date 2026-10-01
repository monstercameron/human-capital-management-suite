package timeclock

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
	"gopkg.in/yaml.v3"
)

func TestOpenAPIDocument_DescriptorsAndMountedRoutes(t *testing.T) {
	document, err := OpenAPIDocument()
	if err != nil {
		t.Fatal(err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(document, &root); err != nil {
		t.Fatal(err)
	}
	paths := yamlValue(root.Content[0], "paths")
	if paths == nil {
		t.Fatal("paths missing")
	}
	files := []protoreflect.FileDescriptor{timev1.File_hcmnext_time_v1_clock_device_service_proto, timev1.File_hcmnext_time_v1_worker_clock_proto, timev1.File_hcmnext_time_v1_missing_punch_proto}
	for _, fd := range files {
		for i := 0; i < fd.Services().Len(); i++ {
			svc := fd.Services().Get(i)
			for j := 0; j < svc.Methods().Len(); j++ {
				method := svc.Methods().Get(j)
				for _, route := range httpRoutes(string(svc.FullName()), string(method.Name())) {
					if yamlValue(paths, route) == nil {
						t.Fatalf("descriptor method %s missing route %q", method.Name(), route)
					}
				}
			}
		}
	}
	for _, route := range []string{
		"/v1/time/clock-device/CreateEnrollmentCode",
		"/v1/time/clock-device/GetWorkerStatus",
		"/v1/time/self",
		"/v1/time/self/in",
		"/v1/time/self/out",
		"/v1/time/missing-punch/SubmitCorrection",
		"/v1/time/missing-punch/ReviewCorrection",
	} {
		if yamlValue(paths, route) == nil {
			t.Fatalf("mounted route %q missing", route)
		}
	}
	if yamlValue(paths, "/hcmnext.time.v1.ClockDeviceService/Heartbeat") != nil {
		t.Fatal("unmounted Connect route was published")
	}
	if !strings.Contains(string(document), "hcmnext.time.v1.ClockDeviceService") {
		t.Fatal("service metadata missing")
	}
}

func TestOpenAPIDocument_ProtoJSONSchemasAuthAndLimits(t *testing.T) {
	document, err := OpenAPIDocument()
	if err != nil {
		t.Fatal(err)
	}
	text := string(document)
	for _, want := range []string{"format: date-time", "format: uint64", "oneOf:", "x-hcmnext-auth:", "x-hcmnext-max-request-bytes: 1048576", "MISSING_PUNCH_DECISION_APPROVED"} {
		if !strings.Contains(text, want) {
			t.Fatalf("document missing %q", want)
		}
	}
	if !strings.Contains(text, "enrollment code and device proof") || !strings.Contains(text, "authenticated human principal") {
		t.Fatal("auth requirements missing")
	}
}

func TestOpenAPIHandler_ServesOnlyFixedAuthenticatedSurface(t *testing.T) {
	handler, err := OpenAPIHandler()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, OpenAPIPath, http.StatusOK},
		{http.MethodPost, OpenAPIPath, http.StatusMethodNotAllowed},
		{http.MethodGet, "/openapi.yaml", http.StatusMethodNotAllowed},
	} {
		req, err := http.NewRequest(tc.method, server.URL+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != tc.status {
			t.Errorf("%s %s status=%d want %d", tc.method, tc.path, response.StatusCode, tc.status)
		}
	}
}

func yamlValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
