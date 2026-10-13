package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type foundationBudgetModel struct {
	mu sync.Mutex
	calls int
}
func (m *foundationBudgetModel) Complete(_ context.Context, _ nativeModelRequest, _ func(string)) (nativeModelResponse,error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	filename := fmt.Sprintf("file-%02d.txt",m.calls)
	return nativeModelResponse{ToolCalls:[]nativeModelToolCall{{
		ID: fmt.Sprintf("call-%02d",m.calls), Name:"files.read",
		Arguments:json.RawMessage(fmt.Sprintf(`{"path":%q}`,filename)),
	}},FinishReason:"tool_calls"},nil
}

func TestFoundationFeedbackToolRoundBudgetIsExplicitCheckpoint(t *testing.T) {
	project:=t.TempDir()
	for i:=1;i<=nativeAgentMaxToolRounds+1;i++ {
		p:=filepath.Join(project,fmt.Sprintf("file-%02d.txt",i))
		if err:=os.WriteFile(p,[]byte("test"),0600);err!=nil{t.Fatal(err)}
	}
	root:=filepath.Join(t.TempDir(),"sessions")
	store:=newSessionPersistenceStore(root,"budget-feedback")
	session,err:=store.createNativeSession(project,sessionCreateInput{Title:"budget"})
	if err!=nil{t.Fatal(err)}
	model:=&foundationBudgetModel{}
	executor:=newNativeToolExecutor(newProcessManager(func()string{return project}),nativeAllowAuthorizer{})
	runtime:=newNativeAgentRuntime(
		nativeTestResolver{provider:tlProviderDefinition{ID:"test",Protocol:"openai-compatible",BaseURL:"http://127.0.0.1:1/v1"},model:tlProviderModel{ID:"test",ToolCall:true}},
		model,executor,store,newLiveEventBus())
	if err:=runtime.Start(project,session.ID,sessionRunInput{
		Text:"Read and inspect all of the numbered test files. Do not modify files.",
		Agent:"code",Model:&sessionModelRef{ProviderID:"test",ID:"test"},
	});err!=nil{t.Fatal(err)}
	deadline:=time.Now().Add(10*time.Second)
	for time.Now().Before(deadline){
		if _,ok:=runtime.NativeStatuses(project)[session.ID];!ok{break}
		time.Sleep(15*time.Millisecond)
	}
	if _,ok:=runtime.NativeStatuses(project)[session.ID];ok{t.Fatal("agent did not pause at budget")}
	messages,ok,err:=store.getMessages(session.ID)
	if !ok||err!=nil{t.Fatalf("history missing: %v",err)}
	rounds:=0
	checkpoint:=false
	for _,message:=range messages{
		if message.Role=="assistant"&&strings.Contains(message.Text,"not verified complete")&&strings.Contains(message.Text,"paused"){
			checkpoint=true
		}
		for _,act:=range message.Activities{
			if act.ToolID=="files.read"&&act.Status=="completed"{rounds++}
		}
	}
	if !checkpoint{t.Fatal("round budget must produce an explicit incomplete-work checkpoint")}
	if rounds!=nativeAgentMaxToolRounds{t.Fatalf("wanted exactly %d completed reads, got %d",nativeAgentMaxToolRounds,rounds)}
}
