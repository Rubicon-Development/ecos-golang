package client

import (
	"math"
	"net/http"
	"testing"
)

const testBase = "http://127.0.0.1:8080"

func serverAvailable() bool {
	resp, err := http.Get(testBase)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

const energyDSLTemplate = `params:
  load[24]
  pv_cap[24]

vars:
  continuous grid[24]
  continuous pv[24]
  continuous charge[24]
  continuous discharge[24]
  continuous soc[25]
  continuous gen[24]
  boolean gen_on[24]

minimize:
  for t in 0..23:
    grid[t] + 5 * gen[t] + 2 * gen_on[t]
  end

subject to:
  for t in 0..23:
    grid[t] + pv[t] + discharge[t] + gen[t] - charge[t] = load[t]
  end
  for t in 0..23:
    soc[t+1] - soc[t] - charge[t] + discharge[t] = 0
  end
  soc[0] = 40
  soc[24] = 40
  for t in 0..23:
    grid[t] >= 0
    grid[t] <= 25
    pv[t] >= 0
    pv[t] <= pv_cap[t]
    charge[t] >= 0
    charge[t] <= 20
    discharge[t] >= 0
    discharge[t] <= 20
    gen[t] >= 0
    gen[t] <= 40 * gen_on[t]
  end
  for t in 0..24:
    soc[t] >= 0
    soc[t] <= 80
  end
`

// energyDSLConcrete is the same model but with values inlined (no template).
const energyDSLConcrete = `params:
  load = [28, 26, 24, 23, 22, 24, 30, 38, 44, 48, 52, 55, 57, 56, 54, 50, 46, 44, 42, 40, 38, 36, 34, 31]
  pv_cap = [0, 0, 0, 0, 0, 2, 8, 16, 24, 32, 36, 38, 40, 36, 30, 22, 12, 4, 0, 0, 0, 0, 0, 0]

vars:
  continuous grid[24]
  continuous pv[24]
  continuous charge[24]
  continuous discharge[24]
  continuous soc[25]
  continuous gen[24]
  boolean gen_on[24]

minimize:
  for t in 0..23:
    grid[t] + 5 * gen[t] + 2 * gen_on[t]
  end

subject to:
  for t in 0..23:
    grid[t] + pv[t] + discharge[t] + gen[t] - charge[t] = load[t]
  end
  for t in 0..23:
    soc[t+1] - soc[t] - charge[t] + discharge[t] = 0
  end
  soc[0] = 40
  soc[24] = 40
  for t in 0..23:
    grid[t] >= 0
    grid[t] <= 25
    pv[t] >= 0
    pv[t] <= pv_cap[t]
    charge[t] >= 0
    charge[t] <= 20
    discharge[t] >= 0
    discharge[t] <= 20
    gen[t] >= 0
    gen[t] <= 40 * gen_on[t]
  end
  for t in 0..24:
    soc[t] >= 0
    soc[t] <= 80
  end
`

var energyParams = Params{
	"load":   []float64{28, 26, 24, 23, 22, 24, 30, 38, 44, 48, 52, 55, 57, 56, 54, 50, 46, 44, 42, 40, 38, 36, 34, 31},
	"pv_cap": []float64{0, 0, 0, 0, 0, 2, 8, 16, 24, 32, 36, 38, 40, 36, 30, 22, 12, 4, 0, 0, 0, 0, 0, 0},
}

func TestTemplateMatchesDirect(t *testing.T) {
	if !serverAvailable() {
		t.Skip("matrix server not running at " + testBase)
	}

	// Solve via direct DSL (the baseline).
	direct, err := SolveDSL(testBase, []byte(energyDSLConcrete))
	if err != nil {
		t.Fatalf("direct solve: %v", err)
	}
	if direct.ExitFlag != 0 {
		t.Fatalf("direct solve non-optimal: %s", direct.Status)
	}

	// Fetch template, then solve with same param values.
	tmpl, err := FetchTemplateDSL(testBase, []byte(energyDSLTemplate))
	if err != nil {
		t.Fatalf("fetch template: %v", err)
	}
	templated, err := tmpl.Solve(energyParams)
	if err != nil {
		t.Fatalf("template solve: %v", err)
	}
	if templated.ExitFlag != 0 {
		t.Fatalf("template solve non-optimal: %s", templated.Status)
	}

	// Objectives should match closely.
	if math.Abs(direct.Objective-templated.Objective) > 1e-3 {
		t.Errorf("objective mismatch: direct=%.6f template=%.6f",
			direct.Objective, templated.Objective)
	}

	// All variables should match.
	for _, name := range direct.ColumnNames {
		dv := toFloat(direct.Variables[name])
		tv := toFloat(templated.Variables[name])
		if math.Abs(dv-tv) > 1e-3 {
			t.Errorf("var %s mismatch: direct=%v template=%v", name, dv, tv)
		}
	}
}

func TestTemplateResolveRepeatedly(t *testing.T) {
	if !serverAvailable() {
		t.Skip("matrix server not running at " + testBase)
	}

	tmpl, err := FetchTemplateDSL(testBase, []byte(energyDSLTemplate))
	if err != nil {
		t.Fatalf("fetch template: %v", err)
	}

	// Solve the same template 5 times with the same params — should be
	// deterministic and not leak/crash.
	var firstObj float64
	for i := 0; i < 5; i++ {
		res, err := tmpl.Solve(energyParams)
		if err != nil {
			t.Fatalf("solve %d: %v", i, err)
		}
		if res.ExitFlag != 0 {
			t.Fatalf("solve %d non-optimal: %s", i, res.Status)
		}
		if i == 0 {
			firstObj = res.Objective
		} else if math.Abs(res.Objective-firstObj) > 1e-6 {
			t.Errorf("solve %d objective %.6f != first %.6f", i, res.Objective, firstObj)
		}
	}
}

func TestTemplateMissingParam(t *testing.T) {
	if !serverAvailable() {
		t.Skip("matrix server not running at " + testBase)
	}

	tmpl, err := FetchTemplateDSL(testBase, []byte(energyDSLTemplate))
	if err != nil {
		t.Fatalf("fetch template: %v", err)
	}

	// Only supply one param — should fail validation.
	_, err = tmpl.Solve(Params{"load": []float64{1, 2, 3}})
	if err == nil {
		t.Fatal("expected error for missing param, got nil")
	}
}

func TestTemplateWrongLength(t *testing.T) {
	if !serverAvailable() {
		t.Skip("matrix server not running at " + testBase)
	}

	tmpl, err := FetchTemplateDSL(testBase, []byte(energyDSLTemplate))
	if err != nil {
		t.Fatalf("fetch template: %v", err)
	}

	_, err = tmpl.Solve(Params{
		"load":   []float64{1, 2, 3}, // wrong length
		"pv_cap": []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	})
	if err == nil {
		t.Fatal("expected error for wrong param length, got nil")
	}
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case bool:
		if x {
			return 1
		}
		return 0
	default:
		return 0
	}
}
