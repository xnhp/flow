// Package pipeline implements the DAG ordering and advance logic for flow.
package pipeline

import (
	"fmt"

	"flow/internal/config"
)

// TopoOrder computes a topological ordering of transitions based on stage dependencies.
// Returns transition indices in the order they should be evaluated.
// Returns an error if the graph contains a cycle.
func TopoOrder(p *config.Pipeline) ([]int, error) {
	// Build adjacency: stage name -> list of transition indices that consume from it.
	// We want transitions ordered so that if transition T1 outputs to stage S,
	// and transition T2 reads from stage S, then T1 comes before T2.

	stageNames := make(map[string]bool, len(p.Stages))
	for _, s := range p.Stages {
		stageNames[config.StageName(s)] = true
	}

	n := len(p.Transitions)
	if n == 0 {
		return nil, nil
	}

	// For each transition, compute dependencies: a transition T depends on
	// any transition that outputs to T's source stage.
	producedBy := make(map[string][]int) // stage name -> transitions that write to it
	for i, t := range p.Transitions {
		producedBy[t.To] = append(producedBy[t.To], i)
	}

	// deps[i] = set of transition indices that must run before transition i
	deps := make([]map[int]bool, n)
	for i := range deps {
		deps[i] = make(map[int]bool)
	}
	for i, t := range p.Transitions {
		// Transition i reads from t.From. Any transition that writes to t.From must run first.
		for _, j := range producedBy[t.From] {
			if j != i {
				deps[i][j] = true
			}
		}
	}

	// Kahn's algorithm
	inDegree := make([]int, n)
	for i := range deps {
		inDegree[i] = len(deps[i])
	}

	var queue []int
	for i, d := range inDegree {
		if d == 0 {
			queue = append(queue, i)
		}
	}

	var order []int
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		order = append(order, cur)

		// For each transition that depends on cur, decrement in-degree.
		for i := range deps {
			if deps[i][cur] {
				inDegree[i]--
				if inDegree[i] == 0 {
					queue = append(queue, i)
				}
			}
		}
	}

	if len(order) != n {
		return nil, fmt.Errorf("cycle detected in transition graph")
	}

	return order, nil
}
