package retrievalstudy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const goListOutputLimit = 16 << 20

func inventoryGoPackages(parent context.Context, workspace string, timeout time.Duration) (GoPackageInventory, error) {
	result := GoPackageInventory{
		Status: "unavailable", Command: "go list -e -json ./...", TimeoutMS: timeout.Milliseconds(),
		OutputLimitBytes: goListOutputLimit, Coverage: "go_list_pattern_only",
	}
	if err := parent.Err(); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "list", "-e", "-json", "./...")
	command.Dir = workspace
	command.Env = setEnv(os.Environ(), "GOWORK", "off", "GOTOOLCHAIN", "local")
	var stdout, stderr limitedBuffer
	stdout.limit = goListOutputLimit
	stderr.limit = 64 << 10
	command.Stdout = &stdout
	command.Stderr = &stderr
	started := time.Now()
	err := command.Run()
	result.LatencyMS = float64(time.Since(started)) / float64(time.Millisecond)
	result.OutputBytes = stdout.Len()
	result.OutputTruncated = stdout.truncated
	var parseErr error
	result.PackageCount, result.PackagesWithErrors, parseErr = parseGoPackageInventory(stdout.Bytes())
	if parent.Err() != nil {
		return result, parent.Err()
	}
	if ctx.Err() != nil && ctx.Err() == context.DeadlineExceeded {
		result.Status = "timeout"
		result.Coverage = "partial_timeout"
		result.Error = "go list package inventory exceeded the configured timeout"
		return result, nil
	}
	if err != nil {
		result.Status = "partial"
		result.Coverage = "partial_command_failure"
		result.Error = "go list package inventory exited with an error"
		return result, nil
	}
	if result.OutputTruncated {
		result.Status = "partial"
		result.Coverage = "partial_output_limit"
		result.Error = "go list package inventory output exceeded the capture limit"
		return result, nil
	}
	if parseErr != nil {
		result.Status = "partial"
		result.Coverage = "partial_invalid_json_output"
		result.Error = "go list package inventory emitted incomplete JSON"
		return result, nil
	}
	if result.PackagesWithErrors > 0 {
		result.Status = "partial"
		result.Coverage = "partial_package_load_errors"
		result.Error = "one or more listed packages had load errors"
		return result, nil
	}
	result.Status = "complete"
	result.Coverage = "go_list_e_dotdot_json_pattern_completed"
	return result, nil
}

func parseGoPackageInventory(data []byte) (int, int, error) {
	type packageError struct{}
	var packages, withErrors int
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var record struct {
			Error      *packageError  `json:"Error"`
			ImportPath string         `json:"ImportPath"`
			DepsErrors []packageError `json:"DepsErrors"`
			Incomplete bool           `json:"Incomplete"`
		}
		if err := decoder.Decode(&record); err != nil {
			if err == io.EOF {
				return packages, withErrors, nil
			}
			return packages, withErrors, err
		}
		if record.ImportPath != "" {
			packages++
		}
		if record.Incomplete || record.Error != nil || len(record.DepsErrors) > 0 {
			withErrors++
		}
	}
}

func setEnv(environment []string, replacements ...string) []string {
	result := make([]string, 0, len(environment)+len(replacements))
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		replaced := false
		for index := 0; index < len(replacements); index += 2 {
			if key == replacements[index] {
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, entry)
		}
	}
	for index := 0; index < len(replacements); index += 2 {
		result = append(result, replacements[index]+"="+replacements[index+1])
	}
	return result
}
