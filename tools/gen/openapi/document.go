package openapi

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

const (
	// DocumentVersion is info.version of the generated document.
	DocumentVersion = "0.1.0"
	// DefaultOutputPath is where the document is written, relative to the
	// repository root.
	DefaultOutputPath = "schema/openapi/rpcs.openapi.yaml"
	// RegenerateCommand regenerates the document from the repository root.
	RegenerateCommand = "go run ./tools/gen/openapi/cmd/openapigen"
	// protoSourceDir holds the .proto sources, relative to the repository
	// root.
	protoSourceDir = "schema/proto"

	errorSchemaName       = "connect.Error"
	errorDetailSchemaName = "connect.ErrorDetail"
	commonErrorDetail     = "hcmnext.common.v1.ErrorDetail"
)

// connectCodes are the Connect protocol error codes.
func connectCodes() []any {
	return []any{
		"canceled", "unknown", "invalid_argument", "deadline_exceeded", "not_found",
		"already_exists", "permission_denied", "resource_exhausted", "failed_precondition",
		"aborted", "out_of_range", "unimplemented", "internal", "unavailable", "data_loss",
		"unauthenticated",
	}
}

// Generate renders the OpenAPI document for every hcmnext service linked into
// this binary. repoRoot locates schema/proto, whose leading comments supply
// summaries and descriptions.
func Generate(repoRoot string) ([]byte, error) {
	doc, err := buildDocument(protoregistry.GlobalFiles, filepath.Join(repoRoot, protoSourceDir))
	if err != nil {
		return nil, err
	}
	return encodeYAML(doc)
}

// buildDocument assembles the document tree.
func buildDocument(registry *protoregistry.Files, protoRoot string) (omap, error) {
	files := hcmnextFiles(registry)
	if len(files) == 0 {
		return nil, fmt.Errorf("openapi: no %s* descriptors are linked", packagePrefix)
	}
	paths := make([]string, len(files))
	for i, fd := range files {
		paths[i] = fd.Path()
	}
	notes, err := readComments(protoRoot, paths)
	if err != nil {
		return nil, fmt.Errorf("openapi: read proto comments: %w", err)
	}

	registered := setOf(registeredServices())
	exposed := setOf(httpExposedProcedures())
	aliases := httpAliases()
	sb := newSchemaBuilder()

	var tags []any
	pathItems := map[string]omap{}
	for _, sd := range services(files) {
		tag := omap{{"name", string(sd.Name())}}
		if c := notes[string(sd.FullName())]; c != "" {
			tag.set("description", c)
		}
		tag.set("x-hcmnext-service", string(sd.FullName()))
		tag.set("x-hcmnext-registered", registered[string(sd.FullName())])
		tags = append(tags, tag)
		for _, md := range methods(sd) {
			path := procedurePath(md)
			op := operation(sb, md, notes[string(md.FullName())], registered[string(sd.FullName())], exposed[path], aliases[path])
			pathItems[path] = omap{{"post", op}}
		}
	}
	// The Connect error details carry hcmnext.common.v1.ErrorDetail.
	errDetail, err := registry.FindDescriptorByName(commonErrorDetail)
	if err != nil {
		return nil, fmt.Errorf("openapi: %s: %w", commonErrorDetail, err)
	}
	sb.ref(errDetail)
	sb.drain()

	pathKeys := make([]string, 0, len(pathItems))
	for p := range pathItems {
		pathKeys = append(pathKeys, p)
	}
	sort.Strings(pathKeys)
	pathsObj := omap{}
	for _, p := range pathKeys {
		pathsObj.set(p, pathItems[p])
	}

	schemas := map[string]omap{}
	for _, n := range sb.names() {
		schemas[n] = sb.schemas[n]
	}
	schemas[errorSchemaName] = errorSchema()
	schemas[errorDetailSchemaName] = errorDetailSchema()
	schemaKeys := make([]string, 0, len(schemas))
	for n := range schemas {
		schemaKeys = append(schemaKeys, n)
	}
	sort.Strings(schemaKeys)
	schemasObj := omap{}
	for _, n := range schemaKeys {
		schemasObj.set(n, schemas[n])
	}

	return omap{
		{"openapi", "3.1.0"},
		{"info", omap{
			{"title", "HCM Next RPC API"},
			{"version", DocumentVersion},
			{"description", infoDescription()},
		}},
		{"servers", []any{omap{{"url", "/"}, {"description", "The cell's Connect edge, relative to the serving origin."}}}},
		{"security", []any{omap{{"bearer", []any{}}}}},
		{"tags", tags},
		{"paths", pathsObj},
		{"components", omap{
			{"securitySchemes", omap{{"bearer", omap{
				{"type", "http"},
				{"scheme", "bearer"},
				{"description", "Bearer token in the Authorization header, verified by the cell's trust verifier. Client-credentials OAuth2 for machine clients is planned under INTAPI-001."},
			}}}},
			{"parameters", omap{
				{"ConnectProtocolVersion", omap{
					{"name", "Connect-Protocol-Version"},
					{"in", "header"},
					{"required", false},
					{"description", "Connect protocol version. Connect clients send 1; the edge does not require it."},
					{"schema", omap{{"type", "string"}, {"enum", []any{"1"}}}},
				}},
				{"RequestID", omap{
					{"name", "X-Request-Id"},
					{"in", "header"},
					{"required", false},
					{"description", "Caller-supplied request identifier. It correlates log and error records; it grants no authority."},
					{"schema", omap{{"type", "string"}}},
				}},
			}},
			{"schemas", schemasObj},
		}},
	}, nil
}

// operation renders the POST operation for one RPC.
func operation(sb *schemaBuilder, md protoreflect.MethodDescriptor, comment string, registered, exposed bool, aliases []string) omap {
	service := md.Parent().(protoreflect.ServiceDescriptor)
	streaming := streamingKind(md)
	desc := comment
	if streaming != "" {
		note := "gRPC / gRPC-Web only; not callable as Connect unary JSON."
		if desc == "" {
			desc = note
		} else {
			desc = note + "\n\n" + desc
		}
	}
	op := omap{{"tags", []any{string(service.Name())}}}
	op.set("summary", summaryOf(comment, string(md.Name())))
	if desc != "" {
		op.set("description", desc)
	}
	op.set("operationId", string(service.Name())+"_"+string(md.Name()))
	op.set("parameters", []any{
		omap{{"$ref", "#/components/parameters/ConnectProtocolVersion"}},
		omap{{"$ref", "#/components/parameters/RequestID"}},
	})
	op.set("requestBody", omap{
		{"required", true},
		{"content", omap{{"application/json", omap{{"schema", sb.ref(md.Input())}}}}},
	})
	okDesc := "Success."
	if streaming != "" {
		okDesc = "Each message of the response stream."
	}
	op.set("responses", omap{
		{"200", omap{
			{"description", okDesc},
			{"content", omap{{"application/json", omap{{"schema", sb.ref(md.Output())}}}}},
		}},
		{"default", omap{
			{"description", "Connect error. The HTTP status follows the edge's canonical error projection."},
			{"content", omap{{"application/json", omap{{"schema", omap{{"$ref", schemaRefPrefix + errorSchemaName}}}}}}},
		}},
	})
	op.set("x-hcmnext-service", string(service.FullName()))
	op.set("x-hcmnext-rpc-kind", rpcKind(md))
	op.set("x-hcmnext-registered", registered)
	op.set("x-hcmnext-http-exposed", exposed)
	if len(aliases) > 0 {
		list := make([]any, len(aliases))
		for i, a := range aliases {
			list[i] = a
		}
		op.set("x-hcmnext-http-aliases", list)
	}
	if streaming != "" {
		op.set("x-hcmnext-streaming", streaming)
	}
	return op
}

// streamingKind is "server", "client", "bidi" or "" for unary.
func streamingKind(md protoreflect.MethodDescriptor) string {
	switch {
	case md.IsStreamingClient() && md.IsStreamingServer():
		return "bidi"
	case md.IsStreamingServer():
		return "server"
	case md.IsStreamingClient():
		return "client"
	}
	return ""
}

// rpcKind classifies a method by its name's leading verb.
func rpcKind(md protoreflect.MethodDescriptor) string {
	if streamingKind(md) != "" {
		return "STREAM"
	}
	name := string(md.Name())
	for _, rule := range []struct {
		kind  string
		verbs []string
	}{
		{"CREATE", []string{"Create", "Propose"}},
		{"READ", []string{"Get", "Explain", "Inspect", "Preview"}},
		{"LIST", []string{"List"}},
		{"UPDATE", []string{"Update", "Save", "Edit"}},
		{"DELETE", []string{"Delete"}},
	} {
		for _, v := range rule.verbs {
			if hasVerb(name, v) {
				return rule.kind
			}
		}
	}
	return "ACTION"
}

// hasVerb reports whether name starts with the whole word verb.
func hasVerb(name, verb string) bool {
	if !strings.HasPrefix(name, verb) {
		return false
	}
	rest := name[len(verb):]
	return rest == "" || unicode.IsUpper(rune(rest[0]))
}

func errorSchema() omap {
	return omap{
		{"type", "object"},
		{"description", "Connect protocol error body."},
		{"required", []any{"code"}},
		{"properties", omap{
			{"code", omap{{"type", "string"}, {"enum", connectCodes()}}},
			{"message", omap{{"type", "string"}}},
			{"details", omap{{"type", "array"}, {"items", omap{{"$ref", schemaRefPrefix + errorDetailSchemaName}}}}},
		}},
	}
}

func errorDetailSchema() omap {
	return omap{
		{"type", "object"},
		{"description", "Connect error detail. The edge attaches one " + commonErrorDetail + "."},
		{"required", []any{"type", "value"}},
		{"properties", omap{
			{"type", omap{{"type", "string"}, {"description", "Fully qualified Protobuf message name of the detail."}}},
			{"value", omap{{"type", "string"}, {"format", "byte"}, {"description", "Base64 of the binary-encoded detail message."}}},
			{"debug", omap{{"$ref", schemaRefPrefix + commonErrorDetail}, {"description", "Optional JSON rendering of the detail, for debugging only."}}},
		}},
	}
}

func infoDescription() string {
	return strings.Join([]string{
		"Generated from the compiled Protobuf descriptors of schema/proto; the Protobuf sources are canonical. Do not edit this file: regenerate it with `" + RegenerateCommand + "`.",
		"Every RPC is listed as a POST to its Connect procedure path with a JSON body under the Protobuf JSON mapping. The edge rejects unknown request fields.",
		"x-hcmnext-registered says whether the service is registered on the cell's gRPC server. x-hcmnext-http-exposed says whether the procedure is mounted on the cell's Connect HTTP edge; procedures that are not are reachable over gRPC only, or not at all. x-hcmnext-streaming marks streaming RPCs, which are gRPC / gRPC-Web only.",
		"Authentication is a bearer token. Client-credentials OAuth2 for machine clients is planned under INTAPI-001.",
	}, "\n\n")
}
