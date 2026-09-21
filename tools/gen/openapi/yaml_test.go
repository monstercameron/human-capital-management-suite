package openapi

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEncodeYAMLKeepsOrderAndQuotesAmbiguousStrings(t *testing.T) {
	m := omap{}
	m.set("zeta", 1)
	m.set("alpha", true)
	m.set("200", "123")
	m.set("list", []any{"a", omap{{"k", "v"}}})
	m.set("empty", omap{})
	m.set("none", []any{})
	out, err := encodeYAML(m)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if strings.Index(text, "zeta") > strings.Index(text, "alpha") {
		t.Fatalf("insertion order lost:\n%s", text)
	}
	var back map[string]any
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back["zeta"] != 1 || back["alpha"] != true || back["200"] != "123" {
		t.Fatalf("round trip = %#v", back)
	}
	if _, ok := back["empty"].(map[string]any); !ok {
		t.Fatalf("empty mapping round trip = %#v", back["empty"])
	}
	if v, ok := m.get("200"); !ok || v != "123" {
		t.Fatalf("get = %v, %v", v, ok)
	}
	if _, ok := m.get("missing"); ok {
		t.Fatal("get found a missing key")
	}
}

func TestEncodeYAMLRejectsUnsupportedValues(t *testing.T) {
	for _, v := range []any{
		3.5,
		omap{{"k", 3.5}},
		[]any{3.5},
	} {
		if _, err := encodeYAML(v); err == nil {
			t.Fatalf("encodeYAML(%#v) succeeded", v)
		}
	}
}
