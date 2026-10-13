package main

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestPermissionPolicyCorruptReadCannotBecomeCachedEmptyPolicy(t *testing.T) {
    path := filepath.Join(t.TempDir(), "permissions.json")
    corrupt := []byte("{malformed-json")
    if err := os.WriteFile(path, corrupt, 0o600); err != nil { t.Fatal(err) }
    store := newPermissionPolicyStore(path)
    for i := 0; i < 2; i++ {
        if _, err := store.snapshot(); err == nil {
            t.Fatalf("attempt %d: malformed permission rules silently accepted", i+1)
        }
    }
    if _, err := store.addAllowRules(t.TempDir(), "write", []string{"*"}); err == nil {
        t.Fatal("mutating an unreadable policy must not overwrite damaged rules")
    }
    persisted, err := os.ReadFile(path)
    if err != nil { t.Fatal(err) }
    if string(persisted) != string(corrupt) {
        t.Fatalf("failed policy load overwrote the only prior copy: %q", persisted)
    }

    valid := []byte(`{"version":1,"rules":[{"id":"existing","project":"/test","permission":"write","matcher":"old","decision":"allow","createdAt":"2026-10-13T00:00:00Z"}]}`)
    if err := os.WriteFile(path, valid, 0o600); err != nil { t.Fatal(err) }
    rules, err := store.snapshot()
    if err != nil { t.Fatalf("policy did not retry after restoring file: %v", err) }
    if len(rules)!=1 || rules[0].ID!="existing" {
        t.Fatalf("recovered policy lost rules: %#v", rules)
    }
}

func TestPermissionPolicyRemovalFailureRollsBackMemory(t *testing.T) {
    path := filepath.Join(t.TempDir(), "permissions.json")
    store := newPermissionPolicyStore(path)
    ids, err := store.addAllowRules(t.TempDir(), "write", []string{"src/*.go"})
    if err != nil || len(ids)!=1 { t.Fatalf("seed rules: %v %+v", err, ids) }
    initial, err := os.ReadFile(path)
    if err != nil { t.Fatal(err) }

    blocker := filepath.Join(t.TempDir(), "not-a-directory")
    if err := os.WriteFile(blocker, []byte("block"), 0o600); err != nil { t.Fatal(err) }
    store.filePath = filepath.Join(blocker, "permissions.json")

    if _, err := store.remove(ids[0]); err == nil {
        t.Fatal("expected policy persist failure on single-rule removal")
    }
    rules, err := store.snapshot()
    if err != nil || len(rules)!=1 {
        t.Fatalf("failed single removal changed in-memory rules: %v %#v", err, rules)
    }
    if err := store.removeIDs(ids); err == nil {
        t.Fatal("expected policy persist failure on multiple-rule removal")
    }
    rules, err = store.snapshot()
    if err != nil || len(rules)!=1 {
        t.Fatalf("failed batch removal changed in-memory rules: %v %#v", err, rules)
    }
    store.filePath = path
    after, err := os.ReadFile(path)
    if err != nil { t.Fatal(err) }
    if !strings.EqualFold(string(initial), string(after)) {
        t.Fatalf("failed persist modified existing policy")
    }
}
