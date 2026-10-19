package control

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"
)

const fixture = `proxies:
  - name: 香港 A
    type: anytls
    server: a.example.com
    port: 443
    password: sample-password
    sni: a.example.com
  - name: 日本 B
    type: vless
    server: b.example.com
    port: 443
    uuid: 12345678-1234-1234-1234-123456789abc
    tls: true
    reality-opts:
      public-key: sample-public-key
      short-id: abcd
`

func fixtureProxies(t *testing.T) []map[string]any {
	t.Helper()
	p, err := parseProxies([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReconcileKeepsPortsOnReorderRemovalAndReturn(t *testing.T) {
	st := initialState()
	p := fixtureProxies(t)
	if err := reconcile(&st, "sub", p); err != nil {
		t.Fatal(err)
	}
	a, b := st.Nodes[0], st.Nodes[1]
	st.Nodes[0].Name = "我的别名"
	st.Nodes[0].Enabled = false
	if err := reconcile(&st, "sub", []map[string]any{p[1], p[0]}); err != nil {
		t.Fatal(err)
	}
	if st.Nodes[0].ID != a.ID || st.Nodes[0].Port != a.Port || st.Nodes[1].Port != b.Port || st.Nodes[0].Name != "我的别名" || st.Nodes[0].Enabled {
		t.Fatal("reorder overwrote user mapping")
	}
	if err := reconcile(&st, "sub", p[1:]); err != nil {
		t.Fatal(err)
	}
	if st.Nodes[0].Available || st.Nodes[0].Port != a.Port {
		t.Fatal("missing node must reserve its port")
	}
	if err := reconcile(&st, "sub", p); err != nil {
		t.Fatal(err)
	}
	if !st.Nodes[0].Available || st.Nodes[0].Port != a.Port {
		t.Fatal("returning node must recover original port")
	}
}

func TestStoreRollsBackExhaustedAllocationAndPersistsPrivate(t *testing.T) {
	s := newStore(t)
	if err := s.Update(func(st *State) error { st.Settings.PortEnd = st.Settings.PortStart; return nil }); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	if err := s.Update(func(st *State) error { return reconcile(st, "sub", fixtureProxies(t)) }); err == nil {
		t.Fatal("expected exhaustion")
	}
	if len(s.Snapshot().Nodes) != 0 || s.Snapshot().Revision != before.Revision {
		t.Fatal("failed update must not partially persist")
	}
	info, err := os.Stat(filepath.Join(s.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %v", info.Mode())
	}
	reopened, err := OpenStore(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot().Settings.Password != before.Settings.Password {
		t.Fatal("credentials changed after reopen")
	}
}

func TestGenerateConfigPreservesNestedOptionsAndExactRouting(t *testing.T) {
	st := initialState()
	if err := reconcile(&st, "sub", fixtureProxies(t)); err != nil {
		t.Fatal(err)
	}
	data, err := GenerateConfig(st)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Listeners []map[string]any `yaml:"listeners"`
		Proxies   []map[string]any `yaml:"proxies"`
		Rules     []string         `yaml:"rules"`
	}
	if yaml.Unmarshal(data, &doc) != nil {
		t.Fatal("invalid YAML")
	}
	if len(doc.Listeners) != 2 || doc.Listeners[0]["type"] != "mixed" || doc.Listeners[0]["proxy"] != doc.Proxies[0]["name"] {
		t.Fatal("incorrect mapping")
	}
	if doc.Proxies[1]["reality-opts"] == nil {
		t.Fatal("nested upstream options lost")
	}
	if doc.Rules[0] != "MATCH,REJECT" {
		t.Fatal("unmapped traffic should fail closed")
	}
	st.Nodes[0].Enabled = false
	st.Nodes[1].Available = false
	data, err = GenerateConfig(st)
	if err != nil {
		t.Fatal(err)
	}
	if yaml.Unmarshal(data, &doc) != nil || len(doc.Listeners) != 0 {
		t.Fatal("disabled or missing nodes generated listeners")
	}
}

func TestExportEscapesCredentialsAndIPv6(t *testing.T) {
	st := initialState()
	_ = reconcile(&st, "sub", fixtureProxies(t))
	st.Settings.Username = "u:@字"
	st.Settings.Password = "p@:/?#%"
	st.Settings.PublicHost = "2001:db8::1"
	links, err := ExportLinks(st, "socks5h", map[string]bool{st.Nodes[0].ID: true})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(links)
	if err != nil {
		t.Fatal(err)
	}
	pass, _ := u.User.Password()
	if u.Scheme != "socks5h" || u.User.Username() != st.Settings.Username || pass != st.Settings.Password || u.Hostname() != st.Settings.PublicHost {
		t.Fatal("incorrect escaping")
	}
	if _, err = ExportLinks(st, "file", nil); err == nil {
		t.Fatal("unexpected protocol accepted")
	}
}

func TestParserRejectsInvalidAndDependentConfigs(t *testing.T) {
	for _, input := range []string{"<html>error</html>", "proxies: []", "proxies:\n- name: X\n  type: anytls\n", "proxies:\n- {name: X, type: anytls, server: x}\n- {name: X, type: vless, server: y}", "proxies:\n- {name: X, type: anytls, server: x, dialer-proxy: Y}"} {
		if _, err := parseProxies([]byte(input)); err == nil {
			t.Errorf("accepted invalid input %s", input)
		}
	}
}

func TestPortAllocationSkipsControllerAndDuplicateNamesAcrossSources(t *testing.T) {
	st := initialState()
	st.Settings.ControllerURL = "http://127.0.0.1:20001"
	if err := reconcile(&st, "one", fixtureProxies(t)); err != nil {
		t.Fatal(err)
	}
	if st.Nodes[0].Port != 20002 {
		t.Fatal("controller port assigned to a node")
	}
	if err := reconcile(&st, "two", fixtureProxies(t)); err != nil {
		t.Fatal(err)
	}
	data, err := GenerateConfig(st)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "type: mixed") != 4 {
		t.Fatal("duplicate source names were conflated")
	}
}

func testHandler(t *testing.T, password string) (*App, http.Handler) {
	t.Helper()
	a := NewApp(newStore(t), password)
	return a, a.Handler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("PortPilot")}})
}
func request(h http.Handler, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var input io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		input = bytes.NewReader(data)
	}
	r := httptest.NewRequest(method, "http://localhost"+path, input)
	r.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAuthenticationCSRFAndDNSRebinding(t *testing.T) {
	_, protected := testHandler(t, "long-admin-password")
	if w := request(protected, "GET", "/api/state", nil); w.Code != 401 {
		t.Fatal("unprotected state")
	}
	login := request(protected, "POST", "/api/login", map[string]string{"password": "long-admin-password"})
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	if w := request(protected, "GET", "/api/state", nil, cookies...); w.Code != 200 {
		t.Fatal("session not accepted")
	}
	r := httptest.NewRequest("GET", "http://localhost/api/state", nil)
	r.Header.Set("Origin", "https://evil.example")
	r.AddCookie(cookies[0])
	w := httptest.NewRecorder()
	protected.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross origin accepted")
	}
	_, local := testHandler(t, "")
	r = httptest.NewRequest("GET", "http://evil.example/api/state", nil)
	w = httptest.NewRecorder()
	local.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("DNS rebinding accepted")
	}
}

func TestSubscriptionNodeCRUDAndDuplicatePort(t *testing.T) {
	a, h := testHandler(t, "")
	w := request(h, "POST", "/api/subscriptions", map[string]any{"name": "测试订阅", "content": fixture, "interval": 0})
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	st := a.store.Snapshot()
	if len(st.Nodes) != 2 {
		t.Fatal("import failed")
	}
	id := st.Nodes[0].ID
	w = request(h, "PATCH", "/api/nodes/"+id, map[string]any{"port": st.Nodes[1].Port})
	if w.Code != 400 {
		t.Fatal("duplicate port accepted")
	}
	w = request(h, "PATCH", "/api/nodes/"+id, map[string]any{"name": "别名", "enabled": false})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if a.store.Snapshot().Nodes[0].Name != "别名" || a.store.Snapshot().Nodes[0].Enabled {
		t.Fatal("edit failed")
	}
	w = request(h, "DELETE", "/api/subscriptions/"+st.Subscriptions[0].ID, nil)
	if w.Code != 200 || len(a.store.Snapshot().Nodes) != 0 {
		t.Fatal("subscription cascade failed")
	}
}

func TestSubscriptionFetchRequestsClashAndDoesNotLeakURLOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.UserAgent(), "clash.meta") {
			t.Error("missing Clash user agent")
		}
		_, _ = w.Write([]byte(fixture))
	}))
	defer srv.Close()
	p, err := fetchSubscription(context.Background(), srv.URL+"?token=private-token")
	if err != nil || len(p) != 2 {
		t.Fatal(err)
	}
	_, err = fetchSubscription(context.Background(), "http://127.0.0.1:1/?token=private-token")
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("URL leaked to error")
	}
}

func TestApplyUsesPayloadAndPersistsOnlyAcceptedConfig(t *testing.T) {
	a, _ := testHandler(t, "")
	reject := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reject {
			w.WriteHeader(400)
			return
		}
		if r.URL.Path != "/configs" || r.Method != "PUT" {
			t.Errorf("incorrect request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(body["payload"], "listeners:") || body["path"] != "" {
			t.Error("expected inline payload")
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	_ = a.store.Update(func(st *State) error {
		st.Settings.ControllerURL = srv.URL
		return reconcile(st, "sub", fixtureProxies(t))
	})
	if a.apply(context.Background()) == nil || a.store.Snapshot().AppliedRevision != 0 {
		t.Fatal("rejected config marked applied")
	}
	if _, err := os.Stat(filepath.Join(a.store.dir, "mihomo.yaml")); !os.IsNotExist(err) {
		t.Fatal("rejected config persisted")
	}
	reject = false
	if err := a.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := a.store.Snapshot()
	if st.AppliedRevision != st.Revision || st.AppliedAt == nil {
		t.Fatal("accepted configuration not recorded")
	}
}

func TestStoreConcurrentUpdates(t *testing.T) {
	s := newStore(t)
	start := s.Snapshot().Revision
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Update(func(st *State) error { st.Revision++; return nil }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if s.Snapshot().Revision != start+16 {
		t.Fatal("lost concurrent updates")
	}
}

func TestSubscriptionNoticesDoNotConsumePorts(t *testing.T) {
	p := fixtureProxies(t)
	p = append(p, map[string]any{"name": "剩余流量：100 GB", "type": "anytls", "server": "a.example.com"})
	actual, info := separateSubscriptionInfo(p)
	if len(actual) != 2 || len(info) != 1 {
		t.Fatal("account notices treated as nodes")
	}
	_, h := testHandler(t, "")
	data, _ := yaml.Marshal(map[string]any{"proxies": p})
	w := request(h, "POST", "/api/subscriptions", map[string]any{"name": "来源", "content": string(data)})
	var state State
	_ = json.Unmarshal(w.Body.Bytes(), &state)
	if w.Code != 201 || len(state.Nodes) != 2 || len(state.Subscriptions[0].Info) != 1 {
		t.Fatal(w.Body.String())
	}
}
