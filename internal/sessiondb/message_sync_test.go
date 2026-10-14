package sessiondb

import (
 "context"
 "fmt"
 "testing"
)

func TestAuthoritativeSessionSnapshotRemovesAndReordersMessages(t *testing.T){
 store,_:=openTestStore(t)
 ctx:=context.Background()
 apply:=func(op string,revision int64,list ...Message){
  t.Helper()
  _,err:=store.Apply(ctx,Mutation{
   OperationID:op,SessionID:"s1",Directory:"/project",
   Metadata:[]byte("{}"),ExpectedRevision:revision,
   ReplaceMessages:true,Messages:list,
  })
  if err!=nil{t.Fatal(err)}
 }
 mk:=func(id,text string)Message{return Message{ID:id,Payload:[]byte(text)}}
 apply("a",0,mk("a","old a"),mk("b","old b"),mk("c","old c"))
 // An agent-side rewrite that removes b, reorders c and changes a must
 // not leave b retrievable from the new committed generation.
 apply("b",1,mk("c","new c"),mk("a","new a"))
 rows,err:=store.Messages(ctx,"s1");if err!=nil{t.Fatal(err)}
 if len(rows)!=2||rows[0].ID!="c"||rows[1].ID!="a"||string(rows[1].Payload)!="new a"{
  t.Fatalf("rejected or reordered messages leaked: %+v",rows)
 }
 // A later append must preserve the new order without reviving b.
 apply("c",2,mk("c","new c"),mk("a","new a"),mk("d","new d"))
 rows,err=store.Messages(ctx,"s1")
 if err!=nil||len(rows)!=3||rows[2].ID!="d"{t.Fatalf("append after reorder incorrect: %+v %v",rows,err)}
}

func TestAuthoritativeSessionHistoryScalesWithoutTruncating(t *testing.T){
 store,_:=openTestStore(t)
 ctx:=context.Background()
 list:=make([]Message,0,1205)
 for i:=0;i<1205;i++{list=append(list,Message{ID:fmt.Sprintf("message-%04d",i),Payload:[]byte(fmt.Sprintf("history-%04d",i))})}
 _,err:=store.Apply(ctx,Mutation{
  OperationID:"long-history",SessionID:"long",Directory:"/project",Metadata:[]byte("{}"),
  ReplaceMessages:true,Messages:list,
 })
 if err!=nil{t.Fatal(err)}
 list[1204]=Message{ID:"message-1204",Payload:[]byte("updated last")}
 _,err=store.Apply(ctx,Mutation{
  OperationID:"long-history-next",SessionID:"long",Directory:"/project",Metadata:[]byte("{}"),
  ExpectedRevision:1,ReplaceMessages:true,Messages:list,
 })
 if err!=nil{t.Fatal(err)}
 result,err:=store.Messages(ctx,"long")
 if err!=nil||len(result)!=1205||result[0].ID!="message-0000"||string(result[1204].Payload)!="updated last"{
  t.Fatalf("long-history rewrite lost old data: count=%d err=%v",len(result),err)
 }
}
