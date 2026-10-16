package sessiondb

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func crashMutation(operation string) Mutation {
	m := Mutation{OperationID: "crash-op", SessionID: "s", Directory: "/p", Metadata: []byte("new"), ExpectedRevision: 1, ReplaceMessages: true, Messages: []Message{{ID: "new", Payload: []byte("new")}}, ChangeData: []byte("new"), PendingRun: []byte("new"), UpdatePendingRun: true}
	if operation == "create" {
		m.SessionID = "created"
		m.ExpectedRevision = 0
	}
	if operation == "delete" {
		m.Delete = true
	}
	return m
}

func killAtPhase(t *testing.T, testName, root, phase, operation string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), "TL_DURABILITY_CHILD="+root, "TL_DURABILITY_PHASE="+phase, "TL_DURABILITY_OPERATION="+operation)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ready := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "PAUSED" {
				ready <- nil
				return
			}
		}
		ready <- fmt.Errorf("child did not reach phase %s", phase)
	}()
	select {
	case err = <-ready:
		if err != nil {
			t.Fatalf("%v: %s", err, stderr.String())
		}
	case <-time.After(45 * time.Second):
		t.Fatal("child phase timed out", phase)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
}

func installCrashHook(db *Store) {
	db.testHook = func(phase string) {
		if phase == os.Getenv("TL_DURABILITY_PHASE") {
			fmt.Println("PAUSED")
			select {}
		}
	}
}

func TestDeterministicSQLiteCrashMatrix(t *testing.T) {
	ctx := context.Background()
	if root := os.Getenv("TL_DURABILITY_CHILD"); root != "" {
		db, err := Open(filepath.Join(root, "state.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Apply(ctx, Mutation{OperationID: "seed", SessionID: "s", Directory: "/p", Metadata: []byte("old"), Messages: []Message{{ID: "old", Payload: []byte("old")}}, ChangeData: []byte("old"), PendingRun: []byte("old"), UpdatePendingRun: true})
		if err != nil {
			t.Fatal(err)
		}
		installCrashHook(db)
		if _, err = db.Apply(ctx, crashMutation(os.Getenv("TL_DURABILITY_OPERATION"))); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, op := range []string{"create", "update", "delete"} {
		for _, phase := range []string{"apply.begin", "apply.metadata", "apply.messages", "apply.before_commit", "apply.after_commit"} {
			if op == "delete" && (phase == "apply.metadata" || phase == "apply.messages") {
				continue
			}
			t.Run(op+"/"+phase, func(t *testing.T) {
				root := t.TempDir()
				killAtPhase(t, "TestDeterministicSQLiteCrashMatrix", root, phase, op)
				db, err := Open(filepath.Join(root, "state.sqlite"))
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if err = db.IntegrityCheck(ctx); err != nil {
					t.Fatal(err)
				}
				m := crashMutation(op)
				session, msgs, changes, pending, found, err := db.ReadSnapshot(ctx, m.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				committed := phase == "apply.after_commit"
				if op == "create" && !committed || op == "delete" && committed {
					if found {
						t.Fatal("unexpected session visibility")
					}
				} else {
					want := "old"
					revision := int64(1)
					if committed {
						want = "new"
						revision = 2
						if op == "create" {
							revision = 1
						}
					}
					if !found || session.Revision != revision || string(session.Metadata) != want || len(msgs) != 1 || string(msgs[0].Payload) != want || string(changes) != want || string(pending) != want {
						t.Fatalf("partial generation after kill: %+v %+v %s %s", session, msgs, changes, pending)
					}
				}
				// COMMIT may precede caller acknowledgment. Retrying the identical
				// operation reconciles the journal instead of executing it again.
				result, err := db.Apply(ctx, m)
				if err != nil || result.PreviouslyCommitted != committed {
					t.Fatalf("ambiguous acknowledgment replay: %+v %v", result, err)
				}
			})
		}
	}
}

func TestLegacyMigrationCrashBoundaries(t *testing.T) {
	ctx := context.Background()
	if root := os.Getenv("TL_DURABILITY_CHILD"); root != "" {
		db, err := Open(filepath.Join(root, "state.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		installCrashHook(db)
		if _, err = db.ImportLegacy(ctx, root); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, phase := range []string{"import.begin", "import.session", "import.before_commit", "import.after_commit"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			snap, archive := writeLegacy(t, root, "s1", true)
			before, _ := os.ReadFile(snap)
			oldArc, _ := os.ReadFile(archive)
			killAtPhase(t, "TestLegacyMigrationCrashBoundaries", root, phase, "")
			db, err := Open(filepath.Join(root, "state.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			rows, err := db.List(ctx, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 0 && phase != "import.after_commit" {
				t.Fatal("partial migration visible")
			}
			report, err := db.ImportLegacy(ctx, root)
			if err != nil || report.Sessions != 1 || report.Messages != 3 {
				t.Fatalf("retry failed: %+v %v", report, err)
			}
			if report.AlreadyMigrated != (phase == "import.after_commit") {
				t.Fatal("migration commit marker does not match visibility")
			}
			if now, _ := os.ReadFile(snap); !bytes.Equal(now, before) {
				t.Fatal("source snapshot changed")
			}
			if now, _ := os.ReadFile(archive); !bytes.Equal(now, oldArc) {
				t.Fatal("source archive changed")
			}
			if !strings.Contains(report.BackupPath, "legacy-backups") {
				t.Fatal("migration lacks preserved source backup")
			}
		})
	}
}

func TestBackupPublicationCrashBoundaries(t *testing.T) {
	ctx := context.Background()
	if root := os.Getenv("TL_DURABILITY_CHILD"); root != "" {
		db, err := Open(filepath.Join(root, "state.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Apply(ctx, Mutation{OperationID: "seed", SessionID: "s", Directory: "/p", Metadata: []byte("{}")}); err != nil {
			t.Fatal(err)
		}
		installCrashHook(db)
		if _, err = db.Backup(ctx, filepath.Join(root, "backup.sqlite")); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, phase := range []string{"backup.before_publish", "backup.after_publish"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			killAtPhase(t, "TestBackupPublicationCrashBoundaries", root, phase, "")
			backup := filepath.Join(root, "backup.sqlite")
			if phase == "backup.before_publish" {
				if _, err := os.Stat(backup); !os.IsNotExist(err) {
					t.Fatal("interrupted backup published partial bytes")
				}
				return
			}
			db, report, err := readOnlyBackup(ctx, backup)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if report.Sessions != 1 {
				t.Fatal("published backup incomplete")
			}
		})
	}
}
