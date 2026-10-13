package main

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/http/httptest"
    "strings"
    "sync/atomic"
    "testing"
)

func TestNativeNoToolFinalRequestAcrossStreamAdapters(t *testing.T) {
    cases := []struct{
        protocol string
        path string
        answer string
        choice any
    }{
        {"openai-compatible", "/v1/chat/completions", `{"choices":[{"message":{"content":"checkpoint"},"finish_reason":"stop"}]}`, "none"},
        {"openai-responses", "/v1/responses", `{"output":[{"type":"message","content":[{"type":"output_text","text":"checkpoint"}]}]}`, "none"},
        {"anthropic-messages", "/v1/messages", `{"content":[{"type":"text","text":"checkpoint"}],"stop_reason":"end_turn"}`, map[string]any{"type":"none"}},
    }
    for _, tc:=range cases {
        t.Run(tc.protocol,func(t *testing.T){
            server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
                if r.URL.Path!=tc.path { t.Errorf("unexpected endpoint %s",r.URL.Path) }
                var request map[string]any
                if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil { t.Error(err);return }
                if _,ok:=request["tools"];ok {t.Error("tool definitions included in final request")}
                actual:=request["tool_choice"]
                expected,_:=json.Marshal(tc.choice)
                received,_:=json.Marshal(actual)
                if string(received)!=string(expected) {t.Errorf("tool_choice=%s, want %s",received,expected)}
                w.Header().Set("Content-Type","application/json")
                _,_=fmt.Fprint(w,tc.answer)
            }))
            defer server.Close()
            got,err:=newNativeModelClient().Complete(context.Background(),nativeModelRequest{
                System:"Summarize without tools",
                Provider:tlProviderDefinition{Protocol:tc.protocol,BaseURL:server.URL+"/v1"},
                Model:tlProviderModel{ID:"test",ToolCall:true},
                APIKey:"test", Messages:[]nativeConversationMessage{{Role:"user",Text:"Summarize"}},
                ToolsDisabled:true,
            },nil)
            if err!=nil {t.Fatal(err)}
            if !strings.Contains(got.Text,"checkpoint")||len(got.ToolCalls)>0 {t.Fatalf("bad no-tool response %+v",got)}
        })
    }
}

func TestNativeStreamSSEActivityCallback(t *testing.T) {
    server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,_ *http.Request){
        w.Header().Set("Content-Type","text/event-stream")
        _,_=fmt.Fprint(w,"data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n")
    }))
    defer server.Close()
    var events atomic.Int32
    response,err:=newNativeModelClient().Complete(context.Background(),nativeModelRequest{
        Provider:tlProviderDefinition{Protocol:"openai-compatible",BaseURL:server.URL},
        Model:tlProviderModel{ID:"test"}, APIKey:"test",
        Messages:[]nativeConversationMessage{{Role:"user",Text:"Hello"}},
        OnStreamActivity:func(){events.Add(1)},
    },nil)
    if err!=nil {t.Fatal(err)}
    if events.Load()==0 {t.Fatal("SSE heartbeat never reached the idle-watchdog hook")}
    if !strings.Contains(response.Text,"hello"){t.Fatalf("bad stream response %+v",response)}
}
