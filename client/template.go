package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	ecos "github.com/Rubicon-Development/ecos-golang/ecosgo"
)

// Params supplies concrete values for template parameters.
// Keys are parameter names. Values are float64 (scalar) or []float64 (array).
type Params map[string]any

// Template is a reusable, parameterized optimization problem. Fetch it once
// from the server, then call Solve repeatedly with different Params — no
// network round-trip or re-parsing needed after the initial fetch.
type Template struct {
	ParamDefs       []ParamDef       `json:"params"`
	N               int              `json:"n"`
	M               int              `json:"m"`
	P               int              `json:"p"`
	L               int              `json:"l"`
	Q               []int            `json:"q"`
	E               int              `json:"e"`
	G               *sparseFormulaJS `json:"g"`
	A               *sparseFormulaJS `json:"a"`
	C               []Formula        `json:"c"`
	H               []Formula        `json:"h"`
	B               []Formula        `json:"b"`
	BoolVarsIdx     []int            `json:"bool_vars_idx"`
	IntVarsIdx      []int            `json:"int_vars_idx"`
	ColumnNames     []string         `json:"column_names"`
	ObjectiveOffset Formula          `json:"objective_offset"`
}

// ParamDef describes one declared parameter and its shape.
type ParamDef struct {
	Name       string `json:"name"`
	Dimensions []int  `json:"dimensions"`
}

// Formula is a linear combination of parameter references plus a constant:
//
//	value = Constant + Σ (term.Coefficient × param[term.Indices...])
type Formula struct {
	Constant float64 `json:"constant"`
	Terms    []Term  `json:"terms"`
}

// Term is one addend in a Formula.
type Term struct {
	Param       string  `json:"param"`
	Indices     []int   `json:"indices"`
	Coefficient float64 `json:"coefficient"`
}

type sparseFormulaJS struct {
	Pr []Formula `json:"pr"`
	Jc []int     `json:"jc"`
	Ir []int     `json:"ir"`
}

// FetchTemplateDSL posts DSL text to baseURL/v1/template/dsl and returns
// a reusable Template.
func FetchTemplateDSL(baseURL string, dsl []byte) (*Template, error) {
	return fetchTemplate(baseURL+"/v1/template/dsl", "text/plain", dsl)
}

// FetchTemplateJSON posts a JSON problem definition to baseURL/v1/template/json
// and returns a reusable Template.
func FetchTemplateJSON(baseURL string, jsonInput []byte) (*Template, error) {
	return fetchTemplate(baseURL+"/v1/template/json", "application/json", jsonInput)
}

func fetchTemplate(endpoint, contentType string, payload []byte) (*Template, error) {
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

	var t Template
	if err := json.Unmarshal(body, &t); err != nil {
		return nil, fmt.Errorf("decode template: %w", err)
	}
	return &t, nil
}

// Solve evaluates every formula in the template with the given parameter
// values, builds the ECOS problem, solves it, and returns the result.
// This is the hot path — no network I/O, just arithmetic and the solver.
func (t *Template) Solve(params Params) (*Result, error) {
	if err := t.validateParams(params); err != nil {
		return nil, err
	}

	c, err := evalSlice(t.C, params)
	if err != nil {
		return nil, fmt.Errorf("evaluate c: %w", err)
	}
	h, err := evalSlice(t.H, params)
	if err != nil {
		return nil, fmt.Errorf("evaluate h: %w", err)
	}

	var b []float64
	if len(t.B) > 0 {
		b, err = evalSlice(t.B, params)
		if err != nil {
			return nil, fmt.Errorf("evaluate b: %w", err)
		}
	}

	var gMat *ecos.SparseMatrix
	if t.G != nil {
		pr, err := evalSlice(t.G.Pr, params)
		if err != nil {
			return nil, fmt.Errorf("evaluate G.pr: %w", err)
		}
		gMat = &ecos.SparseMatrix{
			M: t.M, N: t.N, Nnz: len(pr),
			Data: pr, RowI: t.G.Ir, ColP: t.G.Jc,
		}
	}

	var aMat *ecos.SparseMatrix
	if t.A != nil {
		pr, err := evalSlice(t.A.Pr, params)
		if err != nil {
			return nil, fmt.Errorf("evaluate A.pr: %w", err)
		}
		aMat = &ecos.SparseMatrix{
			M: t.P, N: t.N, Nnz: len(pr),
			Data: pr, RowI: t.A.Ir, ColP: t.A.Jc,
		}
	}

	objOffset, err := t.ObjectiveOffset.Eval(params)
	if err != nil {
		return nil, fmt.Errorf("evaluate objective_offset: %w", err)
	}

	prob := &ecos.Problem{
		N: t.N, M: t.M, P: t.P, L: t.L,
		NCones: len(t.Q), Q: t.Q, NEX: t.E,
		G: gMat, A: aMat, C: c, H: h, B: b,
	}

	hasIntegrality := len(t.BoolVarsIdx) > 0 || len(t.IntVarsIdx) > 0

	var sol *ecos.Solution
	if hasIntegrality {
		ws, err := ecos.BBSetup(&ecos.BBProblem{
			Problem:     prob,
			BoolVarsIdx: t.BoolVarsIdx,
			IntVarsIdx:  t.IntVarsIdx,
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

	objective := objOffset
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
		Variables:   variableMap(t.ColumnNames, sol.X, t.BoolVarsIdx, t.IntVarsIdx),
		X:           sol.X,
		ColumnNames: t.ColumnNames,
	}, nil
}

// Eval computes the concrete float64 value of a formula given param values.
func (f *Formula) Eval(params Params) (float64, error) {
	v := f.Constant
	for _, term := range f.Terms {
		pv, err := lookupParam(params, term.Param, term.Indices)
		if err != nil {
			return 0, err
		}
		v += term.Coefficient * pv
	}
	return v, nil
}

func evalSlice(formulas []Formula, params Params) ([]float64, error) {
	out := make([]float64, len(formulas))
	for i := range formulas {
		v, err := formulas[i].Eval(params)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", i, err)
		}
		out[i] = v
	}
	return out, nil
}

func lookupParam(params Params, name string, indices []int) (float64, error) {
	raw, ok := params[name]
	if !ok {
		return 0, fmt.Errorf("missing param %q", name)
	}

	if len(indices) == 0 {
		// Scalar parameter.
		switch v := raw.(type) {
		case float64:
			return v, nil
		case int:
			return float64(v), nil
		default:
			return 0, fmt.Errorf("param %q: expected scalar, got %T", name, raw)
		}
	}

	// Array parameter — only 1-D supported by the DSL.
	idx := indices[0]
	switch arr := raw.(type) {
	case []float64:
		if idx < 0 || idx >= len(arr) {
			return 0, fmt.Errorf("param %q index %d out of range (len %d)", name, idx, len(arr))
		}
		return arr[idx], nil
	case []int:
		if idx < 0 || idx >= len(arr) {
			return 0, fmt.Errorf("param %q index %d out of range (len %d)", name, idx, len(arr))
		}
		return float64(arr[idx]), nil
	case []any:
		if idx < 0 || idx >= len(arr) {
			return 0, fmt.Errorf("param %q index %d out of range (len %d)", name, idx, len(arr))
		}
		switch v := arr[idx].(type) {
		case float64:
			return v, nil
		case int:
			return float64(v), nil
		default:
			return 0, fmt.Errorf("param %q[%d]: expected number, got %T", name, idx, arr[idx])
		}
	default:
		return 0, fmt.Errorf("param %q: expected array, got %T", name, raw)
	}
}

func (t *Template) validateParams(params Params) error {
	for _, pd := range t.ParamDefs {
		raw, ok := params[pd.Name]
		if !ok {
			return fmt.Errorf("missing param %q", pd.Name)
		}
		if len(pd.Dimensions) == 0 {
			// Scalar — accept any numeric.
			switch raw.(type) {
			case float64, int:
			default:
				return fmt.Errorf("param %q: expected scalar, got %T", pd.Name, raw)
			}
			continue
		}
		// Array — check length.
		want := pd.Dimensions[0]
		switch arr := raw.(type) {
		case []float64:
			if len(arr) != want {
				return fmt.Errorf("param %q: expected length %d, got %d", pd.Name, want, len(arr))
			}
		case []int:
			if len(arr) != want {
				return fmt.Errorf("param %q: expected length %d, got %d", pd.Name, want, len(arr))
			}
		case []any:
			if len(arr) != want {
				return fmt.Errorf("param %q: expected length %d, got %d", pd.Name, want, len(arr))
			}
		default:
			return fmt.Errorf("param %q: expected array of length %d, got %T", pd.Name, want, raw)
		}
	}
	return nil
}
