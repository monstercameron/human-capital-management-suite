package timeclock

import (
	"fmt"
	"net/http"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
	"gopkg.in/yaml.v3"
)

// OpenAPIPath is the clock partner contract route. The general RPC document
// remains at /openapi.yaml; this path avoids replacing that publication.
const OpenAPIPath = "/v1/time/openapi.yaml"

// OpenAPIDocument renders the clock contract from generated descriptors and
// the HTTP projections actually mounted by the time transport.
func OpenAPIDocument() ([]byte, error) {
	doc := map[string]any{
		"openapi":  "3.1.0",
		"info":     map[string]any{"title": "HCM Next Clock Partner API", "version": "0.1.0", "description": "Generated from the compiled time Protobuf descriptors."},
		"security": []any{map[string]any{"bearer": []any{}}},
		"components": map[string]any{
			"securitySchemes": map[string]any{"bearer": map[string]any{"type": "http", "scheme": "bearer"}},
			"schemas":         map[string]any{"ConnectError": errorSchema(), "HTTPError": httpErrorSchema()},
		},
		"paths": map[string]any{},
	}
	paths := doc["paths"].(map[string]any)
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	files := []protoreflect.FileDescriptor{timev1.File_hcmnext_time_v1_clock_device_service_proto, timev1.File_hcmnext_time_v1_worker_clock_proto, timev1.File_hcmnext_time_v1_missing_punch_proto, timev1.File_hcmnext_time_v1_time_service_proto}
	for _, fd := range files {
		if fd == nil {
			return nil, fmt.Errorf("timeclock openapi: generated descriptor is nil")
		}
		for i := 0; i < fd.Services().Len(); i++ {
			svc := fd.Services().Get(i)
			for j := 0; j < svc.Methods().Len(); j++ {
				method := svc.Methods().Get(j)
				in := schemaRef(method.Input(), schemas)
				out := schemaRef(method.Output(), schemas)
				for _, route := range httpRoutes(string(svc.FullName()), string(method.Name())) {
					verb := "post"
					if string(svc.FullName()) == "hcmnext.time.v1.WorkerClockService" && string(method.Name()) == "GetSelfClock" {
						verb = "get"
					}
					paths[route] = operation(string(svc.FullName()), string(method.Name()), verb, in, out, exampleMessage(method.Input()))
				}
			}
		}
	}
	return yaml.Marshal(doc)
}

// OpenAPIHandler publishes the generated clock contract at OpenAPIPath.
func OpenAPIHandler() (http.Handler, error) {
	document, err := OpenAPIDocument()
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != OpenAPIPath {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=300")
		_, _ = w.Write(document)
	}), nil
}

func httpRoutes(service, method string) []string {
	switch service {
	case "hcmnext.time.v1.ClockDeviceService":
		return []string{"/v1/time/clock-device/" + method}
	case "hcmnext.time.v1.WorkerClockService":
		if method == "GetSelfClock" {
			return []string{"/v1/time/self"}
		}
		return []string{"/v1/time/self/in", "/v1/time/self/out"}
	case "hcmnext.time.v1.MissingPunchService":
		return []string{"/v1/time/missing-punch/" + method}
	case "hcmnext.time.v1.TimeService":
		return []string{"/v1/time/" + method}
	default:
		return nil
	}
}

func operation(service, method, verb string, input, output map[string]any, example any) map[string]any {
	op := map[string]any{
		"operationId": service + "_" + method,
		"summary":     method,
		"security":    []any{map[string]any{"bearer": []string{}}},
		"responses": map[string]any{
			"200":     map[string]any{"description": "Success", "content": map[string]any{"application/json": map[string]any{"schema": output}}},
			"default": map[string]any{"description": "Canonical HTTP error", "content": map[string]any{"application/json": map[string]any{"$ref": "#/components/schemas/HTTPError"}}},
		},
		"x-hcmnext-service":           service,
		"x-hcmnext-rpc":               method,
		"x-hcmnext-auth":              authRequirement(service, method),
		"x-hcmnext-max-request-bytes": 1 << 20,
	}
	if verb == "post" {
		op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": input, "example": example}}}
	}
	return map[string]any{verb: op}
}

func authRequirement(service, method string) string {
	if service == "hcmnext.time.v1.ClockDeviceService" && method == "EnrollDevice" {
		return "enrollment code and device proof; machine bearer credential thereafter"
	}
	if service == "hcmnext.time.v1.WorkerClockService" || service == "hcmnext.time.v1.MissingPunchService" {
		return "authenticated human principal"
	}
	return "authenticated principal"
}

func errorSchema() map[string]any {
	return map[string]any{"type": "object", "required": []string{"code"}, "properties": map[string]any{"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}}}
}

func httpErrorSchema() map[string]any {
	return map[string]any{"type": "object", "required": []string{"code", "message"}, "properties": map[string]any{"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}}}
}

func schemaRef(md protoreflect.MessageDescriptor, schemas map[string]any) map[string]any {
	name := string(md.FullName())
	if _, ok := schemas[name]; !ok {
		if name == "google.protobuf.Timestamp" {
			schemas[name] = map[string]any{"type": "string", "format": "date-time"}
			return map[string]any{"$ref": "#/components/schemas/" + name}
		}
		schemas[name] = map[string]any{"type": "object", "properties": map[string]any{}}
		properties := schemas[name].(map[string]any)["properties"].(map[string]any)
		for i := 0; i < md.Fields().Len(); i++ {
			field := md.Fields().Get(i)
			var value any
			switch {
			case field.Message() != nil:
				value = schemaRef(field.Message(), schemas)
			case field.Enum() != nil:
				values := make([]string, field.Enum().Values().Len())
				for n := range values {
					values[n] = string(field.Enum().Values().Get(n).Name())
				}
				value = map[string]any{"type": "string", "enum": values}
			default:
				value = scalarSchema(field.Kind())
			}
			if field.IsList() {
				value = map[string]any{"type": "array", "items": value}
			}
			properties[field.JSONName()] = value
		}
		for i := 0; i < md.Oneofs().Len(); i++ {
			oneof := md.Oneofs().Get(i)
			if oneof.IsSynthetic() {
				continue
			}
			choices := make([]any, 0, oneof.Fields().Len())
			for j := 0; j < oneof.Fields().Len(); j++ {
				choices = append(choices, map[string]any{"required": []string{oneof.Fields().Get(j).JSONName()}})
			}
			schemas[name].(map[string]any)["oneOf"] = choices
		}
	}
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

func scalarSchema(kind protoreflect.Kind) map[string]any {
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

func exampleMessage(md protoreflect.MessageDescriptor) map[string]any {
	out := map[string]any{}
	for i := 0; i < md.Fields().Len(); i++ {
		out[md.Fields().Get(i).JSONName()] = ""
	}
	return out
}
