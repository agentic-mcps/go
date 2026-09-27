// retrievalbench screens Agentic Go's declaration retrieval against a fixed
// ripgrep workflow and, optionally, the repository's pinned gopls provider.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agentic-mcps/go/validation/internal/retrievalstudy"
)

func main() {
	var options retrievalstudy.Options
	flag.StringVar(&options.Manifest, "manifest", "", "versioned JSON manifest with exact repository commit and reviewed gold spans")
	flag.StringVar(&options.RepositoryPath, "repo", "", "local Git clone whose origin and pinned commit are validated")
	flag.StringVar(&options.SourceRepositoryPath, "source-repo", ".", "checkout of the harness and retrieval implementation to fingerprint")
	flag.StringVar(&options.Output, "out", "", "result JSON output path")
	flag.StringVar(&options.GoplsBinary, "gopls", "", "optional path to the pinned gopls v0.21.0 binary")
	flag.BoolVar(&options.TextAblation, "text-candidate-ablation", false, "run a bounded private text-line candidate augmentation with the existing scorer")
	flag.IntVar(&options.Repetitions, "repetitions", 3, "cold and warm timing samples per question and cutoff (1-20)")
	flag.DurationVar(&options.Timeout, "timeout", 5*time.Minute, "per-command and per-search timeout")
	flag.Int64Var(&options.MaxSourceBytes, "max-source-bytes", 2<<30, "maximum extracted supported source bytes")
	flag.Int64Var(&options.MaxFileBytes, "max-file-bytes", 256<<20, "maximum extracted bytes for one supported source file")
	flag.Int64Var(&options.RGOutputBytes, "max-rg-output-bytes", 64<<20, "maximum captured ripgrep output bytes per query")
	flag.IntVar(&options.RGLineLimit, "max-rg-lines", 100000, "maximum matching lines captured per ripgrep query")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: retrievalbench --manifest study.json --repo /path/to/clone --out result.json [--gopls /path/to/gopls]")
		fmt.Fprintln(os.Stderr, "Exports only the manifest's exact Git commit with git archive; dirty checkout files are ignored.")
		fmt.Fprintln(os.Stderr, "Gold spans are one-based repository-relative line ranges. Metrics use declaration anchors for Agentic Go and matching lines for rg.")
		fmt.Fprintln(os.Stderr, "Supported paths: Go, Markdown, reStructuredText, text, YAML, JSON, TOML, proto, Go mod/sum, and common build metadata files.")
		fmt.Fprintln(os.Stderr, "The optional gopls baseline requires v0.21.0; it runs workspace/symbol and reports initialization separately.")
		fmt.Fprintln(os.Stderr, "A timed go list -e -json ./... package inventory runs on the archive; it has no gold relevance score.")
		fmt.Fprintln(os.Stderr, "Archive omissions and transformed blobs are detected; incomplete source coverage makes retrieval scores partial.")
		fmt.Fprintln(os.Stderr, "The report includes the source HEAD, source dirty-diff SHA-256, manifest SHA-256, and retrieval source SHA-256.")
		fmt.Fprintln(os.Stderr, "This screens the candidate-retrieval kernel; it does not claim complete go_context coverage or semantic resolution.")
		fmt.Fprintln(os.Stderr, "--text-candidate-ablation is evaluation-only; it does not change the public MCP contract or live retrieval path.")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "retrievalbench accepts no positional arguments")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := retrievalstudy.Execute(ctx, options); err != nil {
		fmt.Fprintln(os.Stderr, "retrievalbench:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "retrieval report written")
}
