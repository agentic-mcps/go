// Command gateeval runs the pre-registered gate evaluation in
// validation/gate/README.md: select commits, generate variants, run the arms,
// and summarize the runs.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agentic-mcps/go/validation/internal/gateeval"
)

const usageText = `usage: gateeval <command> [flags]

commands:
  select     mine commits from the corpus history
  generate   build the variants of the selected commits
  run        run the arms against the variants
  summarize  score the runs

Run "gateeval <command> -h" for the flags of a command.
`

// usageError marks a mistake in how the command was invoked.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run executes one command and returns the process exit code: 0 on success,
// 1 on failure, 2 on a usage error.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	err := dispatch(ctx, args, stdout, stderr)
	var usage usageError
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.As(err, &usage):
		_, _ = fmt.Fprintf(stderr, "gateeval: %s\n", usage.msg)
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "gateeval: %v\n", err)
		return 1
	}
}

func dispatch(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		_, _ = io.WriteString(stderr, usageText)
		return usageError{"a command is required"}
	}
	switch args[0] {
	case "select":
		return runSelect(ctx, args[1:], stderr)
	case "generate":
		return runGenerate(ctx, args[1:], stderr)
	case "run":
		return runArms(ctx, args[1:], stderr)
	case "summarize":
		return runSummarize(args[1:], stdout, stderr)
	case "-h", "-help", "--help", "help":
		_, _ = io.WriteString(stdout, usageText)
		return nil
	}
	_, _ = io.WriteString(stderr, usageText)
	return usageError{fmt.Sprintf("unknown command %q", args[0])}
}

// parse parses a command's flags; any parse failure is a usage error.
func parse(fs *flag.FlagSet, args []string, stderr io.Writer, required map[string]*string) error {
	names := make([]string, 0, len(required))
	for name := range required {
		names = append(names, name)
	}
	sort.Strings(names)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError{err.Error()}
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Sprintf("unexpected argument %q", fs.Arg(0))}
	}
	for _, name := range names {
		if *required[name] == "" {
			return usageError{"--" + name + " is required"}
		}
	}
	return nil
}

func runSelect(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("select", flag.ContinueOnError)
	var options gateeval.SelectOptions
	fs.StringVar(&options.CorpusCSV, "corpus", "validation/gate/corpus.csv", "corpus CSV (project,url,pinned)")
	fs.StringVar(&options.WorkDir, "work", "", "work directory (clones/ and work/ are created under it)")
	fs.StringVar(&options.Out, "out", "", "selections JSONL to write")
	fs.StringVar(&options.ExclusionsOut, "exclusions", "", "exclusions JSONL to write")
	fs.IntVar(&options.PerRepoTrue, "per-repo-true", 30, "true patches per repository")
	fs.IntVar(&options.PerRepoFlaw, "per-repo-flaw", 10, "flaw seeds per repository")
	fs.IntVar(&options.PerRepoDestructive, "per-repo-destructive", 20, "destructive true patches per repository")
	fs.IntVar(&options.MaxScan, "max-scan", 3000, "commits scanned per repository")
	fs.DurationVar(&options.TestTimeout, "test-timeout", 120*time.Second, "timeout per build or test command")
	required := map[string]*string{"work": &options.WorkDir, "out": &options.Out, "exclusions": &options.ExclusionsOut}
	if err := parse(fs, args, stderr, required); err != nil {
		return err
	}
	return gateeval.Select(ctx, options)
}

func runGenerate(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	var options gateeval.GenerateOptions
	fs.StringVar(&options.WorkDir, "work", "", "work directory holding the clones")
	fs.StringVar(&options.Selected, "selected", "", "selections JSONL from select")
	fs.StringVar(&options.Out, "out", "", "variants JSONL to append to (resumable)")
	fs.StringVar(&options.ExclusionsOut, "exclusions", "", "exclusions JSONL to append to")
	fs.IntVar(&options.MaxMutantAttempts, "max-mutant-attempts", 20, "mutant attempts per commit")
	fs.DurationVar(&options.TestTimeout, "test-timeout", 120*time.Second, "timeout per test command")
	required := map[string]*string{"work": &options.WorkDir, "selected": &options.Selected, "out": &options.Out, "exclusions": &options.ExclusionsOut}
	if err := parse(fs, args, stderr, required); err != nil {
		return err
	}
	return gateeval.Generate(ctx, options)
}

func runArms(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var options gateeval.RunOptions
	var variantsPath, arms string
	fs.StringVar(&options.WorkDir, "work", "", "work directory holding the clones")
	fs.StringVar(&variantsPath, "variants", "", "variants JSONL from generate")
	fs.StringVar(&options.Out, "out", "", "runs JSONL to append to (resumable)")
	fs.StringVar(&arms, "arms", "B0,B1,B2,B2s,B3,Gci,Ghook", "comma-separated arms")
	fs.StringVar(&options.GateBin, "gate-bin", "", "agentic-go binary (needed by Gci and Ghook)")
	fs.StringVar(&options.B3Script, "b3", "validation/gate/b3-grep.sh", "B3 grep script")
	fs.IntVar(&options.Workers, "workers", 2, "parallel workers")
	fs.DurationVar(&options.CommandTimeout, "command-timeout", 10*time.Minute, "timeout per arm command")
	fs.BoolVar(&options.Timing, "timing", false, "run every arm twice on true patches")
	required := map[string]*string{"work": &options.WorkDir, "variants": &variantsPath, "out": &options.Out}
	if err := parse(fs, args, stderr, required); err != nil {
		return err
	}
	parsed, err := parseArms(arms)
	if err != nil {
		return usageError{err.Error()}
	}
	options.Arms = parsed
	if options.B3Script, err = filepath.Abs(options.B3Script); err != nil {
		return fmt.Errorf("resolving --b3: %w", err)
	}
	if options.Variants, err = gateeval.ReadJSONL[gateeval.Variant](variantsPath); err != nil {
		return err
	}
	return gateeval.RunArms(ctx, options)
}

func runSummarize(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("summarize", flag.ContinueOnError)
	var variantsPath, runsPath, exclusionsPaths, outDir string
	fs.StringVar(&variantsPath, "variants", "", "variants JSONL")
	fs.StringVar(&runsPath, "runs", "", "runs JSONL")
	fs.StringVar(&exclusionsPaths, "exclusions", "", "exclusions JSONL (comma-separate several files)")
	fs.StringVar(&outDir, "out", "", "directory for summary.json and summary.md")
	required := map[string]*string{"variants": &variantsPath, "runs": &runsPath, "exclusions": &exclusionsPaths, "out": &outDir}
	if err := parse(fs, args, stderr, required); err != nil {
		return err
	}
	variants, err := gateeval.ReadJSONL[gateeval.Variant](variantsPath)
	if err != nil {
		return err
	}
	runs, err := gateeval.ReadJSONL[gateeval.Run](runsPath)
	if err != nil {
		return err
	}
	exclusions := []gateeval.Exclusion{}
	for _, name := range strings.Split(exclusionsPaths, ",") {
		more, err := gateeval.ReadJSONL[gateeval.Exclusion](strings.TrimSpace(name))
		if err != nil {
			return err
		}
		exclusions = append(exclusions, more...)
	}
	summary := gateeval.Summarize(variants, runs, exclusions)
	if err := gateeval.WriteSummary(outDir, summary); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "wrote %s\n", filepath.Join(outDir, "summary.md"))
	return nil
}

// parseArms turns a comma-separated list into known arms.
func parseArms(list string) ([]gateeval.Arm, error) {
	known := map[gateeval.Arm]bool{
		gateeval.ArmB0: true, gateeval.ArmB1: true, gateeval.ArmB2: true, gateeval.ArmB2Star: true,
		gateeval.ArmB3: true, gateeval.ArmGCI: true, gateeval.ArmGHook: true,
	}
	var arms []gateeval.Arm
	for _, name := range strings.Split(list, ",") {
		arm := gateeval.Arm(strings.TrimSpace(name))
		if !known[arm] {
			return nil, fmt.Errorf("unknown arm %q", name)
		}
		arms = append(arms, arm)
	}
	return arms, nil
}
