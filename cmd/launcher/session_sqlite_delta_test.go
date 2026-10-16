package main

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/pouramin/TL-Studio/internal/sessiondb"
)

func TestSQLiteIncrementalWritersKeepMessagesAndMetadata(t *testing.T) {
	t.Setenv("TL_STUDIO_SQLITE_SESSIONS", "1")
	root, project := t.TempDir(), t.TempDir()
	a := newSessionPersistenceStore(root, "test")
	defer a.Close()
	session, err := a.createNativeSession(project, sessionCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	b := newSessionPersistenceStore(root, "test")
	defer b.Close()
	if _, _, err = b.updateTitle(session.ID, "keep concurrent title"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	fail := make(chan error, 2)
	for i, store := range []*sessionPersistenceStore{a, b} {
		wg.Add(1)
		go func(i int, s *sessionPersistenceStore) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				message := sessionMessageView{ID: fmt.Sprintf("%d-%d", i, j), Role: "assistant", Text: "preserved"}
				var err error
				for retry := 0; retry < 30; retry++ {
					err = s.putNativeMessage(session.ID, project, message)
					if !errors.Is(err, sessiondb.ErrConflict) {
						break
					}
				}
				if err != nil {
					fail <- err
					return
				}
			}
		}(i, store)
	}
	wg.Wait()
	close(fail)
	for err := range fail {
		t.Fatal(err)
	}
	msgs, ok, err := a.getMessages(session.ID)
	if err != nil || !ok || len(msgs) != 40 {
		t.Fatalf("incremental writers lost committed rows: %d %v", len(msgs), err)
	}
	actual, _, err := a.getSession(session.ID)
	if err != nil || actual.Title != "keep concurrent title" {
		t.Fatal("delta update overwrote newer metadata", err)
	}
}
