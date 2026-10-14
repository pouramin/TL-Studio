package main

import (
 "errors"
 "testing"

 "github.com/pouramin/TL-Studio/internal/sessiondb"
)

// This test specifically protects the adapter boundary, not just the SQL
// repository: two launcher instances must never rebase a stale JSON Snapshot
// on the *latest* SQLite revision without conflict detection.
func TestSQLiteAdapterRejectsLostUpdateAcrossLaunchers(t *testing.T){
 t.Setenv("TL_STUDIO_SQLITE_SESSIONS","1")
 root,project:=t.TempDir(),t.TempDir()
 first:=newSessionPersistenceStore(root,"native")
 created,err:=first.createNativeSession(project,sessionCreateInput{Title:"first title"})
 if err!=nil{t.Fatal(err)}
 second:=newSessionPersistenceStore(root,"native")
 if _,ok,err:=second.getSession(created.ID);err!=nil||!ok{t.Fatalf("second launcher failed to load Session: %v",err)}
 stale,err:=first.loadSnapshotLocked(created.ID)
 if err!=nil{t.Fatal(err)}
 if stale.DBRevision!=1{t.Fatalf("expected first committed revision, got %d",stale.DBRevision)}
 renamed,ok,err:=second.updateTitle(created.ID,"second launcher's edit")
 if err!=nil||!ok||renamed.Title!="second launcher's edit"{t.Fatalf("concurrent title update failed: %+v %v",renamed,err)}
 stale.Session.Title="stale first launcher's edit"
 if err=first.sqliteCommitSnapshotLocked(stale.Session,stale);!errors.Is(err,sessiondb.ErrConflict){
  t.Fatalf("stale snapshot overwrote or failed without conflict: %v",err)
 }
 actual,ok,err:=first.getSession(created.ID)
 if err!=nil||!ok||actual.Title!="second launcher's edit"{
  t.Fatalf("lost another launcher's committed metadata: %+v %v",actual,err)
 }
 if first.sqlite!=nil{_ = first.sqlite.Close()}
 if second.sqlite!=nil{_ = second.sqlite.Close()}
}
