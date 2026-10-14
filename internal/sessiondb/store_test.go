package sessiondb

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "sync"
 "testing"
)

func openTestStore(t *testing.T)(*Store,string){
 t.Helper()
 path:=filepath.Join(t.TempDir(),"sessions.db")
 db,err:=Open(path);if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){_ = db.Close()})
 return db,path
}
func TestTransactionDoesNotExposePartiallyFailedMutation(t *testing.T){
 db,_:=openTestStore(t);ctx:=context.Background()
 first:=Mutation{OperationID:"one",SessionID:"s1",Directory:"/project",Metadata:[]byte("v1"),Messages:[]Message{{ID:"m1",Payload:[]byte("hello")}}}
 if _,err:=db.Apply(ctx,first);err!=nil{t.Fatal(err)}
 second:=Mutation{OperationID:"two",SessionID:"s1",Directory:"/project",ExpectedRevision:1,Metadata:[]byte("v2"),Messages:[]Message{{ID:"m2",Payload:[]byte("new")},{ID:"",Payload:[]byte("bad")}}}
 if _,err:=db.Apply(ctx,second);err==nil{t.Fatal("expected mutation error")}
 live,ok,err:=db.Get(ctx,"s1")
 if err!=nil||!ok||string(live.Metadata)!="v1"||live.Revision!=1{t.Fatalf("partially committed metadata: %+v %v",live,err)}
 ms,err:=db.Messages(ctx,"s1")
 if err!=nil||len(ms)!=1||ms[0].ID!="m1"{t.Fatalf("partially committed messages: %+v %v",ms,err)}
 if err=db.IntegrityCheck(ctx);err!=nil{t.Fatal(err)}
}
func TestIdempotencyAndStaleRevision(t *testing.T){
 db,_:=openTestStore(t);ctx:=context.Background()
 m:=Mutation{OperationID:"same-1",SessionID:"s",Directory:"/project",Metadata:[]byte("{}"),Messages:[]Message{{ID:"m",Payload:[]byte("{}")}}}
 r,err:=db.Apply(ctx,m);if err!=nil||r.Revision!=1||r.PreviouslyCommitted{t.Fatalf("first commit: %+v %v",r,err)}
 r,err=db.Apply(ctx,m);if err!=nil||r.Revision!=1||!r.PreviouslyCommitted{t.Fatalf("idempotent retry: %+v %v",r,err)}
 bad:=m;bad.Metadata=[]byte("different")
 if _,err=db.Apply(ctx,bad);err==nil{t.Fatal("operation ID replay with different bytes accepted")}
 stale:=m;stale.OperationID="new-op"
 if _,err=db.Apply(ctx,stale);!errors.Is(err,ErrConflict){t.Fatalf("stale revision not rejected: %v",err)}
}
func TestConcurrentIndependentProcessStoresDoNotLoseSessions(t *testing.T){
 db,path:=openTestStore(t)
 other,err:=Open(path);if err!=nil{t.Fatal(err)};defer other.Close()
 ctx:=context.Background()
 var wg sync.WaitGroup
 errorsCh:=make(chan error,80)
 for i:=0;i<80;i++{
  wg.Add(1)
  go func(i int){defer wg.Done()
   instance:=db;if i%2!=0{instance=other}
   key:=string(rune(0x500+i))
   _,err:=instance.Apply(ctx,Mutation{OperationID:"op-"+key,SessionID:"sess-"+key,Directory:"/project",Metadata:[]byte("{}")})
   if err!=nil{errorsCh<-err}
  }(i)
 }
 wg.Wait();close(errorsCh)
 for err:=range errorsCh{t.Errorf("concurrent write: %v",err)}
 sessions,err:=db.List(ctx,100)
 if err!=nil||len(sessions)!=80{t.Fatalf("lost a Session in cross-process contention: got %d, err %v",len(sessions),err)}
}
func TestRestartAndCorruptDatabaseFailClosed(t *testing.T){
 db,path:=openTestStore(t);ctx:=context.Background()
 _,err:=db.Apply(ctx,Mutation{OperationID:"op",SessionID:"s1",Directory:"/project",Metadata:[]byte("v1")})
 if err!=nil{t.Fatal(err)}
 if err=db.Close();err!=nil{t.Fatal(err)}
 reopened,err:=Open(path);if err!=nil{t.Fatal(err)}
 row,ok,err:=reopened.Get(ctx,"s1");if err!=nil||!ok||string(row.Metadata)!="v1"{t.Fatalf("lost committed state after restart: %+v %v",row,err)}
 if err=reopened.Close();err!=nil{t.Fatal(err)}
 if err=os.WriteFile(path,[]byte("not a sqlite database"),0600);err!=nil{t.Fatal(err)}
 if opened,err:=Open(path);err==nil{opened.Close();t.Fatal("corrupt database must not silently open empty")}
 raw,err:=os.ReadFile(path);if err!=nil||string(raw)!="not a sqlite database"{t.Fatal("corruption bytes changed after failed opening")}
}
func TestMessageRevisionReplacementRetainsSequence(t *testing.T){
 db,_:=openTestStore(t);ctx:=context.Background()
 steps:=[]Mutation{
  {OperationID:"1",SessionID:"s",Directory:"/project",Metadata:[]byte("{}"),Messages:[]Message{{ID:"first",Payload:[]byte("running")},{ID:"second",Payload:[]byte("hi")}}},
  {OperationID:"2",SessionID:"s",Directory:"/project",Metadata:[]byte("{}"),ExpectedRevision:1,Messages:[]Message{{ID:"first",Payload:[]byte("complete")}}},
 }
 for _,step:=range steps{if _,err:=db.Apply(ctx,step);err!=nil{t.Fatal(err)}}
 messages,err:=db.Messages(ctx,"s")
 if err!=nil||len(messages)!=2||messages[0].ID!="first"||string(messages[0].Payload)!="complete"||messages[1].ID!="second"{t.Fatalf("journal sequence lost: %+v %v",messages,err)}
}
