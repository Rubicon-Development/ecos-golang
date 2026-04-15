package ecos

/*
#cgo CFLAGS: -DDLONG -DLDL_LONG
#cgo LDFLAGS: -lecos_bb
#include <stdlib.h>
#include <stddef.h>
#include "ecos_bb.h"
*/
import "C"
import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

// BBProblem extends Problem with boolean/integer variable indices for
// mixed-integer conic programs. ECOS_BB enforces bool_vars ∈ {0,1} and
// int_vars ∈ ℤ via branch-and-bound around the ECOS relaxation.
type BBProblem struct {
	*Problem
	BoolVarsIdx []int // variable indices constrained to {0, 1}
	IntVarsIdx  []int // variable indices constrained to integers
}

type BBWorkspace struct {
	work        *C.ecos_bb_pwork
	n           int
	m           int
	p           int
	allocations []unsafe.Pointer
}

func (ws *BBWorkspace) track(ptr unsafe.Pointer) {
	if ptr != nil {
		ws.allocations = append(ws.allocations, ptr)
	}
}

// BBSetup allocates an ECOS_BB workspace. Settings are left at ECOS_BB
// defaults; call ECOS_BB with tuned parameters is not yet exposed.
func BBSetup(prob *BBProblem) (*BBWorkspace, error) {
	if prob == nil || prob.Problem == nil {
		return nil, errors.New("problem cannot be nil")
	}
	p := prob.Problem

	if p.G == nil || len(p.G.Data) == 0 {
		return nil, errors.New("G matrix is required")
	}
	if p.G.M != p.M || p.G.N != p.N {
		return nil, fmt.Errorf("G dimensions (%dx%d) do not match problem (%dx%d)", p.G.M, p.G.N, p.M, p.N)
	}
	if len(p.G.ColP) != p.N+1 {
		return nil, fmt.Errorf("G.ColP length %d, expected %d", len(p.G.ColP), p.N+1)
	}
	if len(p.G.Data) != len(p.G.RowI) {
		return nil, errors.New("G.Data and G.RowI length mismatch")
	}
	if len(p.C) != p.N {
		return nil, errors.New("c vector length must match N")
	}
	if len(p.H) != p.M {
		return nil, errors.New("h vector length must match M")
	}
	if p.NCones > 0 && len(p.Q) != p.NCones {
		return nil, errors.New("q length must match NCones")
	}
	if p.P > 0 {
		if p.A == nil || len(p.A.Data) == 0 {
			return nil, errors.New("A matrix required when P > 0")
		}
		if p.A.M != p.P || p.A.N != p.N {
			return nil, fmt.Errorf("A dimensions (%dx%d) do not match problem (%dx%d)", p.A.M, p.A.N, p.P, p.N)
		}
		if len(p.A.ColP) != p.N+1 {
			return nil, fmt.Errorf("A.ColP length %d, expected %d", len(p.A.ColP), p.N+1)
		}
		if len(p.A.Data) != len(p.A.RowI) {
			return nil, errors.New("A.Data and A.RowI length mismatch")
		}
		if len(p.B) != p.P {
			return nil, errors.New("b vector length must match P")
		}
	}
	for _, idx := range prob.BoolVarsIdx {
		if idx < 0 || idx >= p.N {
			return nil, fmt.Errorf("bool var index %d out of range [0,%d)", idx, p.N)
		}
	}
	for _, idx := range prob.IntVarsIdx {
		if idx < 0 || idx >= p.N {
			return nil, fmt.Errorf("int var index %d out of range [0,%d)", idx, p.N)
		}
	}

	ws := &BBWorkspace{n: p.N, m: p.M, p: p.P}

	qPtr, qAlloc, err := allocIdxintSlice(p.Q)
	if err != nil {
		return nil, err
	}
	ws.track(qAlloc)

	gPr, a, err := allocPfloatSlice(p.G.Data)
	if err != nil {
		ws.Cleanup()
		return nil, err
	}
	ws.track(a)
	gJc, a, err := allocIdxintSlice(p.G.ColP)
	if err != nil {
		ws.Cleanup()
		return nil, err
	}
	ws.track(a)
	gIr, a, err := allocIdxintSlice(p.G.RowI)
	if err != nil {
		ws.Cleanup()
		return nil, err
	}
	ws.track(a)

	var aPr *C.pfloat
	var aJc, aIr *C.idxint
	if p.P > 0 {
		aPr, a, err = allocPfloatSlice(p.A.Data)
		if err != nil {
			ws.Cleanup()
			return nil, err
		}
		ws.track(a)
		aJc, a, err = allocIdxintSlice(p.A.ColP)
		if err != nil {
			ws.Cleanup()
			return nil, err
		}
		ws.track(a)
		aIr, a, err = allocIdxintSlice(p.A.RowI)
		if err != nil {
			ws.Cleanup()
			return nil, err
		}
		ws.track(a)
	}

	cPtr, a, err := allocPfloatSlice(p.C)
	if err != nil {
		ws.Cleanup()
		return nil, err
	}
	ws.track(a)
	hPtr, a, err := allocPfloatSlice(p.H)
	if err != nil {
		ws.Cleanup()
		return nil, err
	}
	ws.track(a)

	var bPtr *C.pfloat
	if p.P > 0 {
		bPtr, a, err = allocPfloatSlice(p.B)
		if err != nil {
			ws.Cleanup()
			return nil, err
		}
		ws.track(a)
	}

	boolPtr, a, err := allocIdxintSlice(prob.BoolVarsIdx)
	if err != nil {
		ws.Cleanup()
		return nil, err
	}
	ws.track(a)
	intPtr, a, err := allocIdxintSlice(prob.IntVarsIdx)
	if err != nil {
		ws.Cleanup()
		return nil, err
	}
	ws.track(a)

	work := C.ECOS_BB_setup(
		C.idxint(p.N), C.idxint(p.M), C.idxint(p.P),
		C.idxint(p.L), C.idxint(p.NCones), qPtr, C.idxint(p.NEX),
		gPr, gJc, gIr,
		aPr, aJc, aIr,
		cPtr, hPtr, bPtr,
		C.idxint(len(prob.BoolVarsIdx)), boolPtr,
		C.idxint(len(prob.IntVarsIdx)), intPtr,
		nil, // use ECOS_BB default settings
	)
	if work == nil {
		ws.Cleanup()
		return nil, errors.New("failed to setup ECOS_BB workspace")
	}

	_ = qPtr // qPtr may be nil; keep linter quiet
	ws.work = work
	runtime.SetFinalizer(ws, (*BBWorkspace).Cleanup)
	return ws, nil
}

// Solve runs the branch-and-bound loop and returns the best integral
// solution found (or the LP relaxation if BB hit iter limit).
func (w *BBWorkspace) Solve() (*Solution, error) {
	if w == nil || w.work == nil {
		return nil, errors.New("workspace is not initialized")
	}
	exitFlag := int(C.ECOS_BB_solve(w.work))

	sol := &Solution{
		X:        make([]float64, w.n),
		Y:        make([]float64, w.p),
		Z:        make([]float64, w.m),
		S:        make([]float64, w.m),
		ExitFlag: exitFlag,
	}
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

func (w *BBWorkspace) Cleanup() {
	if w == nil {
		return
	}
	if w.work != nil {
		C.ECOS_BB_cleanup(w.work, 0)
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
