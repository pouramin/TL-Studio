package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalAPIGateRejectsAmbientAuthority(t *testing.T) {
	secret, _ := randomSecret(32)
	gate, _ := newLocalAPIGate(secret)
	handler := localOnly(gate.handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })))
	request := func(method, path string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:17111"+path, strings.NewReader("{}"))
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, headers := range []map[string]string{{}, {"Cookie": "tl_studio_auth_legacy=" + secret, "X-TL-Studio-Request": "1"}, {"Authorization": "Bearer wrong"}} {
		if got := request("POST", "/local/permissions/request/reply", headers).Code; got != 401 {
			t.Fatalf("ambient authority accepted: %d", got)
		}
	}
	login := request("POST", "/local/auth/session", map[string]string{"X-TL-Studio-Launch-Token": secret})
	if login.Code != 204 || len(login.Result().Cookies()) != 0 {
		t.Fatal("bootstrap must not issue cookies")
	}
	for _, path := range []string{"/local/status", "/local/events", "/local/permissions/request/reply"} {
		if got := request("POST", path, map[string]string{"Authorization": "Bearer " + secret}).Code; got != 200 {
			t.Fatalf("explicit capability rejected: %s %d", path, got)
		}
	}
	if got := request("POST", "/local/permissions/request/reply", map[string]string{"Authorization": "Bearer " + secret, "Origin": "http://127.0.0.1:9999"}).Code; got != 403 {
		t.Fatal("cross-origin capability request passed")
	}
}

func TestLocalAPIGateInvalidatesRestart(t *testing.T) {
	first, _ := randomSecret(32)
	second, _ := randomSecret(32)
	gate, _ := newLocalAPIGate(second)
	req := httptest.NewRequest("GET", "http://127.0.0.1:17111/local/events", nil)
	req.Header.Set("Authorization", "Bearer "+first)
	w := httptest.NewRecorder()
	gate.handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })).ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("old capability survived restart")
	}
}
