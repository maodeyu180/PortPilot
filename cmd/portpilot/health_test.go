package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthCheckValidatesPanelResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"ready", `{"authenticated":false,"passwordRequired":true}`, 200, true},
		{"login required", `{"error":"unauthorized"}`, 401, false},
		{"wrong service", `<html>other service</html>`, 200, false},
		{"missing session", `{}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/session" {
					t.Error("unexpected health check route")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer s.Close()
			address := strings.TrimPrefix(s.URL, "http://")
			if (checkHealth(address) == nil) != tc.ok {
				t.Fatal("incorrect health status")
			}
			if tc.ok {
				_, port, _ := net.SplitHostPort(address)
				if err := checkHealth(net.JoinHostPort("0.0.0.0", port)); err != nil {
					t.Fatal("wildcard listener should be checked through loopback")
				}
			}
		})
	}
	if checkHealth("invalid-address") == nil {
		t.Fatal("invalid bind address accepted")
	}
}
