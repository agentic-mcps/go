package intelligence

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExportChangeContractCreatesContainedPrivateCopy(t *testing.T) {
	root := snapshotRepository(t)
	core := newContractTestCore(t, root)
	contract, err := core.Begin(context.Background(), BeginRequest{Base: "HEAD", Goal: "explicitly export this handoff"})
	if err != nil {
		t.Fatal(err)
	}
	if mkdirErr := os.Mkdir(filepath.Join(root, "handoff"), 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}

	exported, err := ExportChangeContract(context.Background(), core.workspace, core.runner, core.contracts, ContractExportRequest{
		ContractID: contract.ID, Destination: "handoff/change.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if exported.ContractID != contract.ID || exported.SnapshotID != contract.LatestSnapshot.ID || exported.Path != "handoff/change.json" || exported.Digest == "" {
		t.Fatalf("export = %#v", exported)
	}
	path := filepath.Join(root, "handoff", "change.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ChangeContract
	if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(decoded, contract) {
		t.Fatalf("decoded export = %#v, error %v", decoded, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("export mode = %v, error %v", info, err)
	}
}

func TestExportChangeContractRejectsUnsafeOrAmbiguousDestinations(t *testing.T) {
	root := snapshotRepository(t)
	core := newContractTestCore(t, root)
	contract, err := core.Begin(context.Background(), BeginRequest{Base: "HEAD", Goal: "handoff"})
	if err != nil {
		t.Fatal(err)
	}
	contained := filepath.Join(root, "handoff")
	if err := os.Mkdir(contained, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(contained, "existing.json")
	if err := os.WriteFile(existing, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	for _, destination := range []string{"", ".", filepath.Join(root, "absolute.json"), "../outside.json", ".git/contract.json", "escape/contract.json", "missing/contract.json", "handoff/existing.json"} {
		t.Run(destination, func(t *testing.T) {
			_, exportErr := ExportChangeContract(context.Background(), core.workspace, core.runner, core.contracts, ContractExportRequest{
				ContractID: contract.ID, Destination: destination,
			})
			if exportErr == nil {
				t.Fatal("unsafe export unexpectedly succeeded")
			}
		})
	}
	if got, err := os.ReadFile(existing); err != nil || string(got) != "keep" {
		t.Fatalf("existing destination changed to %q, error %v", got, err)
	}
}

func TestExportChangeContractSelectsCurrentAndHonorsCancellation(t *testing.T) {
	root := snapshotRepository(t)
	core := newContractTestCore(t, root)
	contract, err := core.Begin(context.Background(), BeginRequest{Base: "HEAD", Goal: "current handoff"})
	if err != nil {
		t.Fatal(err)
	}
	if mkdirErr := os.Mkdir(filepath.Join(root, "handoff"), 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	exported, err := ExportChangeContract(context.Background(), core.workspace, core.runner, core.contracts, ContractExportRequest{Destination: "handoff/current.json"})
	if err != nil || exported.ContractID != contract.ID {
		t.Fatalf("current export = %#v, error %v", exported, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ExportChangeContract(cancelled, core.workspace, core.runner, core.contracts, ContractExportRequest{Destination: "handoff/cancelled.json"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled export error = %v", err)
	}
}
