package sessiondb

import (
 "bufio"
 "context"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

// A real child process is killed between asynchronous SQLite commits.
// Unlike a panic-based probe, this also discards in-process caches.
func TestSQLiteCrashRecoveryRealChildProcess(t *testing.T){
 if root:=os.Getenv("TL_STUDIO_SESSION_CRASH_CHILD");root!=""{
  db,err:=Open(filepath.Join(root,"sessions.sqlite"));if err!=nil{t.Fatal(err)}
  defer db.Close()
  ctx:=context.Background()
  for i:=0;i<10000;i++{
   _,err:=db.Apply(ctx,Mutation{OperationID:fmt.Sprintf("op-%d",i),
    SessionID:"live",Directory:"/project",ExpectedRevision:int64(i),
    Metadata:[]byte(fmt.Sprintf(`{"id":"live","counter":%d}`,i+1)),
    Messages:[]Message{{ID:fmt.Sprintf("m-%d",i),Payload:[]byte(fmt.Sprintf(`{"counter":%d}`,i+1))}},
   })
   if err!=nil{t.Fatal(err)}
   fmt.Printf("COMMITTED %d\n",i+1)
  }
  return
 }
 root:=t.TempDir()
 cmd:=exec.Command(os.Args[0],"-test.run=^TestSQLiteCrashRecoveryRealChildProcess$")
 cmd.Env=append(os.Environ(),"TL_STUDIO_SESSION_CRASH_CHILD="+root)
 pipe,err:=cmd.StdoutPipe();if err!=nil{t.Fatal(err)}
 if err=cmd.Start();err!=nil{t.Fatal(err)}
 done:=make(chan error,1)
 go func(){
  reader:=bufio.NewScanner(pipe)
  commits:=0
  for reader.Scan(){
   if strings.HasPrefix(reader.Text(),"COMMITTED ") {commits++;if commits==12{done<-nil;return}}
  }
  done<-fmt.Errorf("child exited before 12 durable commits")
 }()
 select{
 case err=<-done:if err!=nil{_ = cmd.Process.Kill();_ = cmd.Wait();t.Fatal(err)}
 case <-time.After(30*time.Second):_ = cmd.Process.Kill();_ = cmd.Wait();t.Fatal("child commits timed out")
 }
 if err=cmd.Process.Kill();err!=nil{t.Fatal(err)}
 _=cmd.Wait()
 db,err:=Open(filepath.Join(root,"sessions.sqlite"));if err!=nil{t.Fatal(err)}
 defer db.Close()
 ctx:=context.Background()
 if err=db.IntegrityCheck(ctx);err!=nil{t.Fatal(err)}
 session,ok,err:=db.Get(ctx,"live")
 if err!=nil||!ok{t.Fatalf("committed session missing: %v",err)}
 messages,err:=db.Messages(ctx,"live")
 if err!=nil{t.Fatal(err)}
 if len(messages)<12||int64(len(messages))!=session.Revision{
  t.Fatalf("partial transaction after process kill: revision %d messages %d",session.Revision,len(messages))
 }
 for i,msg:=range messages{
  if msg.ID!=fmt.Sprintf("m-%d",i){
   t.Fatalf("history contains a missing or reordered committed row at %d: %+v",i,msg)
  }
 }
}
