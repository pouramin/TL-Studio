package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateJSONAtomicWriteOrdersSyncBeforeRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	const old = "original durable state"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil { t.Fatal(err) }
	stages := []string{}
	err := writePrivateJSONAtomicWithIO(path, map[string]string{"state":"new"}, privateJSONAtomicIO{
		syncFile: func(f *os.File) error {
			stages = append(stages, "sync")
			live, readErr := os.ReadFile(path)
			if readErr != nil || string(live) != old { t.Fatalf("live file changed before sync: %s %v", live, readErr) }
			pending, readErr := os.ReadFile(f.Name())
			if readErr != nil || !strings.Contains(string(pending), `"state": "new"`) {
				t.Fatalf("staged file was not written before sync: %s %v", pending, readErr)
			}
			return f.Sync()
		},
		rename: func(from,to string) error {
			stages = append(stages,"rename")
			return os.Rename(from,to)
		},
	})
	if err != nil { t.Fatal(err) }
	if strings.Join(stages,",")!="sync,rename" { t.Fatalf("invalid durable write order: %v",stages) }
	got, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(got), `"state": "new"`) { t.Fatalf("replacement was not committed: %s %v",got,err) }
}

func TestPrivateJSONAtomicWriteFailurePreservesExistingData(t *testing.T) {
	for _, failure := range []string{"sync","rename"} {
		t.Run(failure,func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir,"state.json")
			const original = "important original session and permission data"
			if err := os.WriteFile(path,[]byte(original),0o600); err != nil { t.Fatal(err) }
			didSync,didRename := false,false
			fail := errors.New("injected durable write failure")
			err := writePrivateJSONAtomicWithIO(path,map[string]string{"state":"replacement"},privateJSONAtomicIO{
				syncFile:func(f *os.File)error {
					didSync=true
					if failure=="sync" { return fail }
					return f.Sync()
				},
				rename:func(from,to string)error {
					didRename=true
					if failure=="rename" { return fail }
					return os.Rename(from,to)
				},
			})
			if !errors.Is(err,fail) { t.Fatalf("expected injected error, got %v",err) }
			if !didSync || (didRename != (failure=="rename")) {
				t.Fatalf("unsafe write transition: synced=%v renamed=%v failure=%s",didSync,didRename,failure)
			}
			got,err := os.ReadFile(path)
			if err!=nil || string(got)!=original { t.Fatalf("existing state lost after %s error: %s %v",failure,got,err) }
			items,err := os.ReadDir(dir)
			if err!=nil || len(items)!=1 { t.Fatalf("staged files leaked after error: %v %v",items,err) }
		})
	}
}
