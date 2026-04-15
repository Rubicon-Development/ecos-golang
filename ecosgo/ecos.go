package ecos

// ECOS is vendored as a git submodule at ../ecos. Before `go build`, run
//     make -C ecos
// from the repo root to produce libecos.a / libecos_bb.a, or let the nix
// flake do it. ECOS must be built with the same integer size as these cgo
// defines (-DDLONG makes idxint = SuiteSparse_long); mismatches silently
// corrupt pointer arithmetic — that's how we got a segfault earlier.

/*
#cgo CFLAGS: -I${SRCDIR}/../ecos/include -I${SRCDIR}/../ecos/external/SuiteSparse_config -I${SRCDIR}/../ecos/external/amd/include -I${SRCDIR}/../ecos/external/ldl/include -DDLONG -DLDL_LONG
#cgo LDFLAGS: -L${SRCDIR}/../ecos -lecos -lm
#include <stdlib.h>
#include <stddef.h>
#include "ecos.h"
*/
import "C"
import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

// Exit Codes:
const (
	Optimal     = C.ECOS_OPTIMAL      // Problem solved to optimality
	PrimalInf   = C.ECOS_PINF         // Found certificate of primal infeasibility
	DualInf     = C.ECOS_DINF         // Found certificate of dual infeasibility
	InaccOffset = C.ECOS_INACC_OFFSET // Offset exitflag at inaccurate results
	MaxIter     = C.ECOS_MAXIT        // Maximum number of iterations reached
	Numerics    = C.ECOS_NUMERICS     // Search direction unreliable
	OutCone     = C.ECOS_OUTCONE      // s or z got outside the cone
	SigInt      = C.ECOS_SIGINT       // Solver interrupted
	Fatal       = C.ECOS_FATAL        // Unknown problem in solver
)

type Settings struct {
	Gamma        float64 // scaling the final step length
	Delta        float64 // regularization parameter
	Eps          float64 // regularization threshold
	FeasTol      float64 // primal/dual infeasibility tolerance
	AbsTol       float64 // absolute tolerance on duality gap
	RelTol       float64 // relative tolerance on duality gap
	FeasTolInacc float64 // primal/dual infeasibility relaxed tolerance
	AbsTolInacc  float64 // absolute relaxed tolerance on duality gap
	RelTolInacc  float64 // relative relaxed tolerance on duality gap
	NItRef       int     // number of iterative refinement steps
	MaxIt        int     // maximum number of iterations
	Verbose      int     // verbosity level
}

func DefaultSettings() *Settings {
	return &Settings{
		Gamma:        0.99,
		Delta:        2e-7,
		Eps:          1e-13,
		FeasTol:      1e-8,
		AbsTol:       1e-8,
		RelTol:       1e-8,
		FeasTolInacc: 1e-4,
		AbsTolInacc:  5e-5,
		RelTolInacc:  5e-5,
		NItRef:       9,
		MaxIt:        100,
		Verbose:      0,
	}
}

type Stats struct {
	PCost    float64 // primal objective
	DCost    float64 // dual objective
	Pres     float64 // primal residual
	Dres     float64 // dual residual
	Pinf     float64 // primal infeasibility
	Dinf     float64 // dual infeasibility
	PinfRes  float64 // primal infeasibility residual
	DinfRes  float64 // dual infeasibility residual
	Gap      float64 // duality gap
	RelGap   float64 // relative duality gap
	Sigma    float64
	Mu       float64
	Step     float64
	StepAff  float64
	KapOvert float64
	Iter     int // number of iterations
	NitRef1  int
	NitRef2  int
	NitRef3  int
}

type SparseMatrix struct {
	M    int       // number of rows
	N    int       // number of columns
	Nnz  int       // number of non-zero elements
	Data []float64 // non-zero values
	RowI []int     // row indices
	ColP []int     // column pointers
}

type Workspace struct {
	work        *C.pwork
	n           int // number of variables
	m           int // number of inequalities
	p           int // number of equalities
	allocations []unsafe.Pointer
}

type Problem struct {
	N      int           // number of variables
	M      int           // number of inequality constraints (dimension of G*x)
	P      int           // number of equality constraints
	L      int           // dimension of positive orthant
	NCones int           // number of second-order cones
	Q      []int         // dimensions of second-order cones
	NEX    int           // number of exponential cones
	G      *SparseMatrix // inequality constraint matrix
	A      *SparseMatrix // equality constraint matrix
	C      []float64     // objective function coefficients
	H      []float64     // inequality constraint RHS
	B      []float64     // equality constraint RHS
}

type Solution struct {
	X        []float64 // primal variables
	Y        []float64 // dual variables for equality constraints
	Z        []float64 // dual variables for inequality constraints
	S        []float64 // slack variables
	ExitFlag int       // exit status
	Stats    *Stats    // solver statistics
}

func copyPfloat(dst []float64, src *C.pfloat, n int) {
	if src == nil || n == 0 {
		return
	}
	copy(dst, unsafe.Slice((*float64)(unsafe.Pointer(src)), n))
}

func allocIdxintSlice(values []int) (*C.idxint, unsafe.Pointer, error) {
	if len(values) == 0 {
		return nil, nil, nil
	}
	bytes := C.size_t(len(values)) * C.size_t(unsafe.Sizeof(C.idxint(0)))
	ptr := C.malloc(bytes)
	if ptr == nil {
		return nil, nil, errors.New("failed to allocate idxint array")
	}
	for i, v := range values {
		*(*C.idxint)(unsafe.Add(ptr, uintptr(i)*unsafe.Sizeof(C.idxint(0)))) = C.idxint(v)
	}
	return (*C.idxint)(ptr), ptr, nil
}

func allocPfloatSlice(values []float64) (*C.pfloat, unsafe.Pointer, error) {
	if len(values) == 0 {
		return nil, nil, nil
	}
	bytes := C.size_t(len(values)) * C.size_t(unsafe.Sizeof(C.pfloat(0)))
	ptr := C.malloc(bytes)
	if ptr == nil {
		return nil, nil, errors.New("failed to allocate pfloat array")
	}
	for i, v := range values {
		*(*C.pfloat)(unsafe.Add(ptr, uintptr(i)*unsafe.Sizeof(C.pfloat(0)))) = C.pfloat(v)
	}
	return (*C.pfloat)(ptr), ptr, nil
}

func Setup(prob *Problem, settings *Settings) (*Workspace, error) {
	if prob == nil {
		return nil, errors.New("problem cannot be nil")
	}
	if prob.G == nil || len(prob.G.Data) == 0 {
		return nil, errors.New("G matrix is required")
	}
	if prob.G.M != prob.M || prob.G.N != prob.N {
		return nil, fmt.Errorf("G dimensions (%dx%d) do not match problem (%dx%d)", prob.G.M, prob.G.N, prob.M, prob.N)
	}
	if len(prob.G.ColP) != prob.N+1 {
		return nil, fmt.Errorf("G.ColP length %d, expected %d", len(prob.G.ColP), prob.N+1)
	}
	if len(prob.G.Data) != len(prob.G.RowI) {
		return nil, errors.New("G.Data and G.RowI length mismatch")
	}
	if len(prob.C) != prob.N {
		return nil, errors.New("c vector length must match N")
	}
	if len(prob.H) != prob.M {
		return nil, errors.New("h vector length must match M")
	}
	if prob.NCones > 0 && len(prob.Q) != prob.NCones {
		return nil, errors.New("q length must match NCones")
	}
	if prob.P > 0 {
		if prob.A == nil || len(prob.A.Data) == 0 {
			return nil, errors.New("A matrix required when P > 0")
		}
		if prob.A.M != prob.P || prob.A.N != prob.N {
			return nil, fmt.Errorf("A dimensions (%dx%d) do not match problem (%dx%d)", prob.A.M, prob.A.N, prob.P, prob.N)
		}
		if len(prob.A.ColP) != prob.N+1 {
			return nil, fmt.Errorf("A.ColP length %d, expected %d", len(prob.A.ColP), prob.N+1)
		}
		if len(prob.A.Data) != len(prob.A.RowI) {
			return nil, errors.New("A.Data and A.RowI length mismatch")
		}
		if len(prob.B) != prob.P {
			return nil, errors.New("b vector length must match P")
		}
	}

	ws := &Workspace{
		n:           prob.N,
		m:           prob.M,
		p:           prob.P,
		allocations: make([]unsafe.Pointer, 0),
	}

	qPtr, qAlloc, err := allocIdxintSlice(prob.Q)
	if err != nil {
		return nil, err
	}
	if qAlloc != nil {
		ws.allocations = append(ws.allocations, qAlloc)
	}

	gPr, gAlloc, err := allocPfloatSlice(prob.G.Data)
	if err != nil {
		ws.Cleanup()
		return nil, err
	}
	if gAlloc != nil {
		ws.allocations = append(ws.allocations, gAlloc)
	}

	gJc, gJcAlloc, err := allocIdxintSlice(prob.G.ColP)
	if err != nil {
		ws.Cleanup()
		return nil, err
	}
	if gJcAlloc != nil {
		ws.allocations = append(ws.allocations, gJcAlloc)
	}

	gIr, gIrAlloc, err := allocIdxintSlice(prob.G.RowI)
	if err != nil {
		ws.Cleanup()
		return nil, err
	}
	if gIrAlloc != nil {
		ws.allocations = append(ws.allocations, gIrAlloc)
	}

	var aPr *C.pfloat
	var aJc *C.idxint
	var aIr *C.idxint
	if prob.P > 0 {
		var aAlloc, aJcAlloc, aIrAlloc unsafe.Pointer
		aPr, aAlloc, err = allocPfloatSlice(prob.A.Data)
		if err != nil {
			ws.Cleanup()
			return nil, err
		}
		if aAlloc != nil {
			ws.allocations = append(ws.allocations, aAlloc)
		}

		aJc, aJcAlloc, err = allocIdxintSlice(prob.A.ColP)
		if err != nil {
			ws.Cleanup()
			return nil, err
		}
		if aJcAlloc != nil {
			ws.allocations = append(ws.allocations, aJcAlloc)
		}

		aIr, aIrAlloc, err = allocIdxintSlice(prob.A.RowI)
		if err != nil {
			ws.Cleanup()
			return nil, err
		}
		if aIrAlloc != nil {
			ws.allocations = append(ws.allocations, aIrAlloc)
		}
	}

	cPtr, cAlloc, err := allocPfloatSlice(prob.C)
	if err != nil {
		ws.Cleanup()
		return nil, err
	}
	if cAlloc != nil {
		ws.allocations = append(ws.allocations, cAlloc)
	}

	hPtr, hAlloc, err := allocPfloatSlice(prob.H)
	if err != nil {
		ws.Cleanup()
		return nil, err
	}
	if hAlloc != nil {
		ws.allocations = append(ws.allocations, hAlloc)
	}

	var bPtr *C.pfloat
	if prob.P > 0 {
		var bAlloc unsafe.Pointer
		bPtr, bAlloc, err = allocPfloatSlice(prob.B)
		if err != nil {
			ws.Cleanup()
			return nil, err
		}
		if bAlloc != nil {
			ws.allocations = append(ws.allocations, bAlloc)
		}
	}

	work := C.ECOS_setup(
		C.idxint(prob.N),
		C.idxint(prob.M),
		C.idxint(prob.P),
		C.idxint(prob.L),
		C.idxint(prob.NCones),
		qPtr,
		C.idxint(prob.NEX),
		gPr, gJc, gIr,
		aPr, aJc, aIr,
		cPtr, hPtr, bPtr,
	)

	if work == nil {
		ws.Cleanup()
		return nil, errors.New("failed to setup ECOS workspace")
	}

	if work.stgs != nil {
		if settings != nil {
			work.stgs.gamma = C.pfloat(settings.Gamma)
			work.stgs.delta = C.pfloat(settings.Delta)
			work.stgs.eps = C.pfloat(settings.Eps)
			work.stgs.feastol = C.pfloat(settings.FeasTol)
			work.stgs.abstol = C.pfloat(settings.AbsTol)
			work.stgs.reltol = C.pfloat(settings.RelTol)
			work.stgs.feastol_inacc = C.pfloat(settings.FeasTolInacc)
			work.stgs.abstol_inacc = C.pfloat(settings.AbsTolInacc)
			work.stgs.reltol_inacc = C.pfloat(settings.RelTolInacc)
			work.stgs.nitref = C.idxint(settings.NItRef)
			work.stgs.maxit = C.idxint(settings.MaxIt)
			work.stgs.verbose = C.idxint(settings.Verbose)
		} else {
			// Quiet by default; ECOS's C default is verbose=1 which floods stdout.
			work.stgs.verbose = 0
		}
	}

	ws.work = work
	runtime.SetFinalizer(ws, (*Workspace).Cleanup)
	return ws, nil
}

func (w *Workspace) Solve() (*Solution, error) {
	if w == nil || w.work == nil {
		return nil, errors.New("workspace is not initialized")
	}
	exitFlag := int(C.ECOS_solve(w.work))

	sol := &Solution{
		X:        make([]float64, w.n),
		Y:        make([]float64, w.p),
		Z:        make([]float64, w.m),
		S:        make([]float64, w.m),
		ExitFlag: exitFlag,
	}

	// Copy solution vectors and statistics. pfloat is double, so it
	// is layout-compatible with float64 and we can bulk-copy.
	copyPfloat(sol.X, w.work.x, w.n)
	copyPfloat(sol.Y, w.work.y, w.p)
	copyPfloat(sol.Z, w.work.z, w.m)
	copyPfloat(sol.S, w.work.s, w.m)

	if w.work.info != nil {
		sol.Stats = &Stats{
			PCost:    float64(w.work.info.pcost),
			DCost:    float64(w.work.info.dcost),
			Pres:     float64(w.work.info.pres),
			Dres:     float64(w.work.info.dres),
			Pinf:     float64(w.work.info.pinf),
			Dinf:     float64(w.work.info.dinf),
			PinfRes:  float64(w.work.info.pinfres),
			DinfRes:  float64(w.work.info.dinfres),
			Gap:      float64(w.work.info.gap),
			RelGap:   float64(w.work.info.relgap),
			Sigma:    float64(w.work.info.sigma),
			Mu:       float64(w.work.info.mu),
			Step:     float64(w.work.info.step),
			StepAff:  float64(w.work.info.step_aff),
			KapOvert: float64(w.work.info.kapovert),
			Iter:     int(w.work.info.iter),
			NitRef1:  int(w.work.info.nitref1),
			NitRef2:  int(w.work.info.nitref2),
			NitRef3:  int(w.work.info.nitref3),
		}
	}

	return sol, nil
}

func (w *Workspace) Cleanup() {
	if w == nil {
		return
	}
	if w.work != nil {
		C.ECOS_cleanup(w.work, 0)
		w.work = nil
	}
	for _, ptr := range w.allocations {
		if ptr != nil {
			C.free(ptr)
		}
	}
	w.allocations = nil
	runtime.SetFinalizer(w, nil)
}

func Version() string {
	return C.GoString(C.ECOS_ver())
}

func NewSparseMatrixFromDense(dense [][]float64) *SparseMatrix {
	if len(dense) == 0 || len(dense[0]) == 0 {
		return nil
	}

	m := len(dense)
	n := len(dense[0])

	for i := 1; i < m; i++ {
		if len(dense[i]) != n {
			return nil
		}
	}

	// Count non-zeros
	nnz := 0
	for i := 0; i < m; i++ {
		for j := 0; j < n; j++ {
			if dense[i][j] != 0 {
				nnz++
			}
		}
	}

	sm := &SparseMatrix{
		M:    m,
		N:    n,
		Nnz:  nnz,
		Data: make([]float64, nnz),
		RowI: make([]int, nnz),
		ColP: make([]int, n+1),
	}

	idx := 0
	for j := 0; j < n; j++ {
		sm.ColP[j] = idx
		for i := 0; i < m; i++ {
			if dense[i][j] != 0 {
				sm.Data[idx] = dense[i][j]
				sm.RowI[idx] = i
				idx++
			}
		}
	}
	sm.ColP[n] = idx
	return sm
}

func NewSparseMatrixFromTriplet(m, n int, rows, cols []int, vals []float64) (*SparseMatrix, error) {
	if len(rows) != len(cols) || len(rows) != len(vals) {
		return nil, errors.New("rows, cols, and vals must have the same length")
	}

	nnz := len(vals)

	sm := &SparseMatrix{
		M:    m,
		N:    n,
		Nnz:  nnz,
		Data: make([]float64, nnz),
		RowI: make([]int, nnz),
		ColP: make([]int, n+1),
	}

	// Count entries per column
	colCounts := make([]int, n)
	for k := 0; k < nnz; k++ {
		if cols[k] >= n || cols[k] < 0 {
			return nil, fmt.Errorf("column index %d out of bounds", cols[k])
		}
		if rows[k] >= m || rows[k] < 0 {
			return nil, fmt.Errorf("row index %d out of bounds", rows[k])
		}
		colCounts[cols[k]]++
	}

	sm.ColP[0] = 0
	for j := 1; j <= n; j++ {
		sm.ColP[j] = sm.ColP[j-1] + colCounts[j-1]
	}

	for j := 0; j < n; j++ {
		colCounts[j] = 0
	}

	for k := 0; k < nnz; k++ {
		col := cols[k]
		idx := sm.ColP[col] + colCounts[col]
		sm.Data[idx] = vals[k]
		sm.RowI[idx] = rows[k]
		colCounts[col]++
	}

	return sm, nil
}

func ExitCodeString(exitCode int) string {
	switch exitCode {
	case Optimal:
		return "Optimal solution found"
	case PrimalInf:
		return "Certificate of primal infeasibility found"
	case DualInf:
		return "Certificate of dual infeasibility found"
	case Optimal + InaccOffset:
		return "Optimal solution found (reduced accuracy)"
	case PrimalInf + InaccOffset:
		return "Certificate of primal infeasibility found (reduced accuracy)"
	case DualInf + InaccOffset:
		return "Certificate of dual infeasibility found (reduced accuracy)"
	case MaxIter:
		return "Maximum number of iterations reached"
	case Numerics:
		return "Numerical problems (unreliable search direction)"
	case OutCone:
		return "Numerical problems (slacks or multipliers outside cone)"
	case SigInt:
		return "Interrupted by signal or CTRL-C"
	case Fatal:
		return "Unknown problem in solver"
	default:
		return fmt.Sprintf("Unknown exit code: %d", exitCode)
	}
}
