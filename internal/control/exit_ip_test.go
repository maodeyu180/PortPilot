package control

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func exitIPProxy(t *testing.T, settings Settings, status int, body string, beforeResponse func()) (int, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(settings.Username+":"+settings.Password))
		if r.Header.Get("Proxy-Authorization") != wantAuth || !r.URL.IsAbs() || r.URL.Host != "exit-ip.example.test" {
			t.Error("IP lookup did not use the selected authenticated proxy")
		}
		if beforeResponse != nil {
			beforeResponse()
		}
		if status == http.StatusFound {
			w.Header().Set("Location", "http://redirect.example.test/")
		}
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(proxy.Close)
	u, _ := url.Parse(proxy.URL)
	_, rawPort, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(rawPort)
	return port, &calls
}

func TestExitIPUsesAuthenticatedNodeProxyAndValidatesResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"IPv4", `{"ip":"203.0.113.42"}`, "203.0.113.42", 200},
		{"IPv6", `{"ip":"2001:db8::42"}`, "2001:db8::42", 200},
		{"invalid IP", `{"ip":"not-an-ip"}`, "", 200},
		{"private IP", `{"ip":"192.168.1.2"}`, "", 200},
		{"loopback IP", `{"ip":"127.0.0.1"}`, "", 200},
		{"IPv6 zone", `{"ip":"2001:db8::1%eth0"}`, "", 200},
		{"invalid JSON", `<html>blocked</html>`, "", 200},
		{"oversized response", strings.Repeat(" ", 4097), "", 200},
		{"HTTP failure", `unavailable`, "", 502},
		{"redirect is not followed", ``, "", 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := initialState().Settings
			settings.Username, settings.Password = "demo-user", "demo:p@ss/word?"
			port, calls := exitIPProxy(t, settings, tc.status, tc.body, nil)
			ip, err := fetchExitIP(context.Background(), settings, Node{Port: port}, "http://exit-ip.example.test/lookup?format=json")
			if ip != tc.want || (err == nil) != (tc.want != "") {
				t.Fatalf("unexpected lookup result: %q, %v", ip, err)
			}
			if calls.Load() != 1 {
				t.Fatal("lookup bypassed the proxy or followed a redirect")
			}
			if err != nil && strings.Contains(err.Error(), settings.Password) {
				t.Fatal("proxy password leaked to error")
			}
		})
	}
}

func TestExitIPQueryOnlyUpdatesSelectedNode(t *testing.T) {
	for _, tc := range []struct {
		name                                         string
		status                                       int
		body                                         string
		disabled, missing, pending, changed, unknown bool
		wantStatus, wantCalls                        int
	}{
		{name: "success", status: 200, body: `{"ip":"203.0.113.42"}`, wantStatus: 200, wantCalls: 1},
		{name: "failure clears only target cached IP", status: 502, wantStatus: 400, wantCalls: 1},
		{name: "disabled", disabled: true, wantStatus: 400},
		{name: "missing source", missing: true, wantStatus: 400},
		{name: "unapplied configuration", pending: true, wantStatus: 400},
		{name: "configuration changes during request", changed: true, status: 200, body: `{"ip":"203.0.113.42"}`, wantStatus: 400, wantCalls: 1},
		{name: "unknown node", unknown: true, wantStatus: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, handler := authenticatedTestHandler(t)
			a.exitIPURL = "http://exit-ip.example.test/lookup?format=json"
			port, calls := exitIPProxy(t, a.store.Snapshot().Settings, tc.status, tc.body, func() {
				if tc.changed {
					_ = a.store.Update(func(st *State) error { st.Revision++; return nil })
				}
			})
			if err := a.store.Update(func(st *State) error {
				if err := reconcile(st, "demo", fixtureProxies(t)); err != nil {
					return err
				}
				old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
				for i := range st.Nodes {
					st.Nodes[i].ExitIP, st.Nodes[i].ExitIPCheckedAt = "198.51.100.10", &old
				}
				st.Nodes[1].Port, st.Nodes[1].Enabled, st.Nodes[1].Available = port, !tc.disabled, !tc.missing
				st.AppliedRevision = st.Revision
				if tc.pending {
					st.Revision++
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := a.store.Snapshot()
			id := before.Nodes[1].ID
			if tc.unknown {
				id = "unknown-node"
			}
			response := request(handler, http.MethodPost, "/api/nodes/"+id+"/exit-ip", map[string]any{})
			if response.Code != tc.wantStatus || int(calls.Load()) != tc.wantCalls {
				t.Fatalf("unexpected response/calls: %d %d %s", response.Code, calls.Load(), response.Body.String())
			}
			after := a.store.Snapshot()
			if !reflect.DeepEqual(before.Nodes[0], after.Nodes[0]) {
				t.Fatal("unselected node changed")
			}
			if tc.wantCalls == 0 || tc.changed {
				if !reflect.DeepEqual(before.Nodes[1], after.Nodes[1]) {
					t.Fatal("ineligible or outdated query changed cached data")
				}
				return
			}
			node := after.Nodes[1]
			if node.ExitIPCheckedAt == nil || !node.ExitIPCheckedAt.After(*before.Nodes[1].ExitIPCheckedAt) {
				t.Fatal("query time was not updated")
			}
			if tc.wantStatus == 200 {
				if node.ExitIP != "203.0.113.42" || node.ExitIPError != "" {
					t.Fatal("exit IP was not saved")
				}
			} else if node.ExitIP != "" || node.ExitIPError == "" {
				t.Fatal("failed lookup retained a stale exit IP")
			}
			if before.Revision != after.Revision || before.AppliedRevision != after.AppliedRevision {
				t.Fatal("IP query changed applied configuration")
			}
		})
	}
}

func TestExitIPCacheInvalidatedByNodeChangesAndSubscriptionSync(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       map[string]any
		sync, keep bool
	}{
		{name: "alias preserves observation", body: map[string]any{"name": "新别名"}, keep: true},
		{name: "port change", body: map[string]any{"port": 20998}},
		{name: "disable", body: map[string]any{"enabled": false}},
		{name: "upstream change", body: map[string]any{"yaml": "name: changed\ntype: http\nserver: changed.example.com\nport: 443\n"}},
		{name: "subscription sync", sync: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, handler := authenticatedTestHandler(t)
			_ = a.store.Update(func(st *State) error {
				if err := reconcile(st, "", fixtureProxies(t)); err != nil {
					return err
				}
				now := time.Now().UTC()
				st.Nodes[0].ExitIP, st.Nodes[0].ExitIPCheckedAt, st.Nodes[0].ExitIPError = "203.0.113.42", &now, ""
				return nil
			})
			if tc.sync {
				if err := a.store.Update(func(st *State) error { return reconcile(st, "", fixtureProxies(t)) }); err != nil {
					t.Fatal(err)
				}
			} else {
				id := a.store.Snapshot().Nodes[0].ID
				response := request(handler, http.MethodPatch, "/api/nodes/"+id, tc.body)
				if response.Code != http.StatusOK {
					t.Fatal(response.Body.String())
				}
			}
			node := a.store.Snapshot().Nodes[0]
			if tc.keep {
				if node.ExitIP == "" || node.ExitIPCheckedAt == nil {
					t.Fatal("renaming invalidated the observed IP")
				}
			} else if node.ExitIP != "" || node.ExitIPCheckedAt != nil || node.ExitIPError != "" {
				t.Fatal("changed upstream retained a stale observation")
			}
		})
	}
}
