package openapi

import (
	"bytes"
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"
)

// omap is an insertion-ordered mapping. The generator builds the whole
// document from omap, []any and scalars so that key order in the output is
// fixed by construction rather than by map iteration.
type omap []kv

type kv struct {
	k string
	v any
}

// set appends key k with value v.
func (m *omap) set(k string, v any) { *m = append(*m, kv{k, v}) }

// get returns the value stored under k.
func (m omap) get(k string) (any, bool) {
	for _, e := range m {
		if e.k == k {
			return e.v, true
		}
	}
	return nil, false
}

// toNode converts an omap/[]any/scalar tree to a yaml.Node tree.
func toNode(v any) (*yaml.Node, error) {
	switch t := v.(type) {
	case omap:
		n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, e := range t {
			val, err := toNode(e.v)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", e.k, err)
			}
			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: e.k}, val)
		}
		return n, nil
	case []any:
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for i, e := range t {
			val, err := toNode(e)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			n.Content = append(n.Content, val)
		}
		return n, nil
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(t)}, nil
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(t)}, nil
	default:
		return nil, fmt.Errorf("unsupported YAML value %T", v)
	}
}

// encodeYAML renders the tree as a YAML document with two-space indentation.
func encodeYAML(v any) ([]byte, error) {
	root, err := toNode(v)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
