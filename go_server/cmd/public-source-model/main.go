// An offline stdin/stdout bridge. Never initializes a database or listener.
package main

import (
	"encoding/json"
	"fmt"
	"go_server/handlers"
	"io"
	"os"
)

func main() {
	var input struct {
		Matches []handlers.PublicSourceModelInput `json:"matches"`
	}
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 64<<20))
	if err := decoder.Decode(&input); err != nil {
		fmt.Fprintln(os.Stderr, "invalid model input")
		os.Exit(1)
	}
	if len(input.Matches) > 500 {
		fmt.Fprintln(os.Stderr, "too many model inputs")
		os.Exit(1)
	}
	results := make([]handlers.PublicSourceModelResult, 0, len(input.Matches))
	for _, match := range input.Matches {
		results = append(results, handlers.AnalyzePublicSourceModel(match))
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		os.Exit(1)
	}
}
