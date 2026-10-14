package main

import (
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestLocalAPIGateRejectsUnauthorizedNativeApproval(t *testing.T){
 secret,err:=randomSecret(32);if err!=nil{t.Fatal(err)}
 gate,err:=newLocalAPIGate(secret);if err!=nil{t.Fatal(err)}
 approved:=false
 next:=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path=="/local/permissions/pending/reply" {
   approved=true
  }
  w.WriteHeader(http.StatusOK)
 })
 server:=localOnly(gate.handler(next))
 request:=func(method,path string,headers map[string]string)*httptest.ResponseRecorder{
  r:=httptest.NewRequest(method,"http://127.0.0.1:17111"+path,strings.NewReader("{}"))
  for k,v:=range headers{r.Header.Set(k,v)}
  w:=httptest.NewRecorder();server.ServeHTTP(w,r);return w
 }
 for _,headers:=range []map[string]string{
  {},
  {"Origin":"http://127.0.0.1:17111"},
  {"Authorization":"Bearer wrong"},
 }{
  response:=request("POST","/local/permissions/pending/reply",headers)
  if response.Code!=http.StatusUnauthorized{t.Fatalf("unauthorized approval passed: %d",response.Code)}
 }
 if approved{t.Fatal("unauthorized local client approved a pending tool")}
 response:=request("POST","/local/auth/session",map[string]string{"X-TL-Studio-Launch-Token":secret})
 if response.Code!=http.StatusNoContent{t.Fatalf("bootstrap failed: %d",response.Code)}
 cookies:=response.Result().Cookies()
 if len(cookies)!=1||!cookies[0].HttpOnly||cookies[0].SameSite!=http.SameSiteStrictMode{
  t.Fatalf("unsafe browser session cookie: %+v",cookies)
 }
 response=request("POST","/local/permissions/pending/reply",map[string]string{
  "Cookie":cookies[0].Name+"="+cookies[0].Value,
 })
 if response.Code!=http.StatusForbidden||approved{t.Fatal("ambient cookie without CSRF header approved a command")}
 response=request("POST","/local/permissions/pending/reply",map[string]string{
  "Cookie":cookies[0].Name+"="+cookies[0].Value,"X-TL-Studio-Request":"1",
 })
 if response.Code!=http.StatusOK||!approved{t.Fatal("authorized UI cannot approve legitimate tool")}
}

func TestLocalAPIGateAllowsAuthenticatedEventStreamAndInvalidatesRestart(t *testing.T){
 secret,err:=randomSecret(32);if err!=nil{t.Fatal(err)}
 gate,err:=newLocalAPIGate(secret);if err!=nil{t.Fatal(err)}
 handler:=gate.handler(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(http.StatusOK)}))
 login:=httptest.NewRequest("POST","http://127.0.0.1:17111/local/auth/session",nil)
 login.Header.Set("X-TL-Studio-Launch-Token",secret)
 result:=httptest.NewRecorder();handler.ServeHTTP(result,login)
 cookie:=result.Result().Cookies()[0]
 event:=httptest.NewRequest("GET","http://127.0.0.1:17111/local/events",nil)
 event.AddCookie(cookie)
 ok:=httptest.NewRecorder();handler.ServeHTTP(ok,event)
 if ok.Code!=http.StatusOK{t.Fatalf("EventSource cookie rejected: %d",ok.Code)}
 secondSecret,_:=randomSecret(32)
 newGate,err:=newLocalAPIGate(secondSecret);if err!=nil{t.Fatal(err)}
 lost:=httptest.NewRecorder();newGate.handler(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(200)})).ServeHTTP(lost,event)
 if lost.Code!=401{t.Fatalf("old cookie survived restart: %d",lost.Code)}
}
