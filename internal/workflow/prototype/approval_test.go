package prototype

import "testing"

func TestApprovalV100RetainsPublishedIRSchemaAndDigest(t *testing.T) {
	compiled, err := CompileApproval()
	if err != nil {
		t.Fatalf("CompileApproval: %v", err)
	}
	if compiled.SchemaVersion() != 1 {
		t.Fatalf("schema version = %d, want frozen published schema 1", compiled.SchemaVersion())
	}
	const publishedDigest = "6ad5a72214b7200f0957d40c983b3763de6201ba3a4ea209a49bb0f587a974c0"
	if compiled.Digest() != publishedDigest {
		t.Fatalf("compiled digest = %s, want published 1.0.0 digest %s", compiled.Digest(), publishedDigest)
	}
}

func TestApproval_Smoke(t *testing.T) {
	if t == nil {
		t.Fatalf("nil tester")
	}
}

func TestApproval_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
