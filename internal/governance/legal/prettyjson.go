package legal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// This file renders a definition file exactly the way Prettier 3 renders it
// under the repository's .prettierrc (printWidth 88). It exists because the
// definition files are both generated and formatted: the extractor writes
// them, `npx prettier --write definitions/legal` runs over them in the
// standard check sequence, and the regeneration test asserts the two agree
// byte for byte. If the generator emitted encoding/json's indentation
// instead, every prettier run would rewrite fifty-one files and the regeneration
// test would fail on a formatting difference rather than on a content one.
//
// The three rules this reproduces, all confirmed against prettier 3.8.3 with
// printWidth 88:
//
//  1. A non-empty object always breaks, one key per line. (Prettier's
//     objectWrap default preserves the input's break, and this printer always
//     breaks, so the two agree and stay agreed.)
//  2. An array prints on one line when the whole line — indentation, the
//     "key": prefix, the brackets, and a trailing comma when one follows —
//     is at most 88 columns. Otherwise it breaks, one element per line.
//  3. An empty object is {} and an empty array is [] on the line they start.
//
// Prettier's "concise fill" for arrays of numbers is deliberately not
// implemented: the definition schema has no numeric arrays, and a printer
// that cannot produce a shape cannot disagree about it.

const prettierPrintWidth = 88
const prettierIndent = "  "

// jsonNode is an order-preserving JSON tree. encoding/json's map decoding
// sorts keys, which would silently reorder a definition file; the token
// stream does not.
type jsonNode struct {
	// kind is 'o' object, 'a' array, 's' scalar.
	kind   byte
	scalar string
	keys   []string
	values []*jsonNode
	items  []*jsonNode
}

func parseJSONNode(dec *json.Decoder) (*jsonNode, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			node := &jsonNode{kind: 'o'}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("legal: non-string object key %v", keyTok)
				}
				value, err := parseJSONNode(dec)
				if err != nil {
					return nil, err
				}
				node.keys = append(node.keys, key)
				node.values = append(node.values, value)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return node, nil
		case '[':
			node := &jsonNode{kind: 'a'}
			for dec.More() {
				item, err := parseJSONNode(dec)
				if err != nil {
					return nil, err
				}
				node.items = append(node.items, item)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return node, nil
		default:
			return nil, fmt.Errorf("legal: unexpected delimiter %v", t)
		}
	default:
		text, err := marshalScalar(tok)
		if err != nil {
			return nil, err
		}
		return &jsonNode{kind: 's', scalar: text}, nil
	}
}

func marshalScalar(tok json.Token) (string, error) {
	switch v := tok.(type) {
	case nil:
		return "null", nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case json.Number:
		return v.String(), nil
	case string:
		return encodeJSONString(v), nil
	default:
		return "", fmt.Errorf("legal: unexpected JSON scalar %T", tok)
	}
}

// encodeJSONString escapes a string the way encoding/json does with HTML
// escaping off, which is also what Prettier emits.
func encodeJSONString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		// A string always marshals; the encoder only fails on unsupported
		// types.
		return `""`
	}
	return strings.TrimRight(buf.String(), "\n")
}

// oneLine renders a node with no line breaks, or reports that it cannot.
func (n *jsonNode) oneLine() (string, bool) {
	switch n.kind {
	case 's':
		return n.scalar, true
	case 'a':
		if len(n.items) == 0 {
			return "[]", true
		}
		parts := make([]string, 0, len(n.items))
		for _, item := range n.items {
			// An array whose elements are non-empty objects always breaks,
			// because a broken object carries a hard line break out of the
			// group.
			if item.kind == 'o' && len(item.keys) > 0 {
				return "", false
			}
			text, ok := item.oneLine()
			if !ok {
				return "", false
			}
			parts = append(parts, text)
		}
		return "[" + strings.Join(parts, ", ") + "]", true
	case 'o':
		if len(n.keys) == 0 {
			return "{}", true
		}
		// A non-empty object always breaks; see rule 1 above.
		return "", false
	default:
		return "", false
	}
}

// render writes n at the given indentation. column is the column the value
// starts at, and suffix is whatever follows it on the same line (a comma, or
// nothing for a last element), both of which count toward the width budget.
func (n *jsonNode) render(b *strings.Builder, indent int, column int, suffix string) {
	if text, ok := n.oneLine(); ok {
		if n.kind != 'a' || column+len(text)+len(suffix) <= prettierPrintWidth {
			b.WriteString(text)
			b.WriteString(suffix)
			return
		}
	}
	pad := strings.Repeat(prettierIndent, indent)
	inner := strings.Repeat(prettierIndent, indent+1)
	switch n.kind {
	case 'a':
		b.WriteString("[\n")
		for i, item := range n.items {
			b.WriteString(inner)
			itemSuffix := ","
			if i == len(n.items)-1 {
				itemSuffix = ""
			}
			item.render(b, indent+1, len(inner), itemSuffix)
			b.WriteString("\n")
		}
		b.WriteString(pad)
		b.WriteString("]")
	case 'o':
		b.WriteString("{\n")
		for i, key := range n.keys {
			keyText := encodeJSONString(key) + ": "
			b.WriteString(inner)
			b.WriteString(keyText)
			valueSuffix := ","
			if i == len(n.keys)-1 {
				valueSuffix = ""
			}
			n.values[i].render(b, indent+1, len(inner)+len(keyText), valueSuffix)
			b.WriteString("\n")
		}
		b.WriteString(pad)
		b.WriteString("}")
	default:
		b.WriteString(n.scalar)
	}
	b.WriteString(suffix)
}

// prettierJSON renders v as Prettier would render it under the repository's
// .prettierrc, with a single trailing newline.
func prettierJSON(v any) ([]byte, error) {
	var compact bytes.Buffer
	enc := json.NewEncoder(&compact)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(compact.Bytes()))
	dec.UseNumber()
	node, err := parseJSONNode(dec)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	node.render(&b, 0, 0, "")
	b.WriteString("\n")
	return []byte(b.String()), nil
}
