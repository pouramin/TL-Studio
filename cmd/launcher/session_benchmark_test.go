package main

import (
	"fmt"
	"testing"
)

func benchmarkSession(b *testing.B, n int) (*sessionPersistenceStore, string) {
	b.Helper()
	b.Setenv("TL_STUDIO_SQLITE_SESSIONS", "1")
	store := newSessionPersistenceStore(b.TempDir(), "benchmark")
	b.Cleanup(store.Close)
	session, err := store.createNativeSession(b.TempDir(), sessionCreateInput{})
	if err != nil {
		b.Fatal(err)
	}
	snapshot, err := store.loadSnapshotLocked(session.ID)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < n; i++ {
		snapshot.Messages = append(snapshot.Messages, sessionMessageView{ID: fmt.Sprintf("m-%05d", i), Role: "user", Text: "A representative historical message with enough content to account for JSON and model-history allocations.", Attachments: []sessionAttachmentView{{Name: "source.txt", MIME: "text/plain", Text: "Preserve this uploaded source across updates and backups."}}})
	}
	if err = store.sqliteCommitSnapshotLocked(session, snapshot); err != nil {
		b.Fatal(err)
	}
	return store, session.ID
}

func BenchmarkSQLiteNativeHistory(b *testing.B) {
	for _, n := range []int{1200, 10000} {
		b.Run(fmt.Sprintf("tail-200/%d", n), func(b *testing.B) {
			store, id := benchmarkSession(b, n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, err := store.getRecentMessages(id, 200); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("update/%d", n), func(b *testing.B) {
			store, id := benchmarkSession(b, n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := store.putNativeFinalMessage(id, "", sessionMessageView{ID: "latest", Role: "assistant", Text: fmt.Sprint("updated ", i)}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
