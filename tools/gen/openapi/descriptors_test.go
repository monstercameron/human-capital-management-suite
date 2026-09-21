package openapi

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// TestBlankImportsLinkEverySchemaProtoFile proves the blank-import list is
// complete: every .proto under schema/proto is a linked descriptor, and
// nothing outside the hcmnext namespace is included.
func TestBlankImportsLinkEverySchemaProtoFile(t *testing.T) {
	root := filepath.Join(repoRoot(t), protoSourceDir)
	var want []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".proto") {
			rel, _ := filepath.Rel(root, p)
			want = append(want, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(want)
	var got []string
	for _, fd := range hcmnextFiles(protoregistry.GlobalFiles) {
		if !strings.HasPrefix(string(fd.Package()), packagePrefix) {
			t.Errorf("%s is outside %s", fd.Path(), packagePrefix)
		}
		got = append(got, fd.Path())
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("linked descriptors:\n%v\nschema/proto files:\n%v", got, want)
	}
}

func TestServicesAndMethodsAreSorted(t *testing.T) {
	svcs := services(hcmnextFiles(protoregistry.GlobalFiles))
	if len(svcs) == 0 {
		t.Fatal("no services")
	}
	for i := 1; i < len(svcs); i++ {
		if svcs[i-1].FullName() >= svcs[i].FullName() {
			t.Fatalf("services not sorted: %s before %s", svcs[i-1].FullName(), svcs[i].FullName())
		}
	}
	for _, sd := range svcs {
		ms := methods(sd)
		if len(ms) != sd.Methods().Len() {
			t.Fatalf("%s: %d methods, want %d", sd.FullName(), len(ms), sd.Methods().Len())
		}
		for i := 1; i < len(ms); i++ {
			if ms[i-1].Name() >= ms[i].Name() {
				t.Fatalf("%s methods not sorted", sd.FullName())
			}
		}
	}
	d, err := protoregistry.GlobalFiles.FindDescriptorByName("hcmnext.intents.v1.IntentService.CreateIntent")
	if err != nil {
		t.Fatal(err)
	}
	if got := procedurePath(d.(protoreflect.MethodDescriptor)); got != "/hcmnext.intents.v1.IntentService/CreateIntent" {
		t.Fatalf("procedurePath = %s", got)
	}
}

func TestHcmnextFilesEmptyRegistry(t *testing.T) {
	if got := hcmnextFiles(new(protoregistry.Files)); len(got) != 0 {
		t.Fatalf("empty registry yielded %d files", len(got))
	}
}
