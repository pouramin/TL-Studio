package sessiondb

import (
	"context"
	"testing"
)

func TestIdempotencyDistinguishesAbsentAndEmptyMutationFields(t *testing.T) {
	db, _ := openTestStore(t)
	ctx := context.Background()
	m := Mutation{OperationID: "exact", SessionID: "s", Directory: "/p", Metadata: []byte("{}")}
	if _, err := db.Apply(ctx, m); err != nil {
		t.Fatal(err)
	}
	changed := m
	changed.ChangeData = []byte{}
	if _, err := db.Apply(ctx, changed); err == nil {
		t.Fatal("same operation ID accepted a semantically different mutation")
	}
}

func TestIdempotencyReconcilesOlderJournalFingerprints(t *testing.T) {
	db, _ := openTestStore(t)
	ctx := context.Background()
	m := Mutation{OperationID: "old", SessionID: "s", Directory: "/p", Metadata: []byte("{}")}
	if _, err := db.Apply(ctx, m); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("UPDATE commits SET fingerprint=? WHERE operation_id=?", legacyFingerprint(m), m.OperationID); err != nil {
		t.Fatal(err)
	}
	result, err := db.Apply(ctx, m)
	if err != nil || !result.PreviouslyCommitted {
		t.Fatalf("old committed operation was replayed/rejected: %+v %v", result, err)
	}
}
