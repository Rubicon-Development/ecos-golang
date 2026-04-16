// Package client posts optimization problems (DSL text or JSON) to a
// matrix-server endpoint, then solves the resulting LP/SOCP/MIP with ECOS.
//
// Two entry points:
//
//	result, err := client.SolveDSL("http://host:8080", dslBytes)
//	result, err := client.SolveJSON("http://host:8080", jsonBytes)
//
// Both return a *Result with named variables, objective value, and solver
// status. The caller never touches ECOS directly.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	ecos "github.com/ecos-golang/ecosgo"
)

// Result holds the solution returned by ECOS after solving.
type Result struct {
	Status      string         `json:"status"`
	ExitFlag    int            `json:"exit_flag"`
	Objective   float64        `json:"objective"`
	Iterations  int            `json:"iterations"`
	Variables   map[string]any `json:"variables"`
	X           []float64      `json:"x"`
	ColumnNames []string       `json:"column_names,omitempty"`
}

// SolveDSL posts DSL text to baseURL/v1/dsl and solves with ECOS.
func SolveDSL(baseURL string, dsl []byte) (*Result, error) {
	return solveVia(baseURL+"/v1/dsl", "text/plain", dsl)
}

// SolveJSON posts a JSON problem definition to baseURL/v1/json and solves with ECOS.
func SolveJSON(baseURL string, jsonInput []byte) (*Result, error) {
	return solveVia(baseURL+"/v1/json", "application/json", jsonInput)
}

func solveVia(endpoint, contentType string, body []byte) (*Result, error) {
	resp, err := fetchMatrices(endpoint, contentType, body)
	if err != nil {
		return nil, err
	}
	return solve(resp)
}

// sparseJSON is the wire format for CCS sparse matrices.
type sparseJSON struct {
	Pr []float64 `json:"pr"`
	Jc []int     `json:"jc"`
	Ir []int     `json:"ir"`
}

func (s *sparseJSON) toSparse(m, n int) *ecos.SparseMatrix {
	if s == nil {
		return nil
	}
	return &ecos.SparseMatrix{
		M:    m,
		N:    n,
		Nnz:  len(s.Pr),
		Data: s.Pr,
		RowI: s.Ir,
		ColP: s.Jc,
	}
}

// problemResponse is the JSON returned by both /v1/dsl and /v1/json.
type problemResponse struct {
	N               int         `json:"n"`
	M               int         `json:"m"`
	P               int         `json:"p"`
	L               int         `json:"l"`
	Q               []int       `json:"q"`
	E               int         `json:"e"`
	G               *sparseJSON `json:"g"`
	A               *sparseJSON `json:"a"`
	C               []float64   `json:"c"`
	H               []float64   `json:"h"`
	B               []float64   `json:"b"`
	BoolVarsIdx     []int       `json:"bool_vars_idx"`
	IntVarsIdx      []int       `json:"int_vars_idx"`
	ColumnNames     []string    `json:"column_names"`
	ObjectiveOffset float64     `json:"objective_offset"`
}

func fetchMatrices(endpoint, contentType string, payload []byte) (*problemResponse, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)

	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("endpoint returned %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	var out problemResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode JSON: %w (body: %s)", err, body)
	}
	return &out, nil
}

func solve(r *problemResponse) (*Result, error) {
	prob := &ecos.Problem{
		N:      r.N,
		M:      r.M,
		P:      r.P,
		L:      r.L,
		NCones: len(r.Q),
		Q:      r.Q,
		NEX:    r.E,
		G:      r.G.toSparse(r.M, r.N),
		A:      r.A.toSparse(r.P, r.N),
		C:      r.C,
		H:      r.H,
		B:      r.B,
	}

	hasIntegrality := len(r.BoolVarsIdx) > 0 || len(r.IntVarsIdx) > 0

	var sol *ecos.Solution
	if hasIntegrality {
		ws, err := ecos.BBSetup(&ecos.BBProblem{
			Problem:     prob,
			BoolVarsIdx: r.BoolVarsIdx,
			IntVarsIdx:  r.IntVarsIdx,
		})
		if err != nil {
			return nil, fmt.Errorf("ECOS_BB setup: %w", err)
		}
		defer ws.Cleanup()
		sol, err = ws.Solve()
		if err != nil {
			return nil, err
		}
	} else {
		ws, err := ecos.Setup(prob, nil)
		if err != nil {
			return nil, fmt.Errorf("ECOS setup: %w", err)
		}
		defer ws.Cleanup()
		sol, err = ws.Solve()
		if err != nil {
			return nil, err
		}
	}

	objective := r.ObjectiveOffset
	iterations := 0
	if sol.Stats != nil {
		objective += sol.Stats.PCost
		iterations = sol.Stats.Iter
	}

	return &Result{
		Status:      ecos.ExitCodeString(sol.ExitFlag),
		ExitFlag:    sol.ExitFlag,
		Objective:   objective,
		Iterations:  iterations,
		Variables:   variableMap(r.ColumnNames, sol.X, r.BoolVarsIdx, r.IntVarsIdx),
		X:           sol.X,
		ColumnNames: r.ColumnNames,
	}, nil
}

func variableMap(names []string, x []float64, boolIdx, intIdx []int) map[string]any {
	boolSet := indexSet(boolIdx)
	intSet := indexSet(intIdx)
	out := make(map[string]any, len(x))
	for i, v := range x {
		name := fmt.Sprintf("x[%d]", i)
		if i < len(names) {
			name = names[i]
		}
		switch {
		case boolSet[i]:
			out[name] = roundInt(v) != 0
		case intSet[i]:
			out[name] = roundInt(v)
		default:
			out[name] = v
		}
	}
	return out
}

func indexSet(idx []int) map[int]bool {
	s := make(map[int]bool, len(idx))
	for _, i := range idx {
		s[i] = true
	}
	return s
}

func roundInt(v float64) int64 {
	if v >= 0 {
		return int64(v + 0.5)
	}
	return int64(v - 0.5)
}
