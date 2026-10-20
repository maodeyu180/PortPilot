package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNodeConnectivitySelection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selected []int
		fail     bool
		want     []int
	}{
		{name: "single node succeeds", selected: []int{1}, want: []int{1}},
		{name: "single node fails", selected: []int{1}, fail: true, want: []int{1}},
		{name: "selected batch", selected: []int{0, 1, 2, 3}, want: []int{0, 1}},
		{name: "empty selection tests all eligible nodes", want: []int{0, 1}},
		{name: "disabled node is skipped", selected: []int{2}},
		{name: "unavailable node is skipped", selected: []int{3}},
		{name: "unknown ID never falls back to all nodes", selected: []int{-1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, handler := authenticatedTestHandler(t)
			var mu sync.Mutex
			var calls []string
			controllerSecret := a.store.Snapshot().Settings.ControllerSecret
			controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+controllerSecret {
					t.Error("invalid controller request or authentication")
				}
				switch r.URL.Path {
				case "/version":
					respond(w, http.StatusOK, map[string]string{"version": "test-core"})
				case "/connections":
					respond(w, http.StatusOK, map[string]any{"connections": []any{}})
				default:
					if !strings.HasPrefix(r.URL.Path, "/proxies/") || !strings.HasSuffix(r.URL.Path, "/delay") {
						t.Errorf("unexpected controller path: %s", r.URL.Path)
						w.WriteHeader(http.StatusNotFound)
						return
					}
					if r.URL.Query().Get("url") != "https://example.com/connectivity-check" || r.URL.Query().Get("timeout") != "5000" {
						t.Error("node test did not use the configured URL and timeout")
					}
					mu.Lock()
					calls = append(calls, r.URL.Path)
					mu.Unlock()
					if tc.fail {
						w.WriteHeader(http.StatusGatewayTimeout)
						return
					}
					respond(w, http.StatusOK, map[string]int{"delay": 91})
				}
			}))
			defer controller.Close()

			if err := a.store.Update(func(st *State) error {
				if err := reconcile(st, "demo", fixtureProxies(t)); err != nil {
					return err
				}
				disabled, unavailable := st.Nodes[0], st.Nodes[1]
				disabled.ID, disabled.Name, disabled.Port, disabled.Enabled = "disabled-node", "停用节点", 20003, false
				unavailable.ID, unavailable.Name, unavailable.Port, unavailable.Available = "unavailable-node", "失效节点", 20004, false
				st.Nodes = append(st.Nodes, disabled, unavailable)
				previousTest := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
				for i := range st.Nodes {
					delay := 300 + i
					st.Nodes[i].TestedAt = &previousTest
					st.Nodes[i].Latency = &delay
				}
				st.Nodes[1].Latency = nil
				st.Nodes[1].TestError = "上次连接失败"
				st.Settings.ControllerURL = controller.URL
				st.Settings.TestURL = "https://example.com/connectivity-check"
				st.AppliedRevision = st.Revision
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := a.store.Snapshot()
			ids := make([]string, 0, len(tc.selected))
			for _, index := range tc.selected {
				if index < 0 {
					ids = append(ids, "deleted-node")
				} else {
					ids = append(ids, before.Nodes[index].ID)
				}
			}
			response := request(handler, http.MethodPost, "/api/nodes/test", map[string]any{"ids": ids})
			var result struct {
				Tested int `json:"tested"`
			}
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Tested != len(tc.want) {
				t.Fatalf("unexpected test result: %d %s", response.Code, response.Body.String())
			}
			var wantCalls []string
			for _, index := range tc.want {
				wantCalls = append(wantCalls, "/proxies/"+configName(before.Nodes[index])+"/delay")
			}
			mu.Lock()
			actualCalls := slices.Clone(calls)
			mu.Unlock()
			if !slices.Equal(actualCalls, wantCalls) {
				t.Fatalf("tested unexpected nodes: got %v, want %v", actualCalls, wantCalls)
			}
			after := a.store.Snapshot()
			for i, node := range after.Nodes {
				if !slices.Contains(tc.want, i) {
					if !reflect.DeepEqual(node, before.Nodes[i]) {
						t.Fatalf("unselected node %d or its previous test result changed", i)
					}
					continue
				}
				if node.TestedAt == nil || !node.TestedAt.After(*before.Nodes[i].TestedAt) {
					t.Fatal("selected node test timestamp was not updated")
				}
				if tc.fail {
					if node.Latency != nil || node.TestError == "" {
						t.Fatal("failed node test did not update the selected result")
					}
				} else if node.Latency == nil || *node.Latency != 91 || node.TestError != "" {
					t.Fatal("successful node test did not replace the previous result")
				}
			}
			if after.Revision != before.Revision || after.AppliedRevision != before.AppliedRevision {
				t.Fatal("connectivity testing changed the applied configuration")
			}
		})
	}
}
