# ecos-golang

Go bindings for the [ECOS](https://github.com/embotech/ecos) conic solver, plus a client library for solving optimization problems via a matrix-server.

## Packages

- **`ecosgo`** — low-level cgo bindings to ECOS and ECOS_BB (branch-and-bound for mixed-integer problems). ECOS C sources are embedded and compiled by cgo automatically.
- **`client`** — high-level client that posts DSL or JSON problem definitions to a [parsecos](https://github.com/Rubicon-Development/parsecos) server, then solves locally with ECOS.

## Install

```
go get github.com/Rubicon-Development/ecos-golang
```

Requires a C compiler (cgo compiles the vendored ECOS sources on first build).

## Quick start

### One-shot solve

Post a problem and get a solution in one call:

```go
package main

import (
    "fmt"
    "log"

    "github.com/Rubicon-Development/ecos-golang/client"
)

const dsl = `
params:
  demand = [10, 20]

vars:
  continuous x[2]

minimize:
  x[0] + x[1]

subject to:
  x[0] >= demand[0]
  x[1] >= demand[1]
`

func main() {
    result, err := client.SolveDSL("http://127.0.0.1:8080", []byte(dsl))
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Status: %s\n", result.Status)
    fmt.Printf("Objective: %.2f\n", result.Objective)
    for _, name := range result.ColumnNames {
        fmt.Printf("  %s = %v\n", name, result.Variables[name])
    }
}
```

JSON input works the same way:

```go
result, err := client.SolveJSON("http://127.0.0.1:8080", jsonBytes)
```

### Templates (parameterized problems)

When you need to solve the same problem structure many times with different
data, fetch a template once and solve locally — no network round-trip per
solve:

```go
// Define the model with symbolic params (no values).
const model = `
params:
  load[24]
  pv_cap[24]

vars:
  continuous grid[24]
  continuous pv[24]
  boolean gen_on[24]

minimize:
  for t in 0..23:
    grid[t] + 2 * gen_on[t]
  end

subject to:
  for t in 0..23:
    grid[t] + pv[t] = load[t]
    pv[t] <= pv_cap[t]
    grid[t] >= 0
    pv[t] >= 0
  end
`

// Fetch template once (one HTTP call).
tmpl, err := client.FetchTemplateDSL("http://127.0.0.1:8080", []byte(model))
if err != nil {
    log.Fatal(err)
}

// Inspect required parameters.
for _, p := range tmpl.ParamDefs {
    if len(p.Dimensions) == 0 {
        fmt.Printf("  %s (scalar)\n", p.Name)
    } else {
        fmt.Printf("  %s [%d]\n", p.Name, p.Dimensions[0])
    }
}

// Solve many times with different data — no network, just local ECOS.
for _, scenario := range scenarios {
    result, err := tmpl.Solve(client.Params{
        "load":   scenario.Load,   // []float64 of length 24
        "pv_cap": scenario.PVCap,  // []float64 of length 24
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Scenario objective: %.2f\n", result.Objective)
}
```

### Low-level ECOS bindings

Use `ecosgo` directly if you already have matrices:

```go
import "github.com/Rubicon-Development/ecos-golang/ecosgo"

prob := &ecosgo.Problem{
    N: 1, M: 2, P: 0, L: 2,
    G: &ecosgo.SparseMatrix{
        M: 2, N: 1, Nnz: 2,
        Data: []float64{1, -1}, RowI: []int{0, 1}, ColP: []int{0, 2},
    },
    C: []float64{-1},
    H: []float64{1, 0},
}

ws, err := ecosgo.Setup(prob, nil)
if err != nil {
    log.Fatal(err)
}
defer ws.Cleanup()

sol, err := ws.Solve()
if err != nil {
    log.Fatal(err)
}
fmt.Printf("x = %.4f, objective = %.4f\n", sol.X[0], sol.Stats.PCost)
```

For mixed-integer problems, use `ecosgo.BBSetup` with `ecosgo.BBProblem`:

```go
ws, err := ecosgo.BBSetup(&ecosgo.BBProblem{
    Problem:     prob,
    BoolVarsIdx: []int{0},    // variables constrained to {0, 1}
    IntVarsIdx:  []int{1},    // variables constrained to integers
})
```

## CLI example

A small example CLI lives in `cmd/dsl/`:

```
# DSL input
cat problem.dsl | go run ./cmd/dsl/ -endpoint http://127.0.0.1:8080

# JSON input
cat problem.json | go run ./cmd/dsl/ -endpoint http://127.0.0.1:8080 -format json

# JSON output
cat problem.dsl | go run ./cmd/dsl/ -json
```

## Development

With nix:

```
nix develop
go test ./...
```

Without nix:

```
git clone --recurse-submodules https://github.com/Rubicon-Development/ecos-golang
cd ecos-golang
go test ./...
```

The ECOS C sources are vendored as a git submodule at `ecos/` and compiled
automatically by cgo via `ecosgo/cgo_sources.c` — no `make` step required.
