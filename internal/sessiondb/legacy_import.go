package sessiondb

import (
 "bytes"
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "sort"
 "strings"
)

// MigrationReport describes the bytes that were proven accessible in the
// legacy store. It never implies that older releases did not truncate history.
type MigrationReport struct {
 Sessions int
 Messages int
 AlreadyMigrated bool
 PossibleTruncation []string
 SourceDigest string
}
type legacyIndex struct {
 Version int `json:"version"`
 Sessions []json.RawMessage `json:"sessions"`
}
type legacySnapshot struct {
 Version int `json:"version"`
 Session json.RawMessage `json:"session"`
 Messages []json.RawMessage `json:"messages"`
 Changes json.RawMessage `json:"changes"`
 ArchivedBatches int `json:"archivedBatches"`
 PendingRun json.RawMessage `json:"pendingRun"`
}
type legacyArchive struct {
 Version int `json:"version"`
 SessionID string `json:"sessionID"`
 Messages []json.RawMessage `json:"messages"`
}
type legacySessionKey struct {
 ID string `json:"id"`
 Directory string `json:"directory"`
 UpdatedAt int64 `json:"updatedAt"`
}
type importedSession struct {
 key legacySessionKey
 metadata []byte
 updatedAt int64
 messages []Message
 changes []byte
 pending []byte
}

func canonical(raw []byte)([]byte,error){
 if len(bytes.TrimSpace(raw))==0{return nil,errors.New("missing legacy JSON value")}
 var compact bytes.Buffer
 if err:=json.Compact(&compact,raw);err!=nil{return nil,err}
 return compact.Bytes(),nil
}

func legacySessionFilename(id string)string{
 hash:=sha256.Sum256([]byte(strings.TrimSpace(id)))
 return "session-"+hex.EncodeToString(hash[:])
}

// ImportLegacy imports the *accessible* legacy history atomically. This
// operation must run before the SQLite authority is used for new writes.
// On any malformed, missing or inconsistent source it fails without
// modifying the legacy files or partially exposing imported sessions.
func (s *Store) ImportLegacy(ctx context.Context,root string)(MigrationReport,error){
 report:=MigrationReport{}
 tx,err:=s.db.BeginTx(ctx,nil)
 if err!=nil{return report,err}
 defer tx.Rollback()
 if _,err=tx.ExecContext(ctx,`CREATE TABLE IF NOT EXISTS migration_state (
    name TEXT PRIMARY KEY, source_digest TEXT NOT NULL)`);err!=nil{return report,err}
 var existing string
 err=tx.QueryRowContext(ctx,"SELECT source_digest FROM migration_state WHERE name='legacy-v1'").Scan(&existing)
 if err==nil{
  report.AlreadyMigrated=true;report.SourceDigest=existing
  return report,nil
 }
 if !errors.Is(err,sql.ErrNoRows){return report,err}
 var already int
 if err=tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM sessions").Scan(&already);err!=nil{return report,err}
 if already>0{return report,errors.New("refusing migration: SQLite store already contains data without a completed migration marker")}

 indexPath:=filepath.Join(root,"index.json")
 content,err:=os.ReadFile(indexPath)
 if errors.Is(err,os.ErrNotExist){
  // An absent index and present session snapshots is not an empty store.
  orphan,glErr:=filepath.Glob(filepath.Join(root,"session-*.json"))
  if glErr!=nil{return report,glErr}
  if len(orphan)>0{return report,fmt.Errorf("legacy index missing while %d JSON files remain; refusing empty migration",len(orphan))}
  content=[]byte(`{"version":1,"sessions":[]}`)
 }else if err!=nil{return report,err}
 var index legacyIndex
 if err=json.Unmarshal(content,&index);err!=nil{return report,fmt.Errorf("decode legacy index: %w",err)}
 if index.Version!=0&&index.Version!=1{return report,fmt.Errorf("unsupported legacy index format: %d",index.Version)}

 sessions:=make([]importedSession,0,len(index.Sessions))
 knownPaths:=map[string]bool{}
 digest:=sha256.New()
 digest.Write([]byte("tl-studio-legacy-v1\x00"))
 digest.Write(content)
 for _,entry:=range index.Sessions{
  indexValue,err:=canonical(entry);if err!=nil{return report,err}
  var indexKey legacySessionKey
  if err=json.Unmarshal(indexValue,&indexKey);err!=nil{return report,err}
  if indexKey.ID==""||indexKey.Directory==""{return report,errors.New("legacy Session has no id or directory")}
  pathPrefix:=legacySessionFilename(indexKey.ID)
  path:=filepath.Join(root,pathPrefix+".json")
  if knownPaths[path]{return report,fmt.Errorf("duplicate Session %s in legacy index",indexKey.ID)}
  knownPaths[path]=true
  data,readErr:=os.ReadFile(path)
  if readErr!=nil{return report,fmt.Errorf("legacy Session %s snapshot missing or unreadable: %w",indexKey.ID,readErr)}
  digest.Write(data)
  var snapshot legacySnapshot
  if err=json.Unmarshal(data,&snapshot);err!=nil{return report,fmt.Errorf("decode Session %s: %w",indexKey.ID,err)}
  if snapshot.Version!=0&&snapshot.Version!=1{return report,fmt.Errorf("unsupported Session %s snapshot version %d",indexKey.ID,snapshot.Version)}
  snapshotMetadata,err:=canonical(snapshot.Session);if err!=nil{return report,err}
  var snapshotKey legacySessionKey
  if err=json.Unmarshal(snapshotMetadata,&snapshotKey);err!=nil{return report,err}
  if snapshotKey.ID!=indexKey.ID||snapshotKey.Directory!=indexKey.Directory{
   return report,fmt.Errorf("legacy Session %s index/snapshot metadata divergence; require explicit recovery",indexKey.ID)
  }
  rows:=[]json.RawMessage{}
  if snapshot.ArchivedBatches<0||snapshot.ArchivedBatches>100000 {return report,errors.New("invalid legacy archive count")}
  for i:=1;i<=snapshot.ArchivedBatches;i++ {
   name:=filepath.Join(root,fmt.Sprintf("%s-history-%06d.json",pathPrefix,i))
   archiveRaw,readErr:=os.ReadFile(name)
   if readErr!=nil{return report,fmt.Errorf("Session %s archive %d missing: %w",indexKey.ID,i,readErr)}
   digest.Write(archiveRaw)
   var archive legacyArchive
   if err=json.Unmarshal(archiveRaw,&archive);err!=nil{return report,fmt.Errorf("decode Session %s archive %d: %w",indexKey.ID,i,err)}
   if archive.Version!=1||archive.SessionID!=indexKey.ID{return report,fmt.Errorf("Session %s archive %d belongs to another format/session",indexKey.ID,i)}
   rows=append(rows,archive.Messages...)
  }
  rows=append(rows,snapshot.Messages...)
  if snapshot.ArchivedBatches==0&&len(snapshot.Messages)==1200{
   report.PossibleTruncation=append(report.PossibleTruncation,indexKey.ID)
  }
  messages:=make([]Message,0,len(rows))
  ids:=map[string]bool{}
  for i,raw:=range rows{
   compact,err:=canonical(raw);if err!=nil{return report,fmt.Errorf("Session %s message %d: %w",indexKey.ID,i,err)}
   var meta struct { ID string `json:"id"` }
   if err=json.Unmarshal(compact,&meta);err!=nil{return report,err}
   // Preserve original message JSON even when a pre-release message had
   // an empty or duplicate ID. Use synthetic DB keys without changing it.
   key:=meta.ID
   if key==""||ids[key]{key=fmt.Sprintf("legacy-row-%08d",i)}
   if ids[key]{return report,fmt.Errorf("unresolvable duplicate legacy message identifier at %d",i)}
   ids[key]=true
   messages=append(messages,Message{ID:key,Payload:compact})
  }
  var changes []byte
  if len(bytes.TrimSpace(snapshot.Changes))>0&&string(snapshot.Changes)!="null"{
   changes,err=canonical(snapshot.Changes);if err!=nil{return report,err}
  }
  var pending []byte
  if len(bytes.TrimSpace(snapshot.PendingRun))>0&&string(snapshot.PendingRun)!="null"{
   pending,err=canonical(snapshot.PendingRun);if err!=nil{return report,err}
  }
  sessions=append(sessions,importedSession{key:indexKey,metadata:snapshotMetadata,updatedAt:snapshotKey.UpdatedAt,messages:messages,changes:changes,pending:pending})
 }
 // Detect abandoned snapshots and archive files rather than hiding data.
 files,err:=filepath.Glob(filepath.Join(root,"session-*.json"))
 if err!=nil{return report,err}
 for _,path:=range files{
  base:=filepath.Base(path)
  if strings.Contains(base,"-history-"){continue}
  if !knownPaths[path]{return report,fmt.Errorf("orphan legacy snapshot %s; refusing to hide data",base)}
 }
 sort.Slice(sessions,func(i,j int)bool{return sessions[i].key.ID<sessions[j].key.ID})
 for _,session:=range sessions{
  if _,err=tx.ExecContext(ctx,"INSERT INTO sessions(id,directory,metadata,revision,changed_at) VALUES(?,?,?,1,?)",session.key.ID,session.key.Directory,session.metadata,session.updatedAt);err!=nil{return report,err}
  for i,message:=range session.messages{
   if _,err=tx.ExecContext(ctx,"INSERT INTO messages(session_id,message_id,sequence,payload) VALUES(?,?,?,?)",session.key.ID,message.ID,i+1,message.Payload);err!=nil{return report,err}
  }
  if len(session.changes)>0{
   if _,err=tx.ExecContext(ctx,"INSERT INTO session_changes(session_id,payload) VALUES(?,?)",session.key.ID,session.changes);err!=nil{return report,err}
  }
  if len(session.pending)>0{
   if _,err=tx.ExecContext(ctx,"INSERT INTO session_pending_run(session_id,payload) VALUES(?,?)",session.key.ID,session.pending);err!=nil{return report,err}
  }
  // Verify every imported row against the reader within this transaction,
  // before the migration marker can become visible.
  var count int
  if err=tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM messages WHERE session_id=?",session.key.ID).Scan(&count);err!=nil{return report,err}
  if count!=len(session.messages){return report,fmt.Errorf("verification failed for Session %s",session.key.ID)}
  report.Sessions++
  report.Messages+=count
 }
 report.SourceDigest=hex.EncodeToString(digest.Sum(nil))
 if _,err=tx.ExecContext(ctx,"INSERT INTO migration_state(name,source_digest) VALUES('legacy-v1',?)",report.SourceDigest);err!=nil{return report,err}
 if err=tx.Commit();err!=nil{return MigrationReport{},err}
 return report,nil
}
