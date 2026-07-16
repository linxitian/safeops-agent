package evidence

import (
	"errors"
	"sort"
	"sync"
)

type NodeType string

const (
	Host           NodeType = "Host"
	Service        NodeType = "Service"
	Process        NodeType = "Process"
	Port           NodeType = "Port"
	File           NodeType = "File"
	Mount          NodeType = "Mount"
	Metric         NodeType = "Metric"
	LogEvent       NodeType = "LogEvent"
	ConfigSnapshot NodeType = "ConfigSnapshot"
)

type Node struct {
	ID           string         `json:"id"`
	Type         NodeType       `json:"type"`
	Label        string         `json:"label"`
	Attributes   map[string]any `json:"attributes,omitempty"`
	EvidenceRefs []string       `json:"evidence_refs,omitempty"`
}
type Edge struct {
	From         string   `json:"from"`
	To           string   `json:"to"`
	Relation     string   `json:"relation"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}
type Snapshot struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}
type Graph struct {
	mu    sync.RWMutex
	nodes map[string]Node
	edges map[string]Edge
}

func New() *Graph { return &Graph{nodes: map[string]Node{}, edges: map[string]Edge{}} }
func (g *Graph) Upsert(node Node) error {
	if node.ID == "" || node.Type == "" {
		return errors.New("evidence node requires id and type")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if existing, ok := g.nodes[node.ID]; ok && existing.Type != node.Type {
		return errors.New("evidence node type cannot change")
	}
	g.nodes[node.ID] = cloneNode(node)
	return nil
}
func (g *Graph) Link(edge Edge) error {
	if edge.From == "" || edge.To == "" || edge.Relation == "" {
		return errors.New("evidence edge is incomplete")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.nodes[edge.From]; !ok {
		return errors.New("edge source node missing")
	}
	if _, ok := g.nodes[edge.To]; !ok {
		return errors.New("edge target node missing")
	}
	key := edge.From + "\x00" + edge.Relation + "\x00" + edge.To
	g.edges[key] = cloneEdge(edge)
	return nil
}
func (g *Graph) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := Snapshot{Nodes: make([]Node, 0, len(g.nodes)), Edges: make([]Edge, 0, len(g.edges))}
	for _, node := range g.nodes {
		out.Nodes = append(out.Nodes, cloneNode(node))
	}
	for _, edge := range g.edges {
		out.Edges = append(out.Edges, cloneEdge(edge))
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	sort.Slice(out.Edges, func(i, j int) bool {
		if out.Edges[i].From == out.Edges[j].From {
			return out.Edges[i].Relation < out.Edges[j].Relation
		}
		return out.Edges[i].From < out.Edges[j].From
	})
	return out
}
func cloneNode(value Node) Node {
	value.EvidenceRefs = append([]string(nil), value.EvidenceRefs...)
	if value.Attributes != nil {
		attrs := map[string]any{}
		for k, v := range value.Attributes {
			attrs[k] = v
		}
		value.Attributes = attrs
	}
	return value
}
func cloneEdge(value Edge) Edge {
	value.EvidenceRefs = append([]string(nil), value.EvidenceRefs...)
	return value
}
