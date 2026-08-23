package main

import (
	"github.com/ashwingopalsamy/agentic-go/internal/analysis/concurrency"
	"github.com/ashwingopalsamy/agentic-go/internal/analysis/errors"
	"golang.org/x/tools/go/analysis/multichecker"
)

func main() {
	multichecker.Main(concurrency.Analyzer, errors.Analyzer)
}
