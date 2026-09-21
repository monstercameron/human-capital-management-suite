package openapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleProto = `syntax = "proto3";

package hcmnext.sample.v1;

// Detached comment, separated by a blank line.

// SampleService does sample things.
//
// Second paragraph with create/claim/
// complete.
service SampleService {
  // DoThing does the thing. More detail here.
  rpc DoThing(Req) returns (Res);
  rpc Bare(Req) returns (Res);
  // Multi splits its declaration.
  rpc Multi(
    Req
  ) returns (Res);
}
`

func TestScanCommentsReadsLeadingComments(t *testing.T) {
	got := comments{}
	if err := scanComments(strings.NewReader(sampleProto), got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"hcmnext.sample.v1.SampleService":         "SampleService does sample things.\n\nSecond paragraph with create/claim/complete.",
		"hcmnext.sample.v1.SampleService.DoThing": "DoThing does the thing. More detail here.",
		"hcmnext.sample.v1.SampleService.Multi":   "Multi splits its declaration.",
	}
	if len(got) != len(want) {
		t.Fatalf("comments = %#v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestReadCommentsFromFilesAndMissingFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "s.proto"), []byte(sampleProto), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readComments(dir, []string{"a/s.proto"})
	if err != nil || got["hcmnext.sample.v1.SampleService.DoThing"] == "" {
		t.Fatalf("readComments = %v, %v", got, err)
	}
	if _, err := readComments(dir, []string{"missing.proto"}); err == nil {
		t.Fatal("missing proto file was not an error")
	}
}

func TestSummaryOf(t *testing.T) {
	long := strings.Repeat("word ", 40)
	for _, tc := range []struct{ in, want string }{
		{"", "Fallback"},
		{"First sentence. Second sentence.", "First sentence."},
		{"Only one\n\nparagraph two", "Only one"},
		{long, strings.TrimSpace(strings.Repeat("word ", 24)) + "..."},
	} {
		if got := summaryOf(tc.in, "Fallback"); got != tc.want {
			t.Errorf("summaryOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
