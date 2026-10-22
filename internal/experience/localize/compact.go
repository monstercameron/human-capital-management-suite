package localize

import (
	"compress/gzip"
	"io"
	"strings"
)

// A large catalog written as a Go map literal costs the browser module about
// 50 bytes of static table for every entry (a key, and a Message with its
// pointers) on top of the text itself, and a loop that copies the table into
// the map. The compact form keeps the text and drops the table: one string of
// alternating keys and texts separated by NUL, cut into substrings of itself
// when the map is built, so no text is copied.
//
// The client build writes this form for the product catalog in place of the
// literal (tools/uxqual/cmd/journeywasm, catalogcompact_overlay.go); the
// literal stays the source everybody edits. Keys and texts that contain NUL are
// not written compactly.

// compactSeparator ends every key and every text of the compact form.
const compactSeparator = "\x00"

// EncodeCompact writes alternating keys and texts. It reports false for a
// string the form cannot carry.
func EncodeCompact(pairs [][2]string) (string, bool) {
	var out strings.Builder
	for _, pair := range pairs {
		if strings.Contains(pair[0], compactSeparator) || strings.Contains(pair[1], compactSeparator) {
			return "", false
		}
		out.WriteString(pair[0])
		out.WriteString(compactSeparator)
		out.WriteString(pair[1])
		out.WriteString(compactSeparator)
	}
	return out.String(), true
}

// EncodeCompactTriples writes keys and their three ordered locale values.
// It reports false if any field contains the compact separator.
func EncodeCompactTriples(entries [][4]string) (string, bool) {
	var out strings.Builder
	for _, entry := range entries {
		for _, value := range entry {
			if strings.Contains(value, compactSeparator) {
				return "", false
			}
			out.WriteString(value)
			out.WriteString(compactSeparator)
		}
	}
	return out.String(), true
}

// walkCompact calls add with each key and text of data and returns how many
// pairs it held. It panics on a string that is not in the compact form, which
// can only come from a generator bug.
func walkCompact(data string, add func(key, text string)) int {
	count := 0
	for data != "" {
		key, rest, ok := strings.Cut(data, compactSeparator)
		if !ok {
			panic("localize: compact catalog ends inside a key")
		}
		text, rest, ok := strings.Cut(rest, compactSeparator)
		if !ok {
			panic("localize: compact catalog ends inside a text")
		}
		add(key, text)
		data = rest
		count++
	}
	return count
}

// ExpandCompact builds the message map of a compact catalog, plus the entries
// that could not be written compactly (plural and gender forms, texts with
// NUL), which are given as an ordinary literal.
func ExpandCompact(data string, extra map[string]Message) map[string]Message {
	messages := make(map[string]Message, strings.Count(data, compactSeparator)/2+len(extra))
	walkCompact(data, func(key, text string) { messages[key] = Message{Text: text} })
	for key, message := range extra {
		messages[key] = message
	}
	return messages
}

// ExpandCompactStrings is ExpandCompact for a catalog of plain strings.
func ExpandCompactStrings(data string, extra map[string]string) map[string]string {
	values := make(map[string]string, strings.Count(data, compactSeparator)/2+len(extra))
	walkCompact(data, func(key, text string) { values[key] = text })
	for key, value := range extra {
		values[key] = value
	}
	return values
}

// ExpandCompactTriples builds a fresh map of keys to three ordered locale
// strings from the compact form. Each record contains four NUL-delimited
// fields: the key followed by its values. Malformed data can only come from a
// build-overlay generator bug, so it panics just like the other expanders.
func ExpandCompactTriples(data string) map[string][3]string {
	values := make(map[string][3]string, strings.Count(data, compactSeparator)/4)
	for data != "" {
		key, rest, ok := strings.Cut(data, compactSeparator)
		if !ok {
			panic("localize: compact triples ends inside a key")
		}
		var locales [3]string
		for i := range locales {
			locales[i], rest, ok = strings.Cut(rest, compactSeparator)
			if !ok {
				panic("localize: compact triples ends inside a value")
			}
		}
		values[key] = locales
		data = rest
	}
	return values
}

// expandGzipCompact decompresses one generated compact payload. expectedLength
// is emitted by the build overlay from the exact source bytes, so the decoder
// rejects both truncated and oversized data without unbounded reads.
func expandGzipCompact(data string, expectedLength int) string {
	maxInt := int(^uint(0) >> 1)
	if expectedLength < 0 || expectedLength == maxInt {
		panic("localize: invalid compressed compact length")
	}
	compressed := strings.NewReader(data)
	reader, err := gzip.NewReader(compressed)
	if err != nil {
		panic("localize: invalid compressed compact data")
	}
	reader.Multistream(false)
	decoded, readErr := io.ReadAll(io.LimitReader(reader, int64(expectedLength)+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(decoded) != expectedLength || compressed.Len() != 0 {
		panic("localize: invalid compressed compact data")
	}
	return string(decoded)
}

// ExpandGzipCompact expands a deterministic, single-member gzip-encoded
// compact catalog and then applies the same extra-entry precedence as
// ExpandCompact.
func ExpandGzipCompact(data string, expectedLength int, extra map[string]Message) map[string]Message {
	return ExpandCompact(expandGzipCompact(data, expectedLength), extra)
}

// ExpandGzipCompactStrings is the compressed form of ExpandCompactStrings.
func ExpandGzipCompactStrings(data string, expectedLength int, extra map[string]string) map[string]string {
	return ExpandCompactStrings(expandGzipCompact(data, expectedLength), extra)
}

// ExpandGzipCompactTriples is the compressed form of ExpandCompactTriples.
func ExpandGzipCompactTriples(data string, expectedLength int) map[string][3]string {
	return ExpandCompactTriples(expandGzipCompact(data, expectedLength))
}
