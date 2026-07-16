package evidence

import "testing"

func TestGraphRequiresNodesAndProducesStableSnapshot(t *testing.T) {
	g := New()
	if err := g.Link(Edge{From: "a", To: "b", Relation: "R"}); err == nil {
		t.Fatal("edge without nodes accepted")
	}
	if err := g.Upsert(Node{ID: "b", Type: Port, Label: "8080"}); err != nil {
		t.Fatal(err)
	}
	if err := g.Upsert(Node{ID: "a", Type: Process, Label: "worker"}); err != nil {
		t.Fatal(err)
	}
	if err := g.Link(Edge{From: "a", To: "b", Relation: "PROCESS_LISTENS_PORT", EvidenceRefs: []string{"e1"}}); err != nil {
		t.Fatal(err)
	}
	snapshot := g.Snapshot()
	if len(snapshot.Nodes) != 2 || snapshot.Nodes[0].ID != "a" || len(snapshot.Edges) != 1 {
		t.Fatalf("unexpected graph: %+v", snapshot)
	}
}
