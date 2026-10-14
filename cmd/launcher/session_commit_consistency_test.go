package main

import (
  "os"
  "path/filepath"
  "testing"
)

func TestSessionFailedSnapshotWriteCannotLeakIntoLaterIndex(t *testing.T) {
  dir, project := t.TempDir(), t.TempDir()
  store := newSessionPersistenceStore(dir,"test")
  s1,err:=store.createNativeSession(project,sessionCreateInput{Title:"v1"})
  if err!=nil { t.Fatal(err) }
  snapshotPath:=store.snapshotPath(s1.ID)
  backup:=snapshotPath+".saved"
  if err:=os.Rename(snapshotPath,backup);err!=nil {t.Fatal(err)}
  if err:=os.Mkdir(snapshotPath,0o700);err!=nil {t.Fatal(err)}
  if err:=os.WriteFile(filepath.Join(snapshotPath,"blocker"),[]byte("fault"),0o600);err!=nil {t.Fatal(err)}
  attempted:=s1
  attempted.Title="v2"
  if err:=store.upsertSession(attempted);err==nil {t.Fatal("injected snapshot failure was not surfaced")}
  current,ok,err:=store.getSession(s1.ID)
  if err!=nil||!ok||current.Title!="v1" {t.Fatalf("unsaved title leaked into in-memory index: %+v %v %v",current,ok,err)}
  if err:=os.RemoveAll(snapshotPath);err!=nil {t.Fatal(err)}
  if err:=os.Rename(backup,snapshotPath);err!=nil {t.Fatal(err)}
  if _,err:=store.createNativeSession(project,sessionCreateInput{Title:"unrelated"});err!=nil {t.Fatal(err)}
  fresh:=newSessionPersistenceStore(dir,"test")
  restored,ok,err:=fresh.getSession(s1.ID)
  if err!=nil||!ok||restored.Title!="v1" {t.Fatalf("later unrelated update committed a rejected title: %+v %v %v",restored,ok,err)}
}

func TestSessionFailedIndexSaveRollsBackSnapshotAndMemory(t *testing.T) {
  dir,project:=t.TempDir(),t.TempDir()
  store:=newSessionPersistenceStore(dir,"test")
  session,err:=store.createNativeSession(project,sessionCreateInput{Title:"v1"})
  if err!=nil {t.Fatal(err)}
  indexPath:=store.indexPath()
  backup:=indexPath+".saved"
  if err:=os.Rename(indexPath,backup);err!=nil {t.Fatal(err)}
  if err:=os.Mkdir(indexPath,0o700);err!=nil {t.Fatal(err)}
  if err:=os.WriteFile(filepath.Join(indexPath,"blocker"),[]byte("fault"),0o600);err!=nil {t.Fatal(err)}
  updated:=session
  updated.Title="v2"
  if err:=store.upsertSession(updated);err==nil {t.Fatal("injected index failure was not surfaced")}
  current,ok,err:=store.getSession(session.ID)
  if err!=nil||!ok||current.Title!="v1" {t.Fatalf("index failure changed memory: %+v %v %v",current,ok,err)}
  diskSnapshot,err:=store.loadSnapshotLocked(session.ID)
  if err!=nil||diskSnapshot.Session.Title!="v1" {t.Fatalf("index failure left uncommitted snapshot: %+v %v",diskSnapshot.Session,err)}
  if err:=os.RemoveAll(indexPath);err!=nil {t.Fatal(err)}
  if err:=os.Rename(backup,indexPath);err!=nil {t.Fatal(err)}
  if _,err:=store.createNativeSession(project,sessionCreateInput{Title:"unrelated"});err!=nil {t.Fatal(err)}
  reloaded:=newSessionPersistenceStore(dir,"test")
  onDisk,ok,err:=reloaded.getSession(session.ID)
  if err!=nil||!ok||onDisk.Title!="v1" {t.Fatalf("index failure later leaked rejected update: %+v %v %v",onDisk,ok,err)}
}

func TestSessionFailedIndexDeletionPreservesSession(t *testing.T) {
  dir,project:=t.TempDir(),t.TempDir()
  store:=newSessionPersistenceStore(dir,"test")
  session,err:=store.createNativeSession(project,sessionCreateInput{Title:"must survive"})
  if err!=nil {t.Fatal(err)}
  indexPath:=store.indexPath()
  backup:=indexPath+".saved"
  if err:=os.Rename(indexPath,backup);err!=nil {t.Fatal(err)}
  if err:=os.Mkdir(indexPath,0o700);err!=nil {t.Fatal(err)}
  if err:=os.WriteFile(filepath.Join(indexPath,"blocker"),[]byte("fault"),0o600);err!=nil {t.Fatal(err)}
  if err:=store.remove(session.ID);err==nil {t.Fatal("injected removal failure not surfaced")}
  got,ok,err:=store.getSession(session.ID)
  if err!=nil||!ok||got.Title!="must survive" {t.Fatalf("failed deletion mutated cache: %+v %v %v",got,ok,err)}
  if err:=os.RemoveAll(indexPath);err!=nil {t.Fatal(err)}
  if err:=os.Rename(backup,indexPath);err!=nil {t.Fatal(err)}
  if _,err:=store.createNativeSession(project,sessionCreateInput{Title:"unrelated"});err!=nil {t.Fatal(err)}
  reloaded:=newSessionPersistenceStore(dir,"test")
  restored,ok,err:=reloaded.getSession(session.ID)
  if err!=nil||!ok||restored.Title!="must survive" {t.Fatalf("failed deletion leaked: %+v %v %v",restored,ok,err)}
}
