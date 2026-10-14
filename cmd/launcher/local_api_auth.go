package main

import (
 "crypto/subtle"
 "encoding/hex"
 "errors"
 "net/http"
 "strings"
 "time"
)

const localAuthCookiePrefix = "tl_studio_auth_"

type localAPIGate struct {
 launchSecret string
 sessionToken string
 cookieName string
}

func newLocalAPIGate(secret string)(*localAPIGate,error){
 if len(secret)!=64 {return nil,errors.New("local API launch key must be 256 bits")}
 if _,err:=hex.DecodeString(secret);err!=nil{return nil,err}
 session,err:=randomSecret(32)
 if err!=nil{return nil,err}
 return &localAPIGate{launchSecret:secret,sessionToken:session,cookieName:localAuthCookiePrefix+secret[:16]},nil
}
func secureTokenEqual(a,b string)bool{
 if len(a)!=len(b){return false}
 return subtle.ConstantTimeCompare([]byte(a),[]byte(b))==1
}
func (g *localAPIGate) authorized(r *http.Request)bool{
 // Header is intended for local CLI and test tools, NOT untrusted project
 // content. The browser uses an HttpOnly cookie after bootstrap.
 token:=strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"),"Bearer "))
 if strings.HasPrefix(r.Header.Get("Authorization"),"Bearer ")&&secureTokenEqual(token,g.launchSecret){return true}
 cookie,err:=r.Cookie(g.cookieName)
 return err==nil&&secureTokenEqual(cookie.Value,g.sessionToken)
}
func (g *localAPIGate) handler(next http.Handler) http.Handler{
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path=="/local/auth/session" {
   if r.Method!=http.MethodPost{w.Header().Set("Allow","POST");http.Error(w,"method not allowed",http.StatusMethodNotAllowed);return}
   if !secureTokenEqual(r.Header.Get("X-TL-Studio-Launch-Token"),g.launchSecret){
    http.Error(w,"unauthorized",http.StatusUnauthorized);return
   }
   http.SetCookie(w,&http.Cookie{
    Name:g.cookieName,Value:g.sessionToken,Path:"/",
    HttpOnly:true,SameSite:http.SameSiteStrictMode,
    MaxAge:24*60*60,Expires:time.Now().Add(24*time.Hour),
   })
   w.Header().Set("Cache-Control","no-store")
   w.WriteHeader(http.StatusNoContent)
   return
  }
  if strings.HasPrefix(r.URL.Path,"/local/") {
   if !g.authorized(r) {
    w.Header().Set("Cache-Control","no-store")
    http.Error(w,"unauthorized",http.StatusUnauthorized)
    return
   }
   switch r.Method {
   case http.MethodGet,http.MethodHead,http.MethodOptions:
   default:
    // A browser cookie is ambient authority. Require a same-origin custom
    // header for mutation so a basic cross-site form cannot use it.
    if !strings.HasPrefix(r.Header.Get("Authorization"),"Bearer ")&&r.Header.Get("X-TL-Studio-Request")!="1" {
     http.Error(w,"missing same-origin request header",http.StatusForbidden)
     return
    }
   }
  }
  next.ServeHTTP(w,r)
 })
}
