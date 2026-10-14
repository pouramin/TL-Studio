package main

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "os"
 "path/filepath"
 "strings"
 "time"

 "github.com/pouramin/TL-Studio/internal/sessiondb"
)

// SQLite is opt-in until the complete release acceptance suite is met.
// A failed import is a hard failure; it never starts a fresh empty history.
func (s *sessionPersistenceStore) sqliteLoadLocked() error {
 ctx,cancel:=context.WithTimeout(context.Background(),60*time.Second)
 defer cancel()
 justOpened:=s.sqlite==nil
 if justOpened{
  db,err:=sessiondb.Open(filepath.Join(s.root,"sessions.sqlite"))
  if err!=nil{return fmt.Errorf("open SQLite session authority: %w",err)}
  if _,err=db.ImportLegacy(ctx,s.root);err!=nil{
   db.Close()
   return fmt.Errorf("import legacy Session history: %w",err)
  }
  s.sqlite=db
 }
 rows,err:=s.sqlite.List(ctx,0)
 if err!=nil{return err}
 next:=make(map[string]sessionView,len(rows))
 for _,row:=range rows {
  var session sessionView
  if err=json.Unmarshal(row.Metadata,&session);err!=nil{return fmt.Errorf("decode persisted Session %q: %w",row.ID,err)}
  if session.ID!=row.ID||session.Directory!=row.Directory{return fmt.Errorf("SQLite Session %q integrity mismatch",row.ID)}
  next[row.ID]=session
 }
 s.sessions=next
 s.loaded=true
 if justOpened{return s.sqliteRecoverInterruptedLocked()}
 return nil
}

func (s *sessionPersistenceStore) sqliteSnapshotLocked(id string)(persistedSessionSnapshot,error){
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
 defer cancel()
 row,msgs,changes,pending,found,err:=s.sqlite.ReadSnapshot(ctx,strings.TrimSpace(id))
 if err!=nil{return persistedSessionSnapshot{},err}
 if !found{return persistedSessionSnapshot{},os.ErrNotExist}
 var snapshot persistedSessionSnapshot
 snapshot.Version=sessionPersistenceVersion
 snapshot.DBRevision=row.Revision
 if err=json.Unmarshal(row.Metadata,&snapshot.Session);err!=nil{return persistedSessionSnapshot{},err}
 for _,msg:=range msgs {
  var view sessionMessageView
  if err=json.Unmarshal(msg.Payload,&view);err!=nil{return persistedSessionSnapshot{},fmt.Errorf("decode Session %s message %s: %w",id,msg.ID,err)}
  snapshot.Messages=append(snapshot.Messages,view)
 }
 if len(changes)>0 {if err=json.Unmarshal(changes,&snapshot.Changes);err!=nil{return persistedSessionSnapshot{},err}}
 if len(pending)>0 {if err=json.Unmarshal(pending,&snapshot.PendingRun);err!=nil{return persistedSessionSnapshot{},err}}
 return snapshot,nil
}

// Called with the Session mutex held. The database revision guards concurrent
// writers in different TL Studio processes, not merely Go goroutines.
func (s *sessionPersistenceStore) sqliteCommitSnapshotLocked(session sessionView,snapshot persistedSessionSnapshot)error{
 ctx,cancel:=context.WithTimeout(context.Background(),60*time.Second)
 defer cancel()
 // The revision is the one observed when the caller read the snapshot.
 // SQLite Apply checks it atomically inside BEGIN IMMEDIATE. Reading the
 // current revision here would turn optimistic concurrency into last-writer-
 // wins and silently lose edits from another launcher.
 revision:=snapshot.DBRevision
 metadata,err:=json.Marshal(session);if err!=nil{return err}
 messages:=make([]sessiondb.Message,0,len(snapshot.Messages))
 for i,message:=range snapshot.Messages{
  payload,encodeErr:=json.Marshal(message);if encodeErr!=nil{return encodeErr}
  id:=strings.TrimSpace(message.ID)
  if id=="" {
   // An ID-less legacy message must not be dropped or collapsed.
   sum:=sha256.Sum256(append([]byte(fmt.Sprint(i)),payload...))
   id="anonymous-"+hex.EncodeToString(sum[:16])
  }
  messages=append(messages,sessiondb.Message{ID:id,Payload:payload})
 }
 changes,err:=json.Marshal(snapshot.Changes)
 if err!=nil{return err}
 var pending []byte
 if snapshot.PendingRun!=nil{
  pending,err=json.Marshal(snapshot.PendingRun)
  if err!=nil{return err}
 }
 // A stable content-derived operation key makes retrying the same snapshot
 // after an ambiguous acknowledgment idempotent.
 hash:=sha256.New()
 hash.Write([]byte(session.ID))
 hash.Write([]byte(fmt.Sprint(revision)))
 hash.Write(metadata)
 for _,message:=range messages{hash.Write([]byte{0});hash.Write([]byte(message.ID));hash.Write(message.Payload)}
 hash.Write(changes);hash.Write(pending)
 opID:="session-state-"+hex.EncodeToString(hash.Sum(nil))
 _,err=s.sqlite.Apply(ctx,sessiondb.Mutation{
  OperationID:opID, SessionID:session.ID, Directory:session.Directory,
  Metadata:metadata, ExpectedRevision:revision,
  Messages:messages,ReplaceMessages:true,ChangeData:changes,
  PendingRun:pending,UpdatePendingRun:true,
 })
 if err!=nil{return err}
 // Pull committed metadata back from the DB instead of keeping a second
 // writable index with a different durable lifetime.
 return s.sqliteLoadLocked()
}
func (s *sessionPersistenceStore) sqliteRemoveLocked(id string)error{
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
 defer cancel()
 prior,exists,err:=s.sqlite.Get(ctx,id)
 if err!=nil{return err}
 if !exists{return nil}
 opID:="session-delete-"+id+"-"+fmt.Sprint(prior.Revision)
 if _,err=s.sqlite.Apply(ctx,sessiondb.Mutation{
  OperationID:opID,SessionID:id,ExpectedRevision:prior.Revision,Delete:true,
 });err!=nil{return err}
 return s.sqliteLoadLocked()
}

// SQLite recovery is applied only to committed pending-run records. The
// external tool's result is unknown and must never be replayed automatically.
func (s *sessionPersistenceStore) sqliteRecoverInterruptedLocked()error{
 if s.sqlite==nil{return nil}
 ids:=make([]string,0,len(s.sessions))
 for id:=range s.sessions {ids=append(ids,id)}
 for _,id:=range ids {
  snapshot,err:=s.sqliteSnapshotLocked(id)
  if err!=nil{return err}
  if snapshot.PendingRun==nil{continue}
  started:=snapshot.PendingRun.StartedAt
  snapshot.PendingRun=nil
  for i:=range snapshot.Messages{
   for j:=range snapshot.Messages[i].Activities{
    act:= &snapshot.Messages[i].Activities[j]
    if act.Kind=="tool"&&act.Status=="running"{
     act.Status="failed"
     act.Error=&sessionErrorView{Type:"interrupted",Message:"TL Studio stopped before the tool outcome was recorded. The operation may have completed; inspect the workspace before retrying."}
    }
   }
  }
  recoveryID:=fmt.Sprintf("tlsm_recovered_%d",started)
  already:=false
  for _,message:=range snapshot.Messages{if message.ID==recoveryID{already=true;break}}
  if !already{snapshot.Messages=append(snapshot.Messages,interruptedNativeRunMessage(id,started))}
  if err=s.sqliteCommitSnapshotLocked(snapshot.Session,snapshot);err!=nil{return err}
 }
 return nil
}

// SQLite remains an opt-in acceptance path until manual provider and
// destructive-crash validation have passed.
