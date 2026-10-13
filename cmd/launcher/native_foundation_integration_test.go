package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type foundationReadEditModel struct {
	mu sync.Mutex
	calls int
	finished chan struct{}
	t *testing.T
}

func (m *foundationReadEditModel) Complete(_ context.Context, request nativeModelRequest, _ func(string)) (nativeModelResponse,error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	switch m.calls {
	case 1:
		seenRead,seenEdit:=false,false
		for _,tool:=range request.Tools {
			if tool.ID=="files.read" {seenRead=true}
			if tool.ID=="files.edit" {seenEdit=true}
		}
		if !seenRead || !seenEdit {
			m.t.Errorf("explicit edit request lost read/edit tools: read=%v edit=%v",seenRead,seenEdit)
		}
		return nativeModelResponse{ToolCalls:[]nativeModelToolCall{{
			ID:"foundation-read",Name:"files.read",Arguments:json.RawMessage(`{"path":"note.txt"}`),
		}},FinishReason:"tool_calls"},nil
	case 2:
		var revision string
		for _,msg:=range request.Messages{
			if msg.Role!="tool" || msg.ToolCallID!="foundation-read"{continue}
			var payload struct{
				OK bool `json:"ok"`
				Output struct{SHA256 string `json:"sha256"`} `json:"output"`
			}
			if err:=json.Unmarshal([]byte(msg.Text),&payload);err!=nil{m.t.Errorf("read result invalid: %v",err)}
			if !payload.OK {m.t.Error("read tool reported failure")}
			revision=payload.Output.SHA256
		}
		if len(revision)!=64{
			m.t.Errorf("tool result lost revision: %q",revision)
			return nativeModelResponse{Text:"cannot edit without read revision",FinishReason:"stop"},nil
		}
		args,_:=json.Marshal(map[string]any{"path":"note.txt","old":"hello","new":"world","expectedSha256":revision})
		return nativeModelResponse{ToolCalls:[]nativeModelToolCall{{
			ID:"foundation-edit",Name:"files.edit",Arguments:args,
		}},FinishReason:"tool_calls"},nil
	case 3:
		seenResult:=false
		for _,msg:=range request.Messages{
			if msg.Role=="tool" && msg.ToolCallID=="foundation-edit" {
				if !strings.Contains(msg.Text,`"ok":true`) {
					m.t.Errorf("edit tool failed unexpectedly: %s",msg.Text)
				}
				seenResult=true
			}
		}
		if !seenResult {m.t.Error("edit result not returned to model")}
		close(m.finished)
		return nativeModelResponse{Text:"Edit completed and checked.",FinishReason:"stop"},nil
	default:
		return nativeModelResponse{},fmt.Errorf("unexpected call %d",m.calls)
	}
}

func TestFoundationReadEditRevisionAndDurableToolJournalEndToEnd(t *testing.T) {
	project:=t.TempDir()
	path:=filepath.Join(project,"note.txt")
	if err:=os.WriteFile(path,[]byte("hello"),0o600);err!=nil{t.Fatal(err)}
	root:=filepath.Join(t.TempDir(),"sessions")
	store:=newSessionPersistenceStore(root,"foundation-test")
	session,err:=store.createNativeSession(project,sessionCreateInput{Title:"foundation integration"})
	if err!=nil{t.Fatal(err)}
	model:=&foundationReadEditModel{t:t,finished:make(chan struct{})}
	executor:=newNativeToolExecutor(newProcessManager(func()string{return project}),nativeAllowAuthorizer{})
	runtime:=newNativeAgentRuntime(
		nativeTestResolver{
			provider:tlProviderDefinition{ID:"test",Protocol:"openai-compatible",BaseURL:"http://127.0.0.1:1/v1"},
			model:tlProviderModel{ID:"test-model",ToolCall:true},
		},
		model,executor,store,newLiveEventBus(),
	)
	if err:=runtime.Start(project,session.ID,sessionRunInput{
		Text:"Edit note.txt: replace hello with world.",Agent:"code",
		Model:&sessionModelRef{ProviderID:"test",ID:"test-model"},
	});err!=nil{t.Fatal(err)}
	select{
	case <-model.finished:
	case <-time.After(5*time.Second):t.Fatal("native read/edit loop did not finish")
	}
	deadline:=time.Now().Add(5*time.Second)
	for len(runtime.NativeStatuses(project))!=0 {
		if time.Now().After(deadline){t.Fatal("agent failed to finalize")}
		time.Sleep(10*time.Millisecond)
	}
	actual,err:=os.ReadFile(path)
	if err!=nil || string(actual)!="world"{t.Fatalf("revision-checked edit missing: %q %v",actual,err)}
	restored:=newSessionPersistenceStore(root,"foundation-test")
	history,ok,err:=restored.getMessages(session.ID)
	if err!=nil||!ok{t.Fatalf("session not recoverable: %v %v",ok,err)}
	if len(history)!=4 {t.Fatalf("expected accepted prompt + two tool turns + final response; got %d",len(history))}
	observed:=[]string{}
	for _,msg:=range history{
		for _,activity:=range msg.Activities{
			if activity.Kind=="tool" {
				if activity.Status!="completed" || activity.Error!=nil{
					t.Fatalf("tool outcome not durable: %#v",activity)
				}
				observed=append(observed,activity.ToolID)
			}
		}
	}
	if len(observed)!=2 || observed[0]!="files.read" || observed[1]!="files.edit"{
		t.Fatalf("tool execution history changed: %v",observed)
	}
	for _,msg:=range history{
		if msg.Error!=nil && msg.Error.Type=="interrupted" {
			t.Fatalf("normal run incorrectly marked interrupted: %#v",msg)
		}
	}
	if _,statErr:=os.Stat(filepath.Join(project,".tl-studio-write-orphan"));!errors.Is(statErr,os.ErrNotExist){
		t.Fatalf("unexpected write staging artifact: %v",statErr)
	}
}
