package retrievalstudy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInventoryGoPackagesReportsOutputLimit(t *testing.T) {
	toolDir := t.TempDir()
	goShim := filepath.Join(toolDir, "go")
	script := "#!/bin/sh\n/bin/dd if=/dev/zero bs=1048576 count=17 2>/dev/null\n"
	if err := os.WriteFile(goShim, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := inventoryGoPackages(context.Background(), t.TempDir(), 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "partial" || result.Coverage != "partial_output_limit" || !result.OutputTruncated {
		t.Fatalf("inventory status = %#v, want explicit output-limit coverage", result)
	}
	if result.OutputBytes != goListOutputLimit {
		t.Fatalf("captured output bytes = %d, want cap %d", result.OutputBytes, goListOutputLimit)
	}
}
