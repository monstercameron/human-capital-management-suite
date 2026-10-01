package timeclockkit

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
	"gopkg.in/yaml.v3"
)

const OpenAPIPath = "/v1/time/openapi.yaml"
const maxOpenAPIBody = 1 << 20

// OpenAPI returns the same generated document served by the device transport.
func OpenAPI() ([]byte, error) {
	document, err := generatedOpenAPI()
	if err != nil {
		return nil, err
	}
	if err := CheckOpenAPI(document); err != nil {
		return nil, err
	}
	return document, nil
}

// CheckOpenAPI verifies that all device methods and non-empty example payloads
// are present in a generated or partner-served OpenAPI document.
func CheckOpenAPI(document []byte) error {
	var root yaml.Node
	if err := yaml.Unmarshal(document, &root); err != nil {
		return fmt.Errorf("%w: invalid OpenAPI YAML: %v", ErrConformance, err)
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%w: OpenAPI root is not a mapping", ErrConformance)
	}
	rootMap := root.Content[0]
	paths := mappingValue(rootMap, "paths")
	if paths == nil {
		return fmt.Errorf("%w: paths are missing", ErrConformance)
	}
	for _, method := range []string{"CreateEnrollmentCode", "EnrollDevice", "RotateDeviceKey", "RevokeDevice", "SyncRoster", "IdentifyWorker", "SubmitPunches", "Heartbeat", "GetWorkerStatus"} {
		path := mappingValue(paths, "/v1/time/clock-device/"+method)
		if path == nil || mappingValue(path, "post") == nil {
			return fmt.Errorf("%w: device method %s is missing", ErrConformance, method)
		}
		op := mappingValue(path, "post")
		body := mappingValue(op, "requestBody")
		content := mappingValue(body, "content")
		jsonBody := mappingValue(content, "application/json")
		example := mappingValue(jsonBody, "example")
		if example == nil || example.Kind != yaml.MappingNode || len(example.Content) == 0 {
			return fmt.Errorf("%w: device method %s has no example payload", ErrConformance, method)
		}
	}
	return nil
}

// FetchClockOpenAPI fetches and validates the contract actually served by a
// partner endpoint. It never accepts a caller-provided contract as evidence.
func FetchClockOpenAPI(ctx context.Context, endpoint string) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("%w: invalid endpoint", ErrConformance)
	}
	if !strings.HasSuffix(u.Path, OpenAPIPath) {
		u.Path = strings.TrimRight(u.Path, "/") + OpenAPIPath
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrConformance, err)
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: fetch served contract: %v", ErrConformance, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: contract endpoint returned HTTP %d", ErrConformance, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxOpenAPIBody+1))
	if err != nil || len(body) > maxOpenAPIBody {
		return nil, fmt.Errorf("%w: contract exceeds 1 MiB", ErrConformance)
	}
	if err := CheckOpenAPI(body); err != nil {
		return nil, err
	}
	return body, nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
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

func generatedOpenAPI() ([]byte, error) {
	doc := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title": "HCM Next Clock Partner API", "version": "0.1.0",
			"description": "Generated from the compiled ClockDeviceService descriptors.",
		},
		"security": []any{map[string]any{"bearer": []string{}}},
		"components": map[string]any{
			"securitySchemes": map[string]any{"bearer": map[string]any{"type": "http", "scheme": "bearer"}},
			"schemas":         map[string]any{},
		},
		"paths": map[string]any{},
	}
	paths := doc["paths"].(map[string]any)
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	fd := timev1.File_hcmnext_time_v1_clock_device_service_proto
	if fd == nil {
		return nil, fmt.Errorf("%w: clock descriptor is nil", ErrConformance)
	}
	service := fd.Services().ByName("ClockDeviceService")
	for i := 0; i < service.Methods().Len(); i++ {
		method := service.Methods().Get(i)
		paths["/v1/time/clock-device/"+string(method.Name())] = map[string]any{"post": generatedOperation(service, method, schemas)}
	}
	return yaml.Marshal(doc)
}

func generatedOperation(service protoreflect.ServiceDescriptor, method protoreflect.MethodDescriptor, schemas map[string]any) map[string]any {
	return map[string]any{
		"operationId": string(service.FullName()) + "_" + string(method.Name()),
		"summary":     string(method.Name()),
		"security":    []any{map[string]any{"bearer": []string{}}},
		"requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": generatedSchemaRef(method.Input(), schemas), "example": generatedExample(method.Input())}}},
		"responses": map[string]any{
			"200":     map[string]any{"description": "Success", "content": map[string]any{"application/json": map[string]any{"schema": generatedSchemaRef(method.Output(), schemas)}}},
			"default": map[string]any{"description": "Canonical HTTP error"},
		},
		"x-hcmnext-service": service.FullName(), "x-hcmnext-rpc": method.Name(), "x-hcmnext-max-request-bytes": 1 << 20,
	}
}

func generatedSchemaRef(md protoreflect.MessageDescriptor, schemas map[string]any) map[string]any {
	name := string(md.FullName())
	if _, ok := schemas[name]; !ok {
		properties := map[string]any{}
		schemas[name] = map[string]any{"type": "object", "properties": properties}
		for i := 0; i < md.Fields().Len(); i++ {
			field := md.Fields().Get(i)
			var value map[string]any
			switch {
			case field.Message() != nil:
				value = generatedSchemaRef(field.Message(), schemas)
			case field.Enum() != nil:
				values := make([]string, field.Enum().Values().Len())
				for n := range values {
					values[n] = string(field.Enum().Values().Get(n).Name())
				}
				value = map[string]any{"type": "string", "enum": values}
			default:
				value = generatedScalarSchema(field.Kind())
			}
			if field.IsList() {
				value = map[string]any{"type": "array", "items": value}
			}
			properties[field.JSONName()] = value
		}
	}
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

func generatedScalarSchema(kind protoreflect.Kind) map[string]any {
	switch kind {
	case protoreflect.BoolKind:
		return map[string]any{"type": "boolean"}
	case protoreflect.BytesKind:
		return map[string]any{"type": "string", "format": "byte"}
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return map[string]any{"type": "number"}
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return map[string]any{"type": "integer", "format": "int32"}
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return map[string]any{"type": "integer", "format": "int64"}
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return map[string]any{"type": "integer", "format": "uint32"}
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return map[string]any{"type": "integer", "format": "uint64"}
	default:
		return map[string]any{"type": "string"}
	}
}

func generatedExample(md protoreflect.MessageDescriptor) map[string]any {
	example := map[string]any{}
	for i := 0; i < md.Fields().Len(); i++ {
		example[md.Fields().Get(i).JSONName()] = ""
	}
	return example
}
