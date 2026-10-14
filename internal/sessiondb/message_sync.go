package sessiondb

import (
 "bytes"
 "context"
 "database/sql"
 "fmt"
)

type persistedMessage struct {
 sequence int64
 payload []byte
}

// replaceMessagesTx treats the supplied ordered message list as the complete
// authoritative state. The common append/content-update path writes O(delta)
// rows; explicit deletion or reordering takes the slower safe reindex path.
func replaceMessagesTx(ctx context.Context,tx *sql.Tx,sessionID string,desired []Message)error{
 rows,err:=tx.QueryContext(ctx,"SELECT message_id,sequence,payload FROM messages WHERE session_id=? ORDER BY sequence",sessionID)
 if err!=nil{return err}
 existing:=make(map[string]persistedMessage)
 oldOrder:=[]string{}
 for rows.Next(){
  var id string
  var message persistedMessage
  if err=rows.Scan(&id,&message.sequence,&message.payload);err!=nil{rows.Close();return err}
  existing[id]=message
  oldOrder=append(oldOrder,id)
 }
 if err=rows.Err();err!=nil{rows.Close();return err}
 rows.Close()
 seen:=make(map[string]bool,len(desired))
 prefixUnchanged:=true
 for i,message:=range desired{
  if message.ID=="" {return fmt.Errorf("message at position %d has no stable ID",i)}
  if seen[message.ID]{return fmt.Errorf("duplicate message ID %q",message.ID)}
  seen[message.ID]=true
  if i<len(oldOrder)&&message.ID!=oldOrder[i]{prefixUnchanged=false}
 }
 if len(desired)<len(oldOrder){prefixUnchanged=false}
 if prefixUnchanged{
  // Existing sequence prefix is stable: update only changed payloads and
  // append missing rows, never rewrite the entire history on every turn.
  for i,message:=range desired{
   old,found:=existing[message.ID]
   if found {
    if old.sequence!=int64(i+1){return fmt.Errorf("noncontiguous message sequence for %q",message.ID)}
    if bytes.Equal(old.payload,message.Payload){continue}
    if _,err=tx.ExecContext(ctx,"UPDATE messages SET payload=? WHERE session_id=? AND message_id=?",message.Payload,sessionID,message.ID);err!=nil{return err}
    continue
   }
   if _,err=tx.ExecContext(ctx,"INSERT INTO messages(session_id,message_id,sequence,payload) VALUES(?,?,?,?)",sessionID,message.ID,i+1,message.Payload);err!=nil{return err}
  }
  return nil
 }
 // Reorder/removal is uncommon but must not leave rejected old messages
 // accessible. Within this transaction, move existing sequence slots out
 // of the positive range first to avoid SQLite UNIQUE collisions.
 for id:=range existing{
  if !seen[id]{
   if _,err=tx.ExecContext(ctx,"DELETE FROM messages WHERE session_id=? AND message_id=?",sessionID,id);err!=nil{return err}
  }
 }
 if _,err=tx.ExecContext(ctx,"UPDATE messages SET sequence=-sequence WHERE session_id=?",sessionID);err!=nil{return err}
 for i,message:=range desired{
  if _,err=tx.ExecContext(ctx,`INSERT INTO messages(session_id,message_id,sequence,payload) VALUES(?,?,?,?)
    ON CONFLICT(session_id,message_id) DO UPDATE
    SET sequence=excluded.sequence,payload=excluded.payload`,sessionID,message.ID,i+1,message.Payload);err!=nil{return err}
 }
 return nil
}
