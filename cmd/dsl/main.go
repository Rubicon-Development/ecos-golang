// dsl is a small CLI that sends a DSL program to the /v1/dsl matrix
// endpoint, feeds the returned LP/MIP into ECOS (or ECOS_BB when
// integer/boolean variables are present), and prints the solution.
//
// Usage:
//
//	cat program.dsl | dsl
//	dsl -endpoint http://host:8080/v1/dsl -file program.dsl
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	ecos "github.com/ecos-golang/ecosgo"
)

type sparseJSON struct {
	Pr []float64 `json:"pr"`
	Jc []int     `json:"jc"`
	Ir []int     `json:"ir"`
}

type dslResponse struct {
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

func fetchMatrices(endpoint string, dsl []byte) (*dslResponse, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(dsl))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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

	var out dslResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode JSON: %w (body: %s)", err, body)
	}
	return &out, nil
}

type solveResult struct {
	X        []float64
	ExitFlag int
	PCost    float64
	Iter     int
}

func solve(r *dslResponse) (*solveResult, error) {
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
	result := &solveResult{}

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
		sol, err := ws.Solve()
		if err != nil {
			return nil, err
		}
		result.X = sol.X
		result.ExitFlag = sol.ExitFlag
		if sol.Stats != nil {
			result.PCost = sol.Stats.PCost
			result.Iter = sol.Stats.Iter
		}
	} else {
		ws, err := ecos.Setup(prob, nil)
		if err != nil {
			return nil, fmt.Errorf("ECOS setup: %w", err)
		}
		defer ws.Cleanup()
		sol, err := ws.Solve()
		if err != nil {
			return nil, err
		}
		result.X = sol.X
		result.ExitFlag = sol.ExitFlag
		if sol.Stats != nil {
			result.PCost = sol.Stats.PCost
			result.Iter = sol.Stats.Iter
		}
	}

	return result, nil
}

func main() {
	endpoint := flag.String("endpoint", "http://127.0.0.1:8080/v1/dsl", "DSL matrix endpoint URL")
	filePath := flag.String("file", "", "DSL file to send (default: stdin)")
	jsonOut := flag.Bool("json", false, "emit solution as JSON instead of a formatted table")
	flag.Parse()

	var dsl []byte
	var err error
	if *filePath != "" {
		dsl, err = os.ReadFile(*filePath)
	} else {
		dsl, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "read DSL: %v\n", err)
		os.Exit(1)
	}
	if len(bytes.TrimSpace(dsl)) == 0 {
		fmt.Fprintln(os.Stderr, "no DSL input provided (stdin was empty; pass -file or pipe input)")
		os.Exit(2)
	}

	resp, err := fetchMatrices(*endpoint, dsl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch matrices: %v\n", err)
		os.Exit(1)
	}

	res, err := solve(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "solve: %v\n", err)
		os.Exit(1)
	}

	objective := res.PCost + resp.ObjectiveOffset

	if *jsonOut {
		out := map[string]any{
			"status":    ecos.ExitCodeString(res.ExitFlag),
			"exit_flag": res.ExitFlag,
			"objective": objective,
			"iter":      res.Iter,
			"variables": variableMap(resp.ColumnNames, res.X, resp.BoolVarsIdx, resp.IntVarsIdx),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}

	fmt.Printf("ECOS %s\n", ecos.Version())
	fmt.Printf("Problem: n=%d m=%d p=%d l=%d ncones=%d nex=%d\n",
		resp.N, resp.M, resp.P, resp.L, len(resp.Q), resp.E)
	fmt.Printf("Integrality: %d boolean, %d integer variables\n",
		len(resp.BoolVarsIdx), len(resp.IntVarsIdx))
	fmt.Printf("\nStatus: %s\n", ecos.ExitCodeString(res.ExitFlag))
	fmt.Printf("Objective: %.6f (offset %.6f)\n", objective, resp.ObjectiveOffset)
	fmt.Printf("Iterations: %d\n\n", res.Iter)

	boolSet := indexSet(resp.BoolVarsIdx)
	intSet := indexSet(resp.IntVarsIdx)

	fmt.Println("Solution:")
	for i, v := range res.X {
		name := fmt.Sprintf("x[%d]", i)
		if i < len(resp.ColumnNames) {
			name = resp.ColumnNames[i]
		}
		switch {
		case boolSet[i]:
			fmt.Printf("  %-16s = %d  (bool)\n", name, roundInt(v))
		case intSet[i]:
			fmt.Printf("  %-16s = %d  (int)\n", name, roundInt(v))
		default:
			fmt.Printf("  %-16s = % .6f\n", name, v)
		}
	}
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
