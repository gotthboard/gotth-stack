package caddy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func TestStageDurabilityCheckpointOrderAndFailures(t *testing.T) {
	fixture := newFixture(t)
	prepared, _ := prepare(t, fixture, "operation-nine", newConfig)
	var calls []string
	fixture.adapter.ops = recordingOps(defaultStorageOps(), 0, &calls)
	if _, err := fixture.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	want := []string{"write", "sync", "close", "write", "sync", "close", "write", "sync", "close", "sync-root", "rename", "sync-root"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v", calls)
	}

	for checkpoint := 1; checkpoint <= len(want); checkpoint++ {
		t.Run(want[checkpoint-1], func(t *testing.T) {
			fixture := newFixture(t)
			prepared, _ := prepare(t, fixture, "operation-checkpoint", newConfig)
			fixture.adapter.ops = recordingOps(defaultStorageOps(), checkpoint, nil)
			if _, err := fixture.adapter.Stage(prepared); !errors.Is(err, ErrStorage) {
				t.Fatalf("checkpoint %d got %v", checkpoint, err)
			}
			fixture.adapter.ops = defaultStorageOps()
			_, state, err := fixture.adapter.ReconcileStage("operation-checkpoint")
			if checkpoint < 7 {
				if state != StateIncomplete || !errors.Is(err, ErrRecoveryRequired) {
					t.Fatalf("checkpoint %d reconcile = %q, %v", checkpoint, state, err)
				}
			} else if state != StatePresent || err != nil {
				t.Fatalf("checkpoint %d reconcile = %q, %v", checkpoint, state, err)
			}
		})
	}
}

func TestConfigReplacementDurabilityCheckpointFailures(t *testing.T) {
	for checkpoint := 1; checkpoint <= 7; checkpoint++ {
		t.Run(string(rune('0'+checkpoint)), func(t *testing.T) {
			fixture := newFixture(t)
			prepared, _ := prepare(t, fixture, "operation-replace", newConfig)
			if _, err := fixture.adapter.Stage(prepared); err != nil {
				t.Fatal(err)
			}
			fixture.adapter.ops = recordingOps(defaultStorageOps(), checkpoint, nil)
			if _, err := fixture.adapter.Install("operation-replace"); !errors.Is(err, ErrStorage) {
				t.Fatalf("checkpoint %d got %v", checkpoint, err)
			}
			fixture.adapter.ops = defaultStorageOps()
			observation, err := fixture.adapter.Observe(context.Background(), "operation-replace")
			if err != nil {
				t.Fatal(err)
			}
			want := StatePrevious
			if checkpoint >= 6 {
				want = StateCandidate
			}
			if observation.FileState != want {
				t.Fatalf("checkpoint %d file = %q", checkpoint, observation.FileState)
			}
		})
	}
}

func TestConfigReplacementCheckpointOrder(t *testing.T) {
	fixture := newFixture(t)
	prepared, _ := prepare(t, fixture, "operation-replace-order", newConfig)
	if _, err := fixture.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	var calls []string
	fixture.adapter.ops = recordingOps(defaultStorageOps(), 0, &calls)
	if _, err := fixture.adapter.Install("operation-replace-order"); err != nil {
		t.Fatal(err)
	}
	want := []string{"chown", "chmod", "write", "sync", "close", "rename", "sync-root"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestRestoreRemovesFailedInstallResidue(t *testing.T) {
	fixture := newFixture(t)
	prepared, _ := prepare(t, fixture, "operation-residue", newConfig)
	if _, err := fixture.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	fixture.adapter.ops = recordingOps(defaultStorageOps(), 3, nil)
	if _, err := fixture.adapter.Install("operation-residue"); !errors.Is(err, ErrStorage) {
		t.Fatalf("install got %v", err)
	}
	fixture.adapter.ops = defaultStorageOps()
	observation, err := fixture.adapter.Observe(context.Background(), "operation-residue")
	if err != nil || observation.FileState != StatePrevious || observation.FileTemporary != StateCandidate {
		t.Fatalf("failed install observation = %#v, %v", observation, err)
	}
	if err := fixture.adapter.RollbackStage(context.Background(), "operation-residue"); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("stage rollback ignored file residue: %v", err)
	}
	if _, err := fixture.adapter.Restore("operation-residue"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.Verify(context.Background(), "operation-residue", DesiredPrevious); err != nil {
		t.Fatal(err)
	}
}

func TestConfigReplacementPreservesModeDespiteUmask(t *testing.T) {
	fixture := newFixture(t)
	if err := os.Chmod(fixture.options.ConfigPath, 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, _ := prepare(t, fixture, "operation-mode", newConfig)
	if _, err := fixture.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	previousUmask := syscall.Umask(0o077)
	defer syscall.Umask(previousUmask)
	if _, err := fixture.adapter.Install("operation-mode"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(fixture.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %04o", info.Mode().Perm())
	}
}

func recordingOps(base storageOps, failure int, calls *[]string) storageOps {
	count := 0
	run := func(name string, operation func() error) error {
		count++
		if calls != nil {
			*calls = append(*calls, name)
		}
		err := operation()
		if count == failure {
			return errors.New("injected checkpoint failure")
		}
		return err
	}
	return storageOps{
		chown: func(file *os.File, uid, gid int) error {
			return run("chown", func() error { return base.chown(file, uid, gid) })
		},
		chmod: func(file *os.File, mode os.FileMode) error {
			return run("chmod", func() error { return base.chmod(file, mode) })
		},
		write: func(file *os.File, value []byte) error {
			return run("write", func() error { return base.write(file, value) })
		},
		sync:  func(file *os.File) error { return run("sync", func() error { return base.sync(file) }) },
		close: func(file *os.File) error { return run("close", func() error { return base.close(file) }) },
		rename: func(root *os.Root, oldName, newName string) error {
			return run("rename", func() error { return base.rename(root, oldName, newName) })
		},
		syncRoot: func(root *os.Root) error { return run("sync-root", func() error { return base.syncRoot(root) }) },
	}
}

func TestTamperedTransactionAndMissingStageFailClosed(t *testing.T) {
	fixture := newFixture(t)
	if _, err := fixture.adapter.Install("missing-operation"); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("missing got %v", err)
	}
	prepared, _ := prepare(t, fixture, "operation-tamper", newConfig)
	if _, err := fixture.adapter.Stage(prepared); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.options.StateRoot, transactionDirName, "operation-tamper", metadataName)
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.Install("operation-tamper"); !errors.Is(err, ErrUnsafeState) {
		t.Fatalf("tamper got %v", err)
	}
}
