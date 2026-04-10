package pipeline

import (
	"testing"

	"flow/internal/config"
)

func TestTopoOrderLinearChain(t *testing.T) {
	p := &config.Pipeline{
		Stages: []config.Stage{
			{Workspace: "./a"},
			{Workspace: "./b"},
			{Workspace: "./c"},
		},
		Transitions: []config.Transition{
			{From: "a", To: "b"},
			{From: "b", To: "c"},
		},
	}

	order, err := TopoOrder(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 {
		t.Fatalf("order length = %d, want 2", len(order))
	}
	// a->b must come before b->c
	if order[0] != 0 || order[1] != 1 {
		t.Errorf("order = %v, want [0, 1]", order)
	}
}

func TestTopoOrderParallel(t *testing.T) {
	p := &config.Pipeline{
		Stages: []config.Stage{
			{Workspace: "./a"},
			{Workspace: "./b"},
			{Workspace: "./c"},
		},
		Transitions: []config.Transition{
			{From: "a", To: "b"},
			{From: "a", To: "c"},
		},
	}

	order, err := TopoOrder(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 {
		t.Fatalf("order length = %d, want 2", len(order))
	}
	// Both read from "a" and write to different stages; no ordering constraint.
	// Just check both are present.
	seen := map[int]bool{order[0]: true, order[1]: true}
	if !seen[0] || !seen[1] {
		t.Errorf("order = %v, want both 0 and 1", order)
	}
}

func TestTopoOrderDiamond(t *testing.T) {
	p := &config.Pipeline{
		Stages: []config.Stage{
			{Workspace: "./a"},
			{Workspace: "./b"},
			{Workspace: "./c"},
			{Workspace: "./d"},
		},
		Transitions: []config.Transition{
			{From: "a", To: "b"}, // 0
			{From: "a", To: "c"}, // 1
			{From: "b", To: "d"}, // 2
			{From: "c", To: "d"}, // 3
		},
	}

	order, err := TopoOrder(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 4 {
		t.Fatalf("order length = %d, want 4", len(order))
	}

	// Constraints: 0 before 2 (a->b before b->d), 1 before 3 (a->c before c->d)
	pos := make(map[int]int, 4)
	for i, idx := range order {
		pos[idx] = i
	}
	if pos[0] >= pos[2] {
		t.Errorf("transition 0 (a->b) must come before 2 (b->d)")
	}
	if pos[1] >= pos[3] {
		t.Errorf("transition 1 (a->c) must come before 3 (c->d)")
	}
}

func TestTopoOrderCycleDetection(t *testing.T) {
	p := &config.Pipeline{
		Stages: []config.Stage{
			{Workspace: "./a"},
			{Workspace: "./b"},
		},
		Transitions: []config.Transition{
			{From: "a", To: "b"},
			{From: "b", To: "a"},
		},
	}

	_, err := TopoOrder(p)
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestTopoOrderEmpty(t *testing.T) {
	p := &config.Pipeline{
		Stages: []config.Stage{
			{Workspace: "./a"},
		},
	}

	order, err := TopoOrder(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 0 {
		t.Errorf("order length = %d, want 0", len(order))
	}
}
