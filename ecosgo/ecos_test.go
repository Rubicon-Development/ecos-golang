package ecos

import (
	"math"
	"runtime"
	"testing"
)

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

// Minimize -x subject to 0 <= x <= 1. Optimum: x = 1, obj = -1.
func TestLPKnownOptimum(t *testing.T) {
	prob := &Problem{
		N: 1, M: 2, P: 0, L: 2,
		G: &SparseMatrix{
			M: 2, N: 1, Nnz: 2,
			Data: []float64{1, -1}, RowI: []int{0, 1}, ColP: []int{0, 2},
		},
		C: []float64{-1},
		H: []float64{1, 0},
	}
	ws, err := Setup(prob, nil)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer ws.Cleanup()
	sol, err := ws.Solve()
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if sol.ExitFlag != Optimal {
		t.Fatalf("exit=%d (%s)", sol.ExitFlag, ExitCodeString(sol.ExitFlag))
	}
	if math.Abs(sol.X[0]-1.0) > 1e-6 {
		t.Fatalf("x=%v, want 1", sol.X[0])
	}
	if sol.Stats == nil || math.Abs(sol.Stats.PCost-(-1.0)) > 1e-6 {
		t.Fatalf("pcost=%v, want -1", sol.Stats.PCost)
	}
}

// Minimize x+y subject to x+y=1, x>=0, y>=0. Optimum: any split, obj=1.
func TestLPWithEquality(t *testing.T) {
	prob := &Problem{
		N: 2, M: 2, P: 1, L: 2,
		G: &SparseMatrix{
			M: 2, N: 2, Nnz: 2,
			Data: []float64{-1, -1}, RowI: []int{0, 1}, ColP: []int{0, 1, 2},
		},
		A: &SparseMatrix{
			M: 1, N: 2, Nnz: 2,
			Data: []float64{1, 1}, RowI: []int{0, 0}, ColP: []int{0, 1, 2},
		},
		C: []float64{1, 1},
		H: []float64{0, 0},
		B: []float64{1},
	}
	ws, err := Setup(prob, nil)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer ws.Cleanup()
	sol, err := ws.Solve()
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if sol.ExitFlag != Optimal {
		t.Fatalf("exit=%d (%s)", sol.ExitFlag, ExitCodeString(sol.ExitFlag))
	}
	if math.Abs(sol.X[0]+sol.X[1]-1.0) > 1e-6 {
		t.Fatalf("x+y=%v, want 1", sol.X[0]+sol.X[1])
	}
	if math.Abs(sol.Stats.PCost-1.0) > 1e-6 {
		t.Fatalf("pcost=%v, want 1", sol.Stats.PCost)
	}
}

// x <= -1 and x >= 1 simultaneously — primal infeasible.
func TestInfeasibleProblem(t *testing.T) {
	prob := &Problem{
		N: 1, M: 2, P: 0, L: 2,
		G: &SparseMatrix{
			M: 2, N: 1, Nnz: 2,
			Data: []float64{1, -1}, RowI: []int{0, 1}, ColP: []int{0, 2},
		},
		C: []float64{0},
		H: []float64{-1, -1},
	}
	ws, err := Setup(prob, nil)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer ws.Cleanup()
	sol, err := ws.Solve()
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if sol.ExitFlag != PrimalInf && sol.ExitFlag != PrimalInf+InaccOffset {
		t.Fatalf("expected primal infeasible, got %d (%s)", sol.ExitFlag, ExitCodeString(sol.ExitFlag))
	}
}

func TestSetupValidation(t *testing.T) {
	cases := []struct {
		name string
		prob *Problem
	}{
		{
			name: "G dim mismatch",
			prob: &Problem{
				N: 2, M: 2, P: 0, L: 2,
				G: &SparseMatrix{ // wrong N
					M: 2, N: 1, Nnz: 1,
					Data: []float64{1}, RowI: []int{0}, ColP: []int{0, 1},
				},
				C: []float64{1, 1}, H: []float64{1, 1},
			},
		},
		{
			name: "ColP length wrong",
			prob: &Problem{
				N: 2, M: 2, P: 0, L: 2,
				G: &SparseMatrix{
					M: 2, N: 2, Nnz: 1,
					Data: []float64{1}, RowI: []int{0}, ColP: []int{0, 1},
				},
				C: []float64{1, 1}, H: []float64{1, 1},
			},
		},
		{
			name: "C wrong length",
			prob: &Problem{
				N: 2, M: 2, P: 0, L: 2,
				G: &SparseMatrix{
					M: 2, N: 2, Nnz: 2,
					Data: []float64{1, 1}, RowI: []int{0, 1}, ColP: []int{0, 1, 2},
				},
				C: []float64{1}, H: []float64{1, 1},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Setup(tc.prob, nil); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

// Minimize -x subject to x <= 0.7, x in {0,1}. LP: x=0.7. MIP: x=0.
func TestBBBooleanKnownOptimum(t *testing.T) {
	prob := &Problem{
		N: 1, M: 1, P: 0, L: 1,
		G: &SparseMatrix{
			M: 1, N: 1, Nnz: 1,
			Data: []float64{1}, RowI: []int{0}, ColP: []int{0, 1},
		},
		C: []float64{-1},
		H: []float64{0.7},
	}
	ws, err := BBSetup(&BBProblem{Problem: prob, BoolVarsIdx: []int{0}})
	if err != nil {
		t.Fatalf("BB setup: %v", err)
	}
	defer ws.Cleanup()
	sol, err := ws.Solve()
	if err != nil {
		t.Fatalf("BB solve: %v", err)
	}
	if sol.ExitFlag != Optimal {
		t.Fatalf("exit=%d (%s)", sol.ExitFlag, ExitCodeString(sol.ExitFlag))
	}
	if math.Abs(sol.X[0]) > 1e-6 {
		t.Fatalf("x=%v, want 0 (MIP picks 0, not LP's 0.7)", sol.X[0])
	}
}

// Minimize 3x+y+2*b s.t. x+y+10*b >= 2, 0<=x,y<=1, b in {0,1}.
// LP relaxation (b can be fractional): b=0.2 makes 10b=2, obj=0.4.
// MIP (b∈{0,1}): either b=1 (obj = 0+0+2 = 2 with x=y=0) or b=0 (needs x+y>=2 but max 2, obj=3+1=4). Best: b=1, obj=2.
func TestBBDistinguishesFromLP(t *testing.T) {
	// vars: [x, y, b], n=3
	// constraints as Gz <= h with z = [x,y,b]:
	//   -x - y - 10b <= -2        (flip of x+y+10b >= 2)
	//   x <= 1
	//   -x <= 0
	//   y <= 1
	//   -y <= 0
	// (b bounds come from ECOS_BB boolean handling)
	// In CCS column-major by variable:
	//   col x: rows [0,1,2] vals [-1, 1, -1]
	//   col y: rows [0,3,4] vals [-1, 1, -1]
	//   col b: rows [0]     vals [-10]
	prob := &Problem{
		N: 3, M: 5, P: 0, L: 5,
		G: &SparseMatrix{
			M: 5, N: 3, Nnz: 7,
			Data: []float64{-1, 1, -1, -1, 1, -1, -10},
			RowI: []int{0, 1, 2, 0, 3, 4, 0},
			ColP: []int{0, 3, 6, 7},
		},
		C: []float64{3, 1, 2},
		H: []float64{-2, 1, 0, 1, 0},
	}

	// First solve with BB to confirm MIP optimum is 2.
	bb, err := BBSetup(&BBProblem{Problem: prob, BoolVarsIdx: []int{2}})
	if err != nil {
		t.Fatalf("BB setup: %v", err)
	}
	defer bb.Cleanup()
	bbSol, err := bb.Solve()
	if err != nil {
		t.Fatalf("BB solve: %v", err)
	}
	if bbSol.ExitFlag != Optimal {
		t.Fatalf("BB exit=%d (%s)", bbSol.ExitFlag, ExitCodeString(bbSol.ExitFlag))
	}
	if math.Abs(bbSol.X[2]-1.0) > 1e-6 {
		t.Fatalf("BB chose b=%v, want 1", bbSol.X[2])
	}
	if math.Abs(bbSol.Stats.PCost-2.0) > 1e-4 {
		t.Fatalf("BB obj=%v, want 2", bbSol.Stats.PCost)
	}

	// Now solve the same problem as LP (no integrality) and confirm obj < 2,
	// which proves the BB test isn't trivially optimal as an LP too.
	lp, err := Setup(prob, nil)
	if err != nil {
		t.Fatalf("LP setup: %v", err)
	}
	defer lp.Cleanup()
	lpSol, err := lp.Solve()
	if err != nil {
		t.Fatalf("LP solve: %v", err)
	}
	if lpSol.ExitFlag != Optimal {
		t.Fatalf("LP exit=%d", lpSol.ExitFlag)
	}
	if lpSol.Stats.PCost >= 2.0-1e-4 {
		t.Fatalf("LP obj=%v should be strictly less than 2 (else test is degenerate)",
			lpSol.Stats.PCost)
	}
}

// Hammer setup/solve/cleanup to shake out leaks and double-free issues.
func TestStressRepeatedSolve(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped in -short mode")
	}
	const iters = 500
	for i := 0; i < iters; i++ {
		prob := &Problem{
			N: 1, M: 2, P: 0, L: 2,
			G: &SparseMatrix{
				M: 2, N: 1, Nnz: 2,
				Data: []float64{1, -1}, RowI: []int{0, 1}, ColP: []int{0, 2},
			},
			C: []float64{-1},
			H: []float64{1, 0},
		}
		ws, err := Setup(prob, nil)
		if err != nil {
			t.Fatalf("iter %d setup: %v", i, err)
		}
		sol, err := ws.Solve()
		if err != nil {
			t.Fatalf("iter %d solve: %v", i, err)
		}
		if sol.ExitFlag != Optimal {
			t.Fatalf("iter %d non-optimal: %d", i, sol.ExitFlag)
		}
		ws.Cleanup()
	}
}

// Cleanup must be idempotent — multiple calls and finalizer after explicit
// cleanup must not double-free the cgo workspace.
func TestCleanupIdempotent(t *testing.T) {
	prob := &Problem{
		N: 1, M: 2, P: 0, L: 2,
		G: &SparseMatrix{
			M: 2, N: 1, Nnz: 2,
			Data: []float64{1, -1}, RowI: []int{0, 1}, ColP: []int{0, 2},
		},
		C: []float64{0}, H: []float64{1, 0},
	}
	ws, err := Setup(prob, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Solve(); err != nil {
		t.Fatal(err)
	}
	ws.Cleanup()
	ws.Cleanup() // must be a no-op, not a double free
	ws.Cleanup()
}

// Workspaces that are discarded without Cleanup should be reclaimed by the
// finalizer without crashing the runtime.
func TestFinalizerReclaim(t *testing.T) {
	if testing.Short() {
		t.Skip("finalizer test skipped in -short mode")
	}
	for i := 0; i < 50; i++ {
		func() {
			prob := &Problem{
				N: 1, M: 2, P: 0, L: 2,
				G: &SparseMatrix{
					M: 2, N: 1, Nnz: 2,
					Data: []float64{1, -1}, RowI: []int{0, 1}, ColP: []int{0, 2},
				},
				C: []float64{-1}, H: []float64{1, 0},
			}
			ws, err := Setup(prob, nil)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			if _, err := ws.Solve(); err != nil {
				t.Fatalf("solve: %v", err)
			}
			// deliberately no Cleanup — rely on finalizer
			_ = ws
		}()
	}
	runtime.GC()
	runtime.GC()
}
