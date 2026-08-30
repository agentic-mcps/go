package main

import (
	"github.com/agentic-mcps/go/internal/analysis/concurrency"
	"github.com/agentic-mcps/go/internal/analysis/errors"
	"golang.org/x/tools/go/analysis/multichecker"
)

func main() {
	multichecker.Main(concurrency.Analyzer, errors.Analyzer)
}
