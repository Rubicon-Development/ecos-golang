package main

import (
	"fmt"

	ecos "github.com/ecos-golang/ecosgo"
)

// Example: Solve a simple SOCP problem
// minimize    c'*x
// subject to  Ax = b
//
//	Gx <= h (in cone K)
func main() {
	fmt.Println("=== ECOS Go Wrapper Example ===")
	fmt.Printf("ECOS Version: %s\n\n", ecos.Version())

	// Feasibility LP problem (from ECOS test suite)
	// minimize    x
	// subject to  0 <= x <= 1

	n := 1 // number of variables
	m := 2 // number of inequality constraints
	p := 0 // no equality constraints
	l := 2 // dimension of positive orthant
	ncones := 0
	q := []int{}

	// Objective: minimize 0 (feasibility test)
	c := []float64{0.0}

	// Constraints encoded as Gx <= h:
	// x <= 1
	// -x <= 0
	G := &ecos.SparseMatrix{
		M:    m,
		N:    n,
		Nnz:  2,
		Data: []float64{1.0, -1.0},
		RowI: []int{0, 1},
		ColP: []int{0, 2},
	}

	h := []float64{1.0, 0.0}

	// Create problem
	prob := &ecos.Problem{
		N:      n,
		M:      m,
		P:      p,
		L:      l,
		NCones: ncones,
		Q:      q,
		NEX:    0,
		G:      G,
		A:      nil,
		C:      c,
		H:      h,
		B:      nil,
	}

	// Setup and solve
	ws, err := ecos.Setup(prob, nil)
	if err != nil {
		fmt.Printf("Error setting up ECOS: %v\n", err)
		return
	}
	defer ws.Cleanup()

	sol, err := ws.Solve()
	if err != nil {
		fmt.Printf("Error solving: %v\n", err)
		return
	}

	// Print results
	fmt.Printf("\nResults:\n")
	fmt.Printf("Status: %s\n", ecos.ExitCodeString(sol.ExitFlag))
	if sol.Stats != nil {
		fmt.Printf("Iterations: %d\n", sol.Stats.Iter)
		fmt.Printf("Optimal value: %.6f\n", sol.Stats.PCost)
	}
	if len(sol.X) > 0 {
		fmt.Printf("Solution: x = [%.6f]\n", sol.X[0])
	}
}
