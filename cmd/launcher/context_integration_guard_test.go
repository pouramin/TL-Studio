package main

import (
    "encoding/json"
    "strings"
    "testing"
)

func TestContextProtectsEveryResultFromMostRecentParallelToolRound(t *testing.T) {
    many := strings.Repeat("large context result ",500)
    payload := func(id string) string {
        data,_:=json.Marshal(map[string]any{"ok":true,"output":map[string]any{"content":many,"id":id}})
        return string(data)
    }
    messages:=[]nativeConversationMessage{
        {Role:"user", Text:"Inspect all selected files"},
        {Role:"assistant", ToolCalls:[]nativeModelToolCall{
            {ID:"previous",Name:"files.read",Arguments:json.RawMessage(`{"path":"old"}`)},
        }},
        {Role:"tool",ToolCallID:"previous",Text:payload("old")},
        {Role:"assistant",ToolCalls:[]nativeModelToolCall{
            {ID:"parallel-a",Name:"files.read",Arguments:json.RawMessage(`{"path":"a"}`)},
            {ID:"parallel-b",Name:"files.read",Arguments:json.RawMessage(`{"path":"b"}`)},
        }},
        {Role:"tool",ToolCallID:"parallel-a",Text:payload("a")},
        {Role:"tool",ToolCallID:"parallel-b",Text:payload("b")},
    }
    compacted,n,_:=nativeCompactCompletedToolHistory(messages,true)
    if n==0 { t.Fatal("older completed tool result should compact") }
    if compacted[4].Text!=messages[4].Text || compacted[5].Text!=messages[5].Text {
        t.Fatal("most recent multi-tool round lost verbatim results before model continuation")
    }
    if compacted[2].Text==messages[2].Text { t.Fatal("older tool result not compacted") }
}

func TestContextBudgetFailsClosedForOversizedLatestResult(t *testing.T) {
    messages:=[]nativeConversationMessage{
        {Role:"user",Text:"Read the report"},
        {Role:"assistant",ToolCalls:[]nativeModelToolCall{{ID:"latest",Name:"files.read"}}},
        {Role:"tool",ToolCallID:"latest",Text:strings.Repeat("X",90000)},
    }
    plan:=buildNativeContextPlan(messages,"System",nil,tlProviderModel{ContextLimit:4096,OutputLimit:1024})
    if !plan.OverBudget { t.Fatalf("oversized latest result not flagged as unsafe, size=%d",plan.EstimatedRequestTokens) }
    if len(plan.Messages)==0 || plan.Messages[len(plan.Messages)-1].Text!=messages[2].Text {
        t.Fatal("latest tool response must not be silently mutilated")
    }
}
