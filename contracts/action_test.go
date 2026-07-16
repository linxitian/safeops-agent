package contracts

import "testing"

func TestProposalDigestCanonicalForMapOrder(t *testing.T) {
	a := ActionProposal{Tool: "service.restart", Arguments: map[string]any{"unit": "demo", "force": false}}
	b := ActionProposal{Tool: "service.restart", Arguments: map[string]any{"force": false, "unit": "demo"}}
	da, err := a.Digest()
	if err != nil {
		t.Fatal(err)
	}
	db, err := b.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Fatalf("map order changed digest %s != %s", da, db)
	}
}
