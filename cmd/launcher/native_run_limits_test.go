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

func TestNativeRunLimitValidation(t *testing.T) {
    for _, input := range []sessionRunInput{
        {MaxToolRounds:-1}, {MaxToolRounds:101},
        {ModelTimeoutSeconds:20}, {ModelTimeoutSeconds:1201},
        {StreamIdleTimeoutSeconds:9}, {StreamIdleTimeoutSeconds:301},
        {MaxRunTokens:-1}, {MaxRunTokens:10000001},
    } {
        if err := validateNativeRunLimits(input); err == nil {
            t.Fatalf("unsafe run budget accepted: %+v", input)
        }
    }
    if err := validateNativeRunLimits(sessionRunInput{MaxToolRounds:100, ModelTimeoutSeconds:600, StreamIdleTimeoutSeconds:60, MaxRunTokens:2000000}); err != nil {
        t.Fatal(err)
    }
    if nativeRunToolRoundLimit(sessionRunInput{}) != 24 || nativeRunIterationLimit(sessionRunInput{MaxToolRounds:100}) < 101 {
        t.Fatal("run default/extended iteration guard mismatch")
    }
    if nativeRunTurnTimeout(sessionRunInput{ModelTimeoutSeconds:360}, time.Minute) != 6*time.Minute {
        t.Fatal("model timeout override missing")
    }
    if nativeRunIdleTimeout(sessionRunInput{StreamIdleTimeoutSeconds:60}) != time.Minute {
        t.Fatal("stream idle override missing")
    }
}

func TestNativeAlternatingCycleGuard(t *testing.T) {
    history := []string{}
    for i:=0;i<11;i++ {
        path := "a.txt"
        if i%2==1 { path = "b.txt" }
        next,err:=nativeNextToolLoopHistory(history, []nativeModelToolCall{{Name:"files.read",Arguments:json.RawMessage(fmt.Sprintf(`{"path":%q}`,path))}})
        if err != nil { t.Fatalf("premature A/B guard at %d: %v",i,err) }
        history=next
    }
    _,err:=nativeNextToolLoopHistory(history,[]nativeModelToolCall{{Name:"files.read",Arguments:json.RawMessage(`{"path":"b.txt"}`)}})
    if err==nil { t.Fatal("sustained ABAB loop not detected") }
    history=nil
    for i:=0;i<15;i++ {
        next,err:=nativeNextToolLoopHistory(history,[]nativeModelToolCall{{Name:"files.read",Arguments:json.RawMessage(fmt.Sprintf(`{"path":"file-%d"}`,i))}})
        if err!=nil { t.Fatalf("unique calls should not trigger guard: %v",err) }
        history=next
    }
}

type configuredRunProbeModel struct {
    mu sync.Mutex
    calls int
    toolRounds int
    finalRequests int
    finished chan struct{}
}

func (m *configuredRunProbeModel) Complete(_ context.Context, req nativeModelRequest, _ func(string)) (nativeModelResponse,error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.calls++
    if req.ToolsDisabled {
        m.finalRequests++
        if len(req.Tools)!=0 { return nativeModelResponse{},fmt.Errorf("final request still had executable tools") }
        close(m.finished)
        return nativeModelResponse{Text:"Verified: inspected files; Remaining: no further edits verified",Usage:sessionUsage{Input:12,Output:12}},nil
    }
    if len(req.Tools)==0 { return nativeModelResponse{},fmt.Errorf("normal model request missing tools") }
    m.toolRounds++
    return nativeModelResponse{ToolCalls:[]nativeModelToolCall{{
        Name:"files.read", ID:fmt.Sprintf("call-%d",m.toolRounds),
        Arguments:json.RawMessage(fmt.Sprintf(`{"path":"file-%d.txt"}`,m.toolRounds)),
    }},FinishReason:"tool_calls",Usage:sessionUsage{Input:100,Output:10}},nil
}

func TestConfigurableNativeRunBudgetFinalCheckpointUsesNoTools(t *testing.T) {
    project:=t.TempDir()
    for i:=1;i<=31;i++ {
        if err:=os.WriteFile(filepath.Join(project,fmt.Sprintf("file-%d.txt",i)),[]byte("ok"),0600);err!=nil { t.Fatal(err) }
    }
    store:=newSessionPersistenceStore(filepath.Join(t.TempDir(),"sessions"),"configured-budget")
    session,err:=store.createNativeSession(project,sessionCreateInput{Title:"Extended Budget"})
    if err!=nil { t.Fatal(err) }
    model:=&configuredRunProbeModel{finished:make(chan struct{})}
    runtime:=newNativeAgentRuntime(nativeTestResolver{
        provider:tlProviderDefinition{ID:"test",Protocol:"openai-compatible",BaseURL:"http://127.0.0.1:1/v1"},
        model:tlProviderModel{ID:"test-model",ToolCall:true},
    },model,newNativeToolExecutor(newProcessManager(func()string{return project}),nativeAllowAuthorizer{}),store,newLiveEventBus())
    if err:=runtime.Start(project,session.ID,sessionRunInput{
        Text:"Inspect project files",Agent:"code",Model:&sessionModelRef{ProviderID:"test",ID:"test-model"},
        MaxToolRounds:30,
    });err!=nil { t.Fatal(err) }
    select { case <-model.finished: case <-time.After(12*time.Second): t.Fatal("extended 30-tool test timed out") }
    deadline:=time.Now().Add(4*time.Second)
    for {
        if _,busy:=runtime.NativeStatuses(project)[session.ID];!busy { break }
        if time.Now().After(deadline){t.Fatal("native run did not finish")}
        time.Sleep(10*time.Millisecond)
    }
    model.mu.Lock()
    rounds,finals:=model.toolRounds,model.finalRequests
    model.mu.Unlock()
    if rounds!=30||finals!=1 {t.Fatalf("tools=%d final=%d, want 30 and 1",rounds,finals)}
    messages,ok,err:=store.getMessages(session.ID)
    if err!=nil||!ok {t.Fatalf("load messages: %v",err)}
    toolCount:=0
    checkpoint:=false
    for _,message:=range messages {
        if message.Role=="assistant" && strings.Contains(message.Text,"not verified complete") && strings.Contains(message.Text,"Remaining:") {checkpoint=true}
        for _,a:=range message.Activities{if a.ToolID=="files.read" && a.Status=="completed"{toolCount++}}
    }
    if toolCount!=30||!checkpoint{t.Fatalf("persisted tools=%d checkpoint=%v",toolCount,checkpoint)}
}

func TestNativeBillableTokenBudget(t *testing.T) {
    if nativeRunBillableTokens(sessionUsage{Input:12,Output:8,Reasoning:3,CacheRead:5})!=20 {t.Fatal("double counting output/reasoning")}
    if nativeRunBillableTokens(sessionUsage{Input:12,Reasoning:3})!=15 {t.Fatal("missing reasoning-only output")}
}
