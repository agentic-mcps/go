package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

func createRunTempDir(prefix string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locating user cache: %w", err)
	}
	runs := filepath.Join(cache, "agentic-go", "runs")
	if err := os.MkdirAll(runs, 0o700); err != nil {
		return "", fmt.Errorf("creating run cache: %w", err)
	}
	dir, err := os.MkdirTemp(runs, prefix+"-")
	if err != nil {
		return "", fmt.Errorf("creating run directory: %w", err)
	}
	return dir, nil
}
