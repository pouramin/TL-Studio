package main

import (
    "strings"
    "testing"
)

func TestCheckpointConversationBoundedAndWithoutProtocolToolCalls(t *testing.T) {
    long := strings.Repeat("large tool payload ",20000)
    history:=[]nativeConversationMessage{{Role:"user", Text:long}}
    for i:=0;i<100;i++ {
        history=append(history,nativeConversationMessage{Role:"assistant",ToolCalls:[]nativeModelToolCall{{ID:"call-123",Name:"files.read"}},Text:long})
        history=append(history,nativeConversationMessage{Role:"tool",ToolCallID:"call-123",ToolName:"files.read",Text:long})
    }
    result:=nativeCheckpointConversation(history,long)
    if len(result)!=1 || result[0].Role!="user" || len(result[0].ToolCalls)!=0 || len([]rune(result[0].Text))>5530 {
        t.Fatalf("checkpoint is not a single bounded plain-text conversation: %d", len([]rune(result[0].Text)))
    }
    if !strings.Contains(result[0].Text,"Original user request") || !strings.Contains(result[0].Text,"Tool requested") {
        t.Fatalf("checkpoint lost essential outline: %q",result[0].Text)
    }
    if !strings.Contains(result[0].Text,"truncated") { t.Fatal("large context was not bounded") }
}
