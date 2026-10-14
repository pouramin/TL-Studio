// Package sessiondb owns transactional TL Studio Session state.
// It deliberately contains no knowledge of HTTP handlers or Agent providers.
// Every mutation is a single SQLite transaction; there are no JSON sidecar
// authority files, and uncommitted writes never change visible state.
package sessiondb

import (
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "strings"
 "time"

 _ "github.com/ncruces/go-sqlite3/driver"
)

var ErrConflict = errors.New("session revision conflict")
var ErrAlreadyCommitted = errors.New("operation already committed")

type Store struct { db *sql.DB }
type Session struct {
 ID string
 Directory string
 Metadata []byte
 Revision int64
}
type Message struct {
 ID string
 Payload []byte
}
type Mutation struct {
 OperationID string
 SessionID string
 Directory string
 Metadata []byte
 ExpectedRevision int64
 Messages []Message
 ChangeData []byte
 PendingRun []byte
 UpdatePendingRun bool
 Delete bool
}
type Commit struct {
 Revision int64
 PreviouslyCommitted bool
}

// Open fails closed for corrupt or unreadable existing stores. It never
// replaces a damaged database with an empty history.
func Open(path string) (*Store,error) {
 if strings.TrimSpace(path)=="" { return nil,errors.New("session database path is required") }
 if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{return nil,err}
 if st,err:=os.Lstat(path);err==nil {
  if !st.Mode().IsRegular() {return nil,fmt.Errorf("session database is not a regular file: %s",path)}
 } else if errors.Is(err,os.ErrNotExist){
  // Create private database bytes without a time-of-check/time-of-use
  // overwrite; concurrent launches can safely race on O_EXCL.
  file,createErr:=os.OpenFile(path,os.O_RDWR|os.O_CREATE|os.O_EXCL,0600)
  if createErr==nil{if closeErr:=file.Close();closeErr!=nil{return nil,closeErr}}
  if createErr!=nil&&!errors.Is(createErr,os.ErrExist){return nil,createErr}
 } else {return nil,err}
 // The immediate transaction mode avoids failed deferred read-to-write
 // upgrades under concurrent launchers.
 dsn:="file:"+filepath.ToSlash(path)+"?_txlock=immediate&_pragma=busy_timeout(10000)"
 db,err:=sql.Open("sqlite3",dsn);if err!=nil{return nil,err}
 db.SetMaxOpenConns(1)
 db.SetMaxIdleConns(1)
 cleanup:=func(e error)(*Store,error){_ = db.Close();return nil,e}
 for _,statement:=range []string{
  "PRAGMA busy_timeout=10000",
  "PRAGMA journal_mode=WAL",
  "PRAGMA synchronous=FULL",
  "PRAGMA foreign_keys=ON",
 } {
  if _,err=db.Exec(statement);err!=nil{return cleanup(fmt.Errorf("session database configuration %s: %w",statement,err))}
 }
 var integrity string
 if err=db.QueryRow("PRAGMA quick_check").Scan(&integrity);err!=nil{return cleanup(err)}
 if integrity!="ok"{return cleanup(fmt.Errorf("session database corrupt: %s",integrity))}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 tx,err:=db.BeginTx(ctx,nil);if err!=nil{return cleanup(err)}
 for _,statement:=range []string{
  `CREATE TABLE IF NOT EXISTS sessions (
     id TEXT PRIMARY KEY, directory TEXT NOT NULL, metadata BLOB NOT NULL,
     revision INTEGER NOT NULL DEFAULT 1, changed_at INTEGER NOT NULL)`,
  `CREATE TABLE IF NOT EXISTS messages (
     session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
     message_id TEXT NOT NULL, sequence INTEGER NOT NULL,
     payload BLOB NOT NULL, PRIMARY KEY(session_id,message_id),
     UNIQUE(session_id,sequence))`,
  `CREATE TABLE IF NOT EXISTS session_changes (
     session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
     payload BLOB NOT NULL)`,
  `CREATE TABLE IF NOT EXISTS session_pending_run (
     session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
     payload BLOB NOT NULL)`,
  `CREATE TABLE IF NOT EXISTS commits (
     operation_id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL,
     session_id TEXT NOT NULL, revision INTEGER NOT NULL)`,
  "CREATE INDEX IF NOT EXISTS sessions_recent ON sessions(changed_at DESC,id)",
 } {
  if _,err=tx.ExecContext(ctx,statement);err!=nil{_ = tx.Rollback();return cleanup(err)}
 }
 if err=tx.Commit();err!=nil{return cleanup(err)}
 return &Store{db:db},nil
}
func (s *Store) Close() error {return s.db.Close()}
func (s *Store) IntegrityCheck(ctx context.Context) error {
 var answer string
 if err:=s.db.QueryRowContext(ctx,"PRAGMA integrity_check").Scan(&answer);err!=nil{return err}
 if answer!="ok"{return fmt.Errorf("session database integrity check failed: %s",answer)}
 return nil
}
func fingerprint(m Mutation) string {
 h:=sha256.New()
 // Versioned domain separator makes idempotency fingerprints unambiguous.
 h.Write([]byte("tlstudio-session-mutation-v1\x00"))
 h.Write([]byte(m.SessionID));h.Write([]byte{0})
 h.Write([]byte(m.Directory));h.Write([]byte{0})
 h.Write(m.Metadata);h.Write([]byte{0})
 h.Write([]byte(fmt.Sprint(m.ExpectedRevision,m.Delete)))
 for _,message:=range m.Messages {h.Write([]byte{0});h.Write([]byte(message.ID));h.Write([]byte{0});h.Write(message.Payload)}
 h.Write([]byte{0});h.Write(m.ChangeData)
 h.Write([]byte{0});h.Write([]byte(fmt.Sprint(m.UpdatePendingRun)));h.Write([]byte{0});h.Write(m.PendingRun)
 return hex.EncodeToString(h.Sum(nil))
}

// Apply makes metadata, messages and changes visible together. A caller must
// retry unknown acknowledgments with the *same* operation ID and identical data.
func (s *Store) Apply(ctx context.Context,m Mutation)(Commit,error){
 if m.OperationID==""||m.SessionID=="" {return Commit{},errors.New("stable operation and session IDs required")}
 fp:=fingerprint(m)
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return Commit{},err}
 defer tx.Rollback()
 var committedFingerprint string
 var committedRevision int64
 err=tx.QueryRowContext(ctx,"SELECT fingerprint,revision FROM commits WHERE operation_id=?",m.OperationID).Scan(&committedFingerprint,&committedRevision)
 if err==nil {
  if committedFingerprint!=fp {return Commit{},fmt.Errorf("operation ID reused with different data")}
  return Commit{Revision:committedRevision,PreviouslyCommitted:true},nil
 }
 if !errors.Is(err,sql.ErrNoRows){return Commit{},err}
 var prior int64
 err=tx.QueryRowContext(ctx,"SELECT revision FROM sessions WHERE id=?",m.SessionID).Scan(&prior)
 if errors.Is(err,sql.ErrNoRows){prior=0}else if err!=nil{return Commit{},err}
 if m.ExpectedRevision!=prior {return Commit{},fmt.Errorf("%w: expected %d got %d",ErrConflict,m.ExpectedRevision,prior)}
 next:=prior+1
 if m.Delete {
  if _,err=tx.ExecContext(ctx,"DELETE FROM sessions WHERE id=?",m.SessionID);err!=nil{return Commit{},err}
 }else{
  if m.Directory==""||len(m.Metadata)==0{return Commit{},errors.New("directory and metadata required")}
  if _,err=tx.ExecContext(ctx,`INSERT INTO sessions(id,directory,metadata,revision,changed_at) VALUES(?,?,?,?,?)
    ON CONFLICT(id) DO UPDATE SET directory=excluded.directory,metadata=excluded.metadata,
      revision=excluded.revision,changed_at=excluded.changed_at`,
   m.SessionID,m.Directory,m.Metadata,next,time.Now().UnixMilli());err!=nil{return Commit{},err}
  for _,message:=range m.Messages {
   if message.ID=="" {return Commit{},errors.New("message ID required")}
   var sequence int64
   if err=tx.QueryRowContext(ctx,"SELECT COALESCE(MAX(sequence),0)+1 FROM messages WHERE session_id=?",m.SessionID).Scan(&sequence);err!=nil{return Commit{},err}
   // Repeated message IDs may update a running tool's journaled status;
   // never duplicate the message or change the immutable sequence.
   if _,err=tx.ExecContext(ctx,`INSERT INTO messages(session_id,message_id,sequence,payload)
       VALUES(?,?,?,?) ON CONFLICT(session_id,message_id) DO UPDATE SET payload=excluded.payload`,
     m.SessionID,message.ID,sequence,message.Payload);err!=nil{return Commit{},err}
  }
  if m.ChangeData!=nil {
   if _,err=tx.ExecContext(ctx,`INSERT INTO session_changes(session_id,payload) VALUES(?,?)
      ON CONFLICT(session_id) DO UPDATE SET payload=excluded.payload`,m.SessionID,m.ChangeData);err!=nil{return Commit{},err}
  }
  if m.UpdatePendingRun {
   if len(m.PendingRun)==0 {
    if _,err=tx.ExecContext(ctx,"DELETE FROM session_pending_run WHERE session_id=?",m.SessionID);err!=nil{return Commit{},err}
   } else if _,err=tx.ExecContext(ctx,`INSERT INTO session_pending_run(session_id,payload) VALUES(?,?)
     ON CONFLICT(session_id) DO UPDATE SET payload=excluded.payload`,m.SessionID,m.PendingRun);err!=nil{return Commit{},err}
  }
 }
 if _,err=tx.ExecContext(ctx,"INSERT INTO commits(operation_id,fingerprint,session_id,revision) VALUES(?,?,?,?)",m.OperationID,fp,m.SessionID,next);err!=nil{return Commit{},err}
 if err=tx.Commit();err!=nil{return Commit{},err}
 return Commit{Revision:next},nil
}
func (s *Store) Get(ctx context.Context,id string)(Session,bool,error){
 var x Session
 x.ID=id
 err:=s.db.QueryRowContext(ctx,"SELECT directory,metadata,revision FROM sessions WHERE id=?",id).Scan(&x.Directory,&x.Metadata,&x.Revision)
 if errors.Is(err,sql.ErrNoRows){return Session{},false,nil}
 return x,err==nil,err
}
func (s *Store) List(ctx context.Context,limit int)([]Session,error){
 if limit<=0 {limit=1000000}
 rows,err:=s.db.QueryContext(ctx,"SELECT id,directory,metadata,revision FROM sessions ORDER BY changed_at DESC,id LIMIT ?",limit)
 if err!=nil{return nil,err}
 defer rows.Close()
 result:=[]Session{}
 for rows.Next(){var x Session;if err=rows.Scan(&x.ID,&x.Directory,&x.Metadata,&x.Revision);err!=nil{return nil,err};result=append(result,x)}
 return result,rows.Err()
}
func (s *Store) Messages(ctx context.Context,id string)([]Message,error){
 rows,err:=s.db.QueryContext(ctx,"SELECT message_id,payload FROM messages WHERE session_id=? ORDER BY sequence",id)
 if err!=nil{return nil,err}
 defer rows.Close()
 result:=[]Message{}
 for rows.Next(){var x Message;if err=rows.Scan(&x.ID,&x.Payload);err!=nil{return nil,err};result=append(result,x)}
 return result,rows.Err()
}
func (s *Store) Changes(ctx context.Context,id string)([]byte,error){
 var value []byte
 err:=s.db.QueryRowContext(ctx,"SELECT payload FROM session_changes WHERE session_id=?",id).Scan(&value)
 if errors.Is(err,sql.ErrNoRows){return nil,nil}
 return value,err
}

func (s *Store) PendingRun(ctx context.Context,id string)([]byte,error){
 var value []byte
 err:=s.db.QueryRowContext(ctx,"SELECT payload FROM session_pending_run WHERE session_id=?",id).Scan(&value)
 if errors.Is(err,sql.ErrNoRows){return nil,nil}
 return value,err
}

// ReadSnapshot reads metadata, messages, changes and pending-run state in one
// read transaction. Never interleave unrelated committed generations.
func (s *Store) ReadSnapshot(ctx context.Context,id string)(Session,[]Message,[]byte,[]byte,bool,error){
 tx,err:=s.db.BeginTx(ctx,&sql.TxOptions{ReadOnly:true})
 if err!=nil{return Session{},nil,nil,nil,false,err}
 defer tx.Rollback()
 var session Session
 session.ID=id
 err=tx.QueryRowContext(ctx,"SELECT directory,metadata,revision FROM sessions WHERE id=?",id).Scan(&session.Directory,&session.Metadata,&session.Revision)
 if errors.Is(err,sql.ErrNoRows){return Session{},nil,nil,nil,false,nil}
 if err!=nil{return Session{},nil,nil,nil,false,err}
 rows,err:=tx.QueryContext(ctx,"SELECT message_id,payload FROM messages WHERE session_id=? ORDER BY sequence",id)
 if err!=nil{return Session{},nil,nil,nil,false,err}
 messages:=[]Message{}
 for rows.Next(){var msg Message;if err=rows.Scan(&msg.ID,&msg.Payload);err!=nil{rows.Close();return Session{},nil,nil,nil,false,err};messages=append(messages,msg)}
 if err=rows.Err();err!=nil{rows.Close();return Session{},nil,nil,nil,false,err}
 rows.Close()
 var changes,pending []byte
 if err=tx.QueryRowContext(ctx,"SELECT payload FROM session_changes WHERE session_id=?",id).Scan(&changes);err!=nil&&!errors.Is(err,sql.ErrNoRows){return Session{},nil,nil,nil,false,err}
 if err=tx.QueryRowContext(ctx,"SELECT payload FROM session_pending_run WHERE session_id=?",id).Scan(&pending);err!=nil&&!errors.Is(err,sql.ErrNoRows){return Session{},nil,nil,nil,false,err}
 if err=tx.Commit();err!=nil{return Session{},nil,nil,nil,false,err}
 return session,messages,changes,pending,true,nil
}
