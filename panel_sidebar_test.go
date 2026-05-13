package main

import "testing"

func TestSidebarVisualNodeAtAccountsForWrappedRows(t *testing.T) {
	sp := NewSidebarPanel(28, 20)
	sp.nodes = []SidebarNode{
		{Kind: NodeGroup, Label: "COLLECTIONS", Depth: -1},
		{Kind: NodeHistEntry, Label: "POST    localhost:5954/very-long-path", Depth: 0},
		{Kind: NodeHistEntry, Label: "GET     google.com", Depth: 0},
	}

	height := sp.nodeVisualHeight(sp.nodes[1])
	if height < 2 {
		t.Fatalf("wrapped node height = %d, want at least 2", height)
	}
	if idx, ok := sp.visualNodeAt(1); !ok || idx != 1 {
		t.Fatalf("visual row 1 = (%d, %v), want node 1", idx, ok)
	}
	if idx, ok := sp.visualNodeAt(height); !ok || idx != 1 {
		t.Fatalf("last wrapped row = (%d, %v), want node 1", idx, ok)
	}
	if idx, ok := sp.visualNodeAt(height + 1); !ok || idx != 2 {
		t.Fatalf("row after wrapped node = (%d, %v), want node 2", idx, ok)
	}
}
