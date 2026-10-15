package main

import (
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

// Explicit capabilities, not cookies: cookies do not isolate loopback ports.
type localAPIGate struct{ launchSecret string }

func newLocalAPIGate(secret string) (*localAPIGate, error) {
	if len(secret) != 64 {
		return nil, errors.New("local API launch key must be 256 bits")
	}
	if _, err := hex.DecodeString(secret); err != nil {
		return nil, err
	}
	return &localAPIGate{launchSecret: secret}, nil
}
func secureTokenEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func (g *localAPIGate) authorized(r *http.Request) bool {
	bearer := r.Header.Get("Authorization")
	return strings.HasPrefix(bearer, "Bearer ") && secureTokenEqual(strings.TrimPrefix(bearer, "Bearer "), g.launchSecret)
}
func (g *localAPIGate) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/local/auth/session" {
			w.Header().Set("Cache-Control", "no-store")
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				http.Error(w, "method not allowed", 405)
				return
			}
			if !secureTokenEqual(r.Header.Get("X-TL-Studio-Launch-Token"), g.launchSecret) {
				http.Error(w, "unauthorized", 401)
				return
			}
			// Validate the capability without creating ambient browser authority.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path != "/local/health" && strings.HasPrefix(r.URL.Path, "/local/") && !g.authorized(r) {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "unauthorized", 401)
			return
		}
		next.ServeHTTP(w, r)
	})
}
