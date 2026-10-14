package sessiondb

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func writeLegacy(t *testing.T,root,id string,archived bool) (string,string){
 t.Helper()
 metadata:=map[string]any{"id":id,"directory":"/project","title":"legacy title","updatedAt":1700000000000}
 snapshot:=map[string]any{"version":1,"session":metadata,"messages":[]any{
  map[string]any{"id":"third","role":"assistant","text":"latest"},
 },"changes":[]any{map[string]any{"path":"file.txt"}}}
 if archived{snapshot["archivedBatches"]=1}
 index,_:=json.Marshal(map[string]any{"version":1,"sessions":[]any{metadata}})
 if err:=os.WriteFile(filepath.Join(root,"index.json"),index,0600);err!=nil{t.Fatal(err)}
 b,_:=json.Marshal(snapshot)
 name:=legacySessionFilename(id)
 snapshotPath:=filepath.Join(root,name+".json")
 if err:=os.WriteFile(snapshotPath,b,0600);err!=nil{t.Fatal(err)}
 archivePath:=filepath.Join(root,name+"-history-000001.json")
 if archived {
  raw,_:=json.Marshal(map[string]any{"version":1,"sessionID":id,"messages":[]any{
   map[string]any{"id":"first","role":"user","text":"important old prompt"},
   map[string]any{"id":"second","role":"assistant","text":"important old answer"},
  }})
  if err:=os.WriteFile(archivePath,raw,0600);err!=nil{t.Fatal(err)}
 }
 return snapshotPath,archivePath
}
func TestLegacyMigrationPreservesHistoryAndOriginalBytes(t *testing.T){
 root:=t.TempDir()
 snap,arc:=writeLegacy(t,root,"s1",true)
 bytesSnapshot,err:=os.ReadFile(snap);if err!=nil{t.Fatal(err)}
 bytesArchive,err:=os.ReadFile(arc);if err!=nil{t.Fatal(err)}
 db,_:=openTestStore(t)
 report,err:=db.ImportLegacy(context.Background(),root)
 if err!=nil{t.Fatal(err)}
 if report.Sessions!=1||report.Messages!=3||report.SourceDigest==""{t.Fatalf("incomplete migration: %+v",report)}
 messages,err:=db.Messages(context.Background(),"s1")
 if err!=nil||len(messages)!=3{t.Fatalf("lost legacy messages: %+v %v",messages,err)}
 if !strings.Contains(string(messages[0].Payload),"important old prompt")||messages[2].ID!="third"{t.Fatalf("message order lost: %+v",messages)}
 changes,err:=db.Changes(context.Background(),"s1")
 if err!=nil||!strings.Contains(string(changes),"file.txt"){t.Fatalf("changes lost: %s %v",changes,err)}
 nowSnap,err:=os.ReadFile(snap);if err!=nil||string(nowSnap)!=string(bytesSnapshot){t.Fatal("migration changed original snapshot")}
 nowArc,err:=os.ReadFile(arc);if err!=nil||string(nowArc)!=string(bytesArchive){t.Fatal("migration changed original archive")}
 repeated,err:=db.ImportLegacy(context.Background(),root)
 if err!=nil||!repeated.AlreadyMigrated{t.Fatalf("migration was not idempotent: %+v %v",repeated,err)}
}
func TestLegacyMigrationCorruptionBlocksPartialImport(t *testing.T){
 root:=t.TempDir()
 snap,arc:=writeLegacy(t,root,"s1",true)
 if err:=os.Remove(arc);err!=nil{t.Fatal(err)}
 db,_:=openTestStore(t)
 if _,err:=db.ImportLegacy(context.Background(),root);err==nil{t.Fatal("missing archive accepted")}
 rows,err:=db.List(context.Background(),100)
 if err!=nil||len(rows)!=0{t.Fatalf("partially imported a damaged Session: %+v %v",rows,err)}
 if _,err:=os.Stat(snap);err!=nil{t.Fatal("legacy original removed")}
 // Repair legacy bytes and retry from scratch, without deleting new DB.
 arch:=map[string]any{"version":1,"sessionID":"s1","messages":[]any{map[string]any{"id":"old","text":"restored"}}}
 data,_:=json.Marshal(arch)
 if err:=os.WriteFile(arc,data,0600);err!=nil{t.Fatal(err)}
 report,err:=db.ImportLegacy(context.Background(),root)
 if err!=nil||report.Messages!=2{t.Fatalf("failed retry after repair: %+v %v",report,err)}
}
func TestLegacyMigrationDoesNotHideOrphanOrStartEmpty(t *testing.T){
 root:=t.TempDir()
 hash:=sha256.Sum256([]byte("orphan"))
 orphan:=filepath.Join(root,"session-"+hex.EncodeToString(hash[:])+".json")
 if err:=os.WriteFile(orphan,[]byte(`{"messages":[]}`),0600);err!=nil{t.Fatal(err)}
 db,_:=openTestStore(t)
 if _,err:=db.ImportLegacy(context.Background(),root);err==nil{t.Fatal("orphan was hidden by empty migration")}
 if _,err:=os.Stat(orphan);err!=nil{t.Fatal(err)}
}
func TestLegacyMigrationPreservesPendingRun(t *testing.T){
 root:=t.TempDir()
 snap,_:=writeLegacy(t,root,"s1",false)
 raw,err:=os.ReadFile(snap);if err!=nil{t.Fatal(err)}
 var payload map[string]any
 if err=json.Unmarshal(raw,&payload);err!=nil{t.Fatal(err)}
 payload["pendingRun"]=map[string]any{"messageID":"user-1","startedAt":1700}
 newBytes,_:=json.Marshal(payload)
 if err=os.WriteFile(snap,newBytes,0600);err!=nil{t.Fatal(err)}
 db,_:=openTestStore(t)
 if _,err:=db.ImportLegacy(context.Background(),root);err!=nil{t.Fatal(err)}
 marker,err:=db.PendingRun(context.Background(),"s1")
 if err!=nil||!strings.Contains(string(marker),"user-1"){t.Fatalf("lost interrupted run marker: %s %v",marker,err)}
}
