package main

import (
 "context"
 "os"
 "testing"
)

func TestSQLiteSessionOptInPreservesNativeHistoryAfterRestart(t *testing.T){
 t.Setenv("TL_STUDIO_SQLITE_SESSIONS","1")
 root,project:=t.TempDir(),t.TempDir()
 db:=newSessionPersistenceStore(root,"test")
 s,err:=db.createNativeSession(project,sessionCreateInput{Title:"SQLite native"})
 if err!=nil{t.Fatal(err)}
 if err=db.recordAcceptedRun(s.ID,project,sessionRunInput{Text:"important question",MessageID:"user-one"});err!=nil{t.Fatal(err)}
 if err=db.putNativeFinalMessage(s.ID,project,sessionMessageView{ID:"assistant-one",Role:"assistant",Text:"important answer"});err!=nil{t.Fatal(err)}
 if db.sqlite==nil{t.Fatal("SQLite backend was not enabled")}
 if err=db.sqlite.Close();err!=nil{t.Fatal(err)}
 fresh:=newSessionPersistenceStore(root,"test")
 messages,ok,err:=fresh.getMessages(s.ID)
 if err!=nil||!ok||len(messages)!=2{t.Fatalf("restart dropped native history: %+v %v",messages,err)}
 if messages[0].Text!="important question"||messages[1].Text!="important answer"{t.Fatalf("history changed: %+v",messages)}
 if fresh.sqlite==nil{t.Fatal("reopened store did not use SQLite")}
 if err=fresh.sqlite.IntegrityCheck(context.Background());err!=nil{t.Fatal(err)}
 fresh.sqlite.Close()
}

func TestSQLiteSessionOptInImportsExistingJSONWithoutMutatingIt(t *testing.T){
 t.Setenv("TL_STUDIO_SQLITE_SESSIONS","0")
 root,project:=t.TempDir(),t.TempDir()
 old:=newSessionPersistenceStore(root,"legacy")
 s,err:=old.createNativeSession(project,sessionCreateInput{Title:"legacy"})
 if err!=nil{t.Fatal(err)}
 if err=old.putNativeMessage(s.ID,project,sessionMessageView{ID:"legacy-msg",Role:"user",Text:"preserved"});err!=nil{t.Fatal(err)}
 before,err:=os.ReadFile(old.snapshotPath(s.ID));if err!=nil{t.Fatal(err)}
 indexBefore,err:=os.ReadFile(old.indexPath());if err!=nil{t.Fatal(err)}
 t.Setenv("TL_STUDIO_SQLITE_SESSIONS","1")
 migrated:=newSessionPersistenceStore(root,"new")
 messages,ok,err:=migrated.getMessages(s.ID)
 if err!=nil||!ok||len(messages)!=1||messages[0].Text!="preserved"{t.Fatalf("legacy migration failed: %+v %v",messages,err)}
 after,err:=os.ReadFile(old.snapshotPath(s.ID));if err!=nil||string(before)!=string(after){t.Fatal("legacy snapshot mutated")}
 idx,err:=os.ReadFile(old.indexPath());if err!=nil||string(indexBefore)!=string(idx){t.Fatal("legacy index mutated")}
 migrated.sqlite.Close()
}

func TestSQLiteSessionOptInRefusesDamagedLegacyIndex(t *testing.T){
 t.Setenv("TL_STUDIO_SQLITE_SESSIONS","1")
 root:=t.TempDir()
 original:=[]byte("{broken")
 if err:=os.WriteFile(root+"/index.json",original,0600);err!=nil{t.Fatal(err)}
 sqlite:=newSessionPersistenceStore(root,"new")
 if _,err:=sqlite.list(10);err==nil{t.Fatal("damaged legacy Session history must not become an empty store")}
 raw,err:=os.ReadFile(root+"/index.json")
 if err!=nil||string(raw)!=string(original){t.Fatal("damaged legacy original modified")}
}
