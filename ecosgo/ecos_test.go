package ecos

import "testing"

func TestFeasibilityProblem(t *testing.T) {
	// Based on ecos/test/feasibilityProblems/feas.h
	prob := &Problem{
		N:      1,
		M:      2,
		P:      0,
		L:      2,
		NCones: 0,
		Q:      nil,
		NEX:    0,
		G: &SparseMatrix{
			M:    2,
			N:    1,
			Nnz:  2,
			Data: []float64{1, -1},
			RowI: []int{0, 1},
			ColP: []int{0, 2},
		},
		A: nil,
		C: []float64{0},
		H: []float64{1, 0},
		B: nil,
	}

	ws, err := Setup(prob, nil)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer ws.Cleanup()

	sol, err := ws.Solve()
	if err != nil {
		t.Fatalf("solve failed: %v", err)
	}

	if sol.ExitFlag != int(Optimal) {
		t.Fatalf("expected exit flag %d, got %d", Optimal, sol.ExitFlag)
	}
}
