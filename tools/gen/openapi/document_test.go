package openapi

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

func TestBuildDocumentOverASyntheticRegistry(t *testing.T) {
	reg, fd := sampleFile(t, true)
	// No proto sources exist for the synthetic file; supply an empty one so
	// summaries fall back to method names.
	dir := t.TempDir()
	writeFile(t, dir, "hcmnext/sample/v1/sample.proto", "")
	writeFile(t, dir, "hcmnext/common/v1/common.proto", "")
	doc, err := buildDocument(reg, dir)
	if err != nil {
		t.Fatalf("buildDocument: %v", err)
	}
	pathsV, _ := doc.get("paths")
	paths := pathsV.(omap)
	if len(paths) != fd.Services().Get(0).Methods().Len() {
		t.Fatalf("paths = %d", len(paths))
	}
	for i := 1; i < len(paths); i++ {
		if paths[i-1].k >= paths[i].k {
			t.Fatalf("paths not sorted: %s, %s", paths[i-1].k, paths[i].k)
		}
	}
	op := func(path string) omap {
		item, ok := paths.get(path)
		if !ok {
			t.Fatalf("missing %s", path)
		}
		post, _ := item.(omap).get("post")
		return post.(omap)
	}
	get := op("/hcmnext.sample.v1.SampleService/GetNode")
	if s, _ := get.get("summary"); s != "GetNode" {
		t.Errorf("summary fallback = %v", s)
	}
	if _, ok := get.get("description"); ok {
		t.Error("unary RPC without a comment has a description")
	}
	if v, _ := get.get("x-hcmnext-registered"); v != false {
		t.Errorf("synthetic service registered = %v", v)
	}
	for path, want := range map[string]string{
		"/hcmnext.sample.v1.SampleService/WatchNode": "server",
		"/hcmnext.sample.v1.SampleService/PushNodes": "client",
		"/hcmnext.sample.v1.SampleService/ChatNodes": "bidi",
	} {
		o := op(path)
		if v, _ := o.get("x-hcmnext-streaming"); v != want {
			t.Errorf("%s streaming = %v, want %s", path, v, want)
		}
		if d, _ := o.get("description"); d != "gRPC / gRPC-Web only; not callable as Connect unary JSON." {
			t.Errorf("%s description = %v", path, d)
		}
	}
	if _, err := encodeYAML(doc); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

func TestBuildDocumentFailures(t *testing.T) {
	if _, err := buildDocument(new(protoregistry.Files), t.TempDir()); err == nil || !strings.Contains(err.Error(), "no hcmnext.") {
		t.Fatalf("empty registry err = %v", err)
	}
	reg, _ := sampleFile(t, false)
	dir := t.TempDir()
	if _, err := buildDocument(reg, dir); err == nil || !strings.Contains(err.Error(), "read proto comments") {
		t.Fatalf("missing proto sources err = %v", err)
	}
	writeFile(t, dir, "hcmnext/sample/v1/sample.proto", "")
	if _, err := buildDocument(reg, dir); err == nil || !strings.Contains(err.Error(), commonErrorDetail) {
		t.Fatalf("missing ErrorDetail err = %v", err)
	}
}

func TestRPCKindClassifiesByLeadingVerb(t *testing.T) {
	_, fd := sampleFile(t, false)
	methods := fd.Services().Get(0).Methods()
	if got := rpcKind(methods.ByName("GetNode")); got != "READ" {
		t.Errorf("GetNode = %s", got)
	}
	if got := rpcKind(methods.ByName("WatchNode")); got != "STREAM" {
		t.Errorf("WatchNode = %s", got)
	}
	for name, want := range map[string]string{
		"CreateIntent": "CREATE", "ProposePromotion": "CREATE", "GetIntent": "READ",
		"ExplainIntent": "READ", "InspectJourney": "READ", "PreviewThing": "READ",
		"ListIntents": "LIST", "UpdateX": "UPDATE", "SaveX": "UPDATE", "EditX": "UPDATE",
		"DeleteX": "DELETE", "SubmitIntent": "ACTION", "Listen": "ACTION", "Getaway": "ACTION",
		"List": "LIST",
	} {
		if got := rpcKind(fakeMethod(name)); got != want {
			t.Errorf("rpcKind(%s) = %s, want %s", name, got, want)
		}
	}
}

// fakeMethod is a unary method descriptor with the given name.
type fakeMethodDescriptor struct {
	protoreflect.MethodDescriptor
	name string
}

func (f fakeMethodDescriptor) Name() protoreflect.Name { return protoreflect.Name(f.name) }
func (fakeMethodDescriptor) IsStreamingClient() bool   { return false }
func (fakeMethodDescriptor) IsStreamingServer() bool   { return false }

func fakeMethod(name string) protoreflect.MethodDescriptor { return fakeMethodDescriptor{name: name} }

func TestConnectCodesAreTheSixteenConnectCodes(t *testing.T) {
	codes := connectCodes()
	seen := map[any]bool{}
	for _, c := range codes {
		seen[c] = true
	}
	if len(codes) != 16 || len(seen) != 16 || !seen["unauthenticated"] || !seen["invalid_argument"] {
		t.Fatalf("codes = %v", codes)
	}
}
