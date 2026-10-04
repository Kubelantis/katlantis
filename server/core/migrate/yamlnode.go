package migrate

import (
	"go.yaml.in/yaml/v4"
)

// Small helpers over yaml.Node so files keep their comments and key order.

func mapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func mapDelete(m *yaml.Node, key string) bool {
	if m == nil || m.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return true
		}
	}
	return false
}

// mapSet replaces key's value, or appends key at the end.
func mapSet(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content, scalar(key), val)
}

func scalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

func scalarAt(m *yaml.Node, key string) string {
	if n := mapGet(m, key); n != nil && n.Kind == yaml.ScalarNode {
		return n.Value
	}
	return ""
}

func scalars(seq *yaml.Node) []string {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, 0, len(seq.Content))
	for _, n := range seq.Content {
		out = append(out, n.Value)
	}
	return out
}

func seqOf(vals []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, v := range vals {
		n.Content = append(n.Content, scalar(v))
	}
	return n
}

func newMap() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"} }
