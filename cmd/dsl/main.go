// Command ecos-solve is a small CLI that demonstrates the client library.
// It reads a problem (DSL text or JSON), posts it to a matrix-server
// endpoint, and prints the ECOS solution.
//
// Usage:
//
//	cat problem.dsl  | ecos-solve
//	cat problem.json | ecos-solve -format json
//	ecos-solve -file problem.dsl
//	ecos-solve -file problem.json -format json -json
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Rubicon-Development/ecos-golang/client"
)

func main() {
	baseURL := flag.String("endpoint", "http://127.0.0.1:8080", "base URL of the matrix server")
	filePath := flag.String("file", "", "input file (default: stdin)")
	format := flag.String("format", "dsl", "input format: dsl or json")
	jsonOut := flag.Bool("json", false, "emit solution as JSON")
	flag.Parse()

	input, err := readInput(*filePath)
	if err != nil {
		fatal(err)
	}
	if len(bytes.TrimSpace(input)) == 0 {
		fmt.Fprintln(os.Stderr, "no input provided (stdin was empty; pass -file or pipe input)")
		os.Exit(2)
	}

	var result *client.Result
	switch *format {
	case "dsl":
		result, err = client.SolveDSL(*baseURL, input)
	case "json":
		result, err = client.SolveJSON(*baseURL, input)
	default:
		fmt.Fprintf(os.Stderr, "unknown format %q (use dsl or json)\n", *format)
		os.Exit(2)
	}
	if err != nil {
		fatal(err)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
		return
	}

	printResult(result)
}

func readInput(path string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path)
	}
	return io.ReadAll(os.Stdin)
}

func printResult(r *client.Result) {
	fmt.Printf("Status: %s (exit %d)\n", r.Status, r.ExitFlag)
	fmt.Printf("Objective: %.6f\n", r.Objective)
	fmt.Printf("Iterations: %d\n\n", r.Iterations)
	fmt.Println("Variables:")
	for _, name := range r.ColumnNames {
		v := r.Variables[name]
		switch val := v.(type) {
		case bool:
			fmt.Printf("  %-20s = %v  (bool)\n", name, val)
		case int64:
			fmt.Printf("  %-20s = %d  (int)\n", name, val)
		default:
			fmt.Printf("  %-20s = % .6f\n", name, v)
		}
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
