package localize

import (
	"bytes"
	"compress/gzip"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestCompactRoundTrip(t *testing.T) {
	pairs := [][2]string{{"a.b", "Hello"}, {"empty", ""}, {"uni", "Grüße ⟦x⟧ \n\t\"q\""}, {"", "empty key"}}
	data, ok := EncodeCompact(pairs)
	if !ok {
		t.Fatal("encode refused plain strings")
	}
	got := ExpandCompact(data, map[string]Message{"plural": {Text: "one", Plural: map[string]string{"other": "many"}}})
	want := map[string]Message{"a.b": {Text: "Hello"}, "empty": {}, "uni": {Text: "Grüße ⟦x⟧ \n\t\"q\""}, "": {Text: "empty key"},
		"plural": {Text: "one", Plural: map[string]string{"other": "many"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expanded = %v, want %v", got, want)
	}
	strs := ExpandCompactStrings(data, map[string]string{"x": "y"})
	if len(strs) != 5 || strs["a.b"] != "Hello" || strs["x"] != "y" {
		t.Fatalf("strings = %v", strs)
	}
	if got := ExpandCompact("", nil); len(got) != 0 {
		t.Fatalf("empty data = %v", got)
	}
}

func TestCompactRefusesNUL(t *testing.T) {
	for _, pair := range [][2]string{{"a\x00b", "c"}, {"a", "b\x00c"}} {
		if _, ok := EncodeCompact([][2]string{pair}); ok {
			t.Fatalf("%q was encoded", pair)
		}
	}
}

func TestCompactExtraWins(t *testing.T) {
	data, _ := EncodeCompact([][2]string{{"k", "compact"}})
	if got := ExpandCompact(data, map[string]Message{"k": {Text: "extra"}}); got["k"].Text != "extra" {
		t.Fatalf("got %v", got)
	}
}

func TestCompactPanicsOnMalformedData(t *testing.T) {
	for _, data := range []string{"key-without-terminator", "key\x00text-without-terminator", "key\x00"} {
		func() {
			defer func() {
				if message, _ := recover().(string); !strings.Contains(message, "compact catalog") {
					t.Errorf("%q: recovered %v", data, message)
				}
			}()
			ExpandCompact(data, nil)
		}()
	}
}

func TestCompactTriplesRoundTripAndFreshMaps(t *testing.T) {
	entries := [][4]string{
		{"", "", "", ""},
		{"tab", "actual\ttab", `literal\t`, "Arabic العربية"},
		{"newline", "actual\nline", `literal\n`, "line\nbreak"},
		{"quote", `She said "hello"`, "Grüße ⟦x⟧", "مرحبا"},
	}
	data, ok := EncodeCompactTriples(entries)
	if !ok {
		t.Fatal("encoder refused plain triple values")
	}
	want := map[string][3]string{}
	for _, entry := range entries {
		want[entry[0]] = [3]string{entry[1], entry[2], entry[3]}
	}
	first := ExpandCompactTriples(data)
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("expanded triples = %#v, want %#v", first, want)
	}
	first["new"] = [3]string{"value", "value", "value"}
	second := ExpandCompactTriples(data)
	if !reflect.DeepEqual(second, want) {
		t.Fatalf("second expansion shared mutable map state: %#v", second)
	}
}

func TestCompactTriplesRefusesNUL(t *testing.T) {
	for _, entry := range [][4]string{
		{"key\x00part", "en", "de", "ar"},
		{"key", "en", "de\x00part", "ar"},
	} {
		if _, ok := EncodeCompactTriples([][4]string{entry}); ok {
			t.Fatalf("entry with NUL was encoded: %#v", entry)
		}
	}
}

func TestCompactStringsReturnsFreshMutableMaps(t *testing.T) {
	data, ok := EncodeCompact([][2]string{{"key", "value"}})
	if !ok {
		t.Fatal("encoder refused plain strings")
	}
	first := ExpandCompactStrings(data, nil)
	second := ExpandCompactStrings(data, nil)
	first["key"] = "changed"
	first["added"] = "value"
	if second["key"] != "value" || len(second) != 1 {
		t.Fatalf("expanded maps share state: first=%v second=%v", first, second)
	}
}

func TestCompactTriplesPanicsOnMalformedData(t *testing.T) {
	for _, data := range []string{
		"key-without-terminator",
		"key\x00en-without-terminator",
		"key\x00en\x00de-without-terminator",
		"key\x00en\x00de\x00ar-without-terminator",
		"key\x00en\x00de\x00",
	} {
		t.Run(strconv.Quote(data), func(t *testing.T) {
			defer func() {
				if message, _ := recover().(string); !strings.Contains(message, "compact triples") {
					t.Fatalf("recovered %v, want malformed compact triples panic", message)
				}
			}()
			ExpandCompactTriples(data)
		})
	}
}

func TestCompactGzipRoundTripsAndReturnsFreshMaps(t *testing.T) {
	pairs := [][2]string{{"", ""}, {"plain", "actual\nline\tGrüße العربية"}, {"quoted", `literal\n`}}
	plain, ok := EncodeCompact(pairs)
	if !ok {
		t.Fatal("encoder refused plain compact data")
	}
	compressed := gzipCompactForTest(t, plain)
	extra := map[string]Message{
		"plain":  {Text: "overrides compact value"},
		"plural": {Text: "one", Plural: map[string]string{"other": "many"}},
	}
	firstMessages := ExpandGzipCompact(compressed, len(plain), extra)
	secondMessages := ExpandGzipCompact(compressed, len(plain), extra)
	if firstMessages["plain"].Text != "overrides compact value" || firstMessages["quoted"].Text != `literal\n` || firstMessages["plural"].Plural["other"] != "many" {
		t.Fatalf("compressed messages differ: %#v", firstMessages)
	}
	if empty, exists := firstMessages[""]; !exists || empty.Text != "" || len(firstMessages) != 4 {
		t.Fatalf("compressed empty key/value is missing: %#v", firstMessages)
	}
	firstMessages["new"] = Message{Text: "mutation"}
	if _, exists := secondMessages["new"]; exists || len(extra) != 2 || extra["plain"].Text != "overrides compact value" || extra["plural"].Plural["other"] != "many" {
		t.Fatalf("compressed message maps share state or mutated extras: first=%v second=%v extra=%v", firstMessages, secondMessages, extra)
	}

	firstStrings := ExpandGzipCompactStrings(compressed, len(plain), map[string]string{"plain": "override", "extra": "value"})
	secondStrings := ExpandGzipCompactStrings(compressed, len(plain), nil)
	if firstStrings["plain"] != "override" || firstStrings["extra"] != "value" {
		t.Fatalf("compressed extra entries did not take precedence: %v", firstStrings)
	}
	firstStrings["plain"] = "changed"
	if secondStrings["plain"] != "actual\nline\tGrüße العربية" {
		t.Fatalf("compressed string maps differ or share state: first=%v second=%v", firstStrings, secondStrings)
	}
	if _, exists := secondStrings["extra"]; exists || firstStrings["extra"] != "value" || len(firstStrings) != 4 {
		t.Fatalf("compressed extra-entry handling differs: first=%v second=%v", firstStrings, secondStrings)
	}

	triples := [][4]string{{"", "", "", ""}, {"key", "actual\n", `literal\n`, "مرحبا"}}
	tripleData, ok := EncodeCompactTriples(triples)
	if !ok {
		t.Fatal("encoder refused plain compact triples")
	}
	tripleCompressed := gzipCompactForTest(t, tripleData)
	firstTriples := ExpandGzipCompactTriples(tripleCompressed, len(tripleData))
	secondTriples := ExpandGzipCompactTriples(tripleCompressed, len(tripleData))
	firstTriples["new"] = [3]string{"x", "x", "x"}
	if _, exists := secondTriples["new"]; exists || secondTriples["key"] != [3]string{"actual\n", `literal\n`, "مرحبا"} {
		t.Fatalf("compressed triple maps differ or share state: first=%v second=%v", firstTriples, secondTriples)
	}
	if value, exists := secondTriples[""]; !exists || value != [3]string{} || len(secondTriples) != 2 {
		t.Fatalf("compressed empty triple key/value is missing: %#v", secondTriples)
	}
}

func TestCompactGzipRejectsBadLengthAndMalformedStreams(t *testing.T) {
	plain, ok := EncodeCompact([][2]string{{"key", "text"}})
	if !ok {
		t.Fatal("encoder refused compact data")
	}
	valid := gzipCompactForTest(t, plain)
	secondMember := gzipCompactForTest(t, "extra")
	corruptCRC := []byte(valid)
	corruptCRC[len(corruptCRC)-8] ^= 0xff
	corruptSize := []byte(valid)
	corruptSize[len(corruptSize)-1] ^= 0xff
	cases := map[string]struct {
		data   string
		length int
	}{
		"negative length":       {valid, -1},
		"maximum int length":    {valid, int(^uint(0) >> 1)},
		"short expected length": {valid, len(plain) - 1},
		"long expected length":  {valid, len(plain) + 1},
		"truncated trailer":     {valid[:len(valid)-4], len(plain)},
		"truncated body":        {valid[:len(valid)-10], len(plain)},
		"truncated header":      {valid[:5], len(plain)},
		"bad checksum":          {string(corruptCRC), len(plain)},
		"bad uncompressed size": {string(corruptSize), len(plain)},
		"trailing junk":         {valid + "junk", len(plain)},
		"second gzip member":    {valid + secondMember, len(plain)},
		"invalid header":        {"not gzip", len(plain)},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			assertCompactGzipPanics(t, func() { ExpandGzipCompactStrings(test.data, test.length, nil) })
		})
	}

	empty := gzipCompactForTest(t, "")
	if got := ExpandGzipCompactStrings(empty, 0, nil); len(got) != 0 {
		t.Fatalf("empty compressed catalog = %v", got)
	}
}

func gzipCompactForTest(t *testing.T, data string) string {
	t.Helper()
	var encoded bytes.Buffer
	writer, err := gzip.NewWriterLevel(&encoded, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	writer.Header.Name = ""
	writer.Header.Comment = ""
	writer.Header.OS = 255
	if _, err := writer.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return encoded.String()
}

func assertCompactGzipPanics(t *testing.T, call func()) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected compressed compact data to be rejected")
		}
	}()
	call()
}
