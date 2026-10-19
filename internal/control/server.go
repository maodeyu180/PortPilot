package control

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type App struct {
	store         *Store
	syncer        *Synchronizer
	auth          *adminAuth
	sessionKey    []byte
	applyMu       sync.Mutex
	testMu        sync.Mutex
	loginMu       sync.Mutex
	loginFailures map[string][]time.Time
}

func NewApp(store *Store, password string) (*App, error) {
	auth, err := openAdmin(store.dir, password)
	if err != nil {
		return nil, err
	}
	return &App{store: store, syncer: &Synchronizer{store: store}, auth: auth, sessionKey: []byte(randomID()), loginFailures: map[string][]time.Time{}}, nil
}

func (a *App) SetupRequired() bool { return a.auth.setupRequired() }

func (a *App) RunSync(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-a.auth.ready:
		a.syncer.Run(ctx)
	}
}
func (a *App) signature(s string) string {
	mac := hmac.New(sha256.New, a.sessionKey)
	mac.Write([]byte(s))
	return hex.EncodeToString(mac.Sum(nil))
}
func (a *App) authorized(r *http.Request) bool {
	if a.auth.setupRequired() {
		return false
	}
	cookie, err := r.Cookie("portpilot_session")
	if err != nil {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}
	expires, err := strconv.ParseInt(parts[0], 10, 64)
	return err == nil && expires > time.Now().Unix() && hmac.Equal([]byte(parts[1]), []byte(a.signature(parts[0])))
}

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func decode(r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fmt.Errorf("请求必须为 JSON")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxSubscriptionSize+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("请求内容无效或包含不支持的字段")
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("请求只能包含一个 JSON 对象")
	}
	return nil
}

func (a *App) Handler(assets fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]bool{"authenticated": a.authorized(r), "passwordRequired": true, "setupRequired": a.auth.setupRequired()})
	})
	mux.HandleFunc("POST /api/setup", a.setup)
	mux.HandleFunc("POST /api/login", a.login)
	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "portpilot_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		respond(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, _ *http.Request) { respond(w, 200, a.store.Snapshot()) })
	mux.HandleFunc("PUT /api/settings", a.updateSettings)
	mux.HandleFunc("POST /api/subscriptions", a.addSubscription)
	mux.HandleFunc("PUT /api/subscriptions/{id}", a.editSubscription)
	mux.HandleFunc("DELETE /api/subscriptions/{id}", a.deleteSubscription)
	mux.HandleFunc("POST /api/subscriptions/{id}/sync", func(w http.ResponseWriter, r *http.Request) {
		if err := a.syncer.Sync(r.Context(), r.PathValue("id")); err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 200, a.store.Snapshot())
	})
	mux.HandleFunc("POST /api/nodes", a.addNode)
	mux.HandleFunc("POST /api/nodes/convert", a.convertNodes)
	mux.HandleFunc("PATCH /api/nodes/{id}", a.editNode)
	mux.HandleFunc("DELETE /api/nodes/{id}", a.deleteNode)
	mux.HandleFunc("POST /api/nodes/batch", a.batchNodes)
	mux.HandleFunc("POST /api/nodes/test", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := decode(r, &body); err != nil {
			fail(w, 400, err.Error())
			return
		}
		ids := map[string]bool{}
		for _, id := range body.IDs {
			ids[id] = true
		}
		n, err := a.testNodes(r.Context(), ids)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 200, map[string]int{"tested": n})
	})
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		data, err := GenerateConfig(a.store.Snapshot())
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		if r.URL.Query().Get("download") == "1" {
			w.Header().Set("Content-Disposition", `attachment; filename="portpilot.yaml"`)
		}
		_, _ = w.Write(data)
	})
	mux.HandleFunc("POST /api/export", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Protocol string   `json:"protocol"`
			IDs      []string `json:"ids"`
		}
		if err := decode(r, &body); err != nil {
			fail(w, 400, err.Error())
			return
		}
		ids := map[string]bool{}
		for _, id := range body.IDs {
			ids[id] = true
		}
		links, err := ExportLinks(a.store.Snapshot(), body.Protocol, ids)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 200, map[string]string{"links": links})
	})
	mux.HandleFunc("GET /api/core", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, getCoreStatus(r.Context(), a.store.Snapshot().Settings))
	})
	mux.HandleFunc("POST /api/core/apply", func(w http.ResponseWriter, r *http.Request) {
		var body struct{}
		if err := decode(r, &body); err != nil {
			fail(w, 400, err.Error())
			return
		}
		if err := a.apply(r.Context()); err != nil {
			fail(w, 400, err.Error())
			return
		}
		respond(w, 200, a.store.Snapshot())
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) { fail(w, 404, "接口不存在") })
	fileServer := http.FileServer(http.FS(assets))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fileServer.ServeHTTP(w, r) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					fail(w, 403, "不允许跨站请求")
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "不允许跨站请求")
				return
			}
			if r.URL.Path != "/api/session" && r.URL.Path != "/api/login" && r.URL.Path != "/api/setup" && !a.authorized(r) {
				fail(w, 401, "请先登录控制台")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxSubscriptionSize)
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *App) updateSettings(w http.ResponseWriter, r *http.Request) {
	var settings Settings
	if err := decode(r, &settings); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if err := validateSettings(settings); err != nil {
		fail(w, 400, err.Error())
		return
	}
	err := a.store.Update(func(st *State) error {
		for _, n := range st.Nodes {
			if n.Port < settings.PortStart || n.Port > settings.PortEnd {
				return fmt.Errorf("新端口范围必须包含所有现有端口；现有映射不会自动改号")
			}
		}
		st.Settings = settings
		st.Revision++
		st.event("info", "运行与认证设置已更新，等待应用")
		return nil
	})
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	respond(w, 200, a.store.Snapshot())
}

func (a *App) addSubscription(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Interval int    `json:"interval"`
		Content  string `json:"content"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" || len(body.Name) > 100 || body.Interval < 0 || body.Interval > 8760 {
		fail(w, 400, "请填写订阅名称和有效的更新间隔")
		return
	}
	var proxies []map[string]any
	var err error
	if body.Content != "" {
		proxies, err = parseProxies([]byte(body.Content))
		body.URL = ""
		body.Interval = 0
	} else {
		proxies, err = fetchSubscription(r.Context(), body.URL)
	}
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	now := time.Now().UTC()
	proxies, info := separateSubscriptionInfo(proxies)
	if len(proxies) == 0 {
		fail(w, 400, "订阅中没有实际代理节点")
		return
	}
	sub := Subscription{ID: randomID(), Name: strings.TrimSpace(body.Name), URL: body.URL, Interval: body.Interval, UpdatedAt: &now, AttemptedAt: &now, NodeCount: len(proxies), Info: info}
	err = a.store.Update(func(st *State) error {
		st.Subscriptions = append(st.Subscriptions, sub)
		if err := reconcile(st, sub.ID, proxies); err != nil {
			return err
		}
		st.event("success", fmt.Sprintf("已导入 %s，共 %d 个节点", sub.Name, len(proxies)))
		return nil
	})
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	respond(w, 201, a.store.Snapshot())
}

func (a *App) editSubscription(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Interval int    `json:"interval"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" || len(body.Name) > 100 || (body.URL != "" && !validHTTP(body.URL)) || body.Interval < 0 || body.Interval > 8760 || (body.URL == "" && body.Interval != 0) {
		fail(w, 400, "订阅名称、地址或更新间隔无效")
		return
	}
	err := a.store.Update(func(st *State) error {
		for i := range st.Subscriptions {
			if st.Subscriptions[i].ID == r.PathValue("id") {
				sub := &st.Subscriptions[i]
				sub.Name = body.Name
				sub.URL = body.URL
				sub.Interval = body.Interval
				sub.Error = ""
				st.event("info", "订阅设置已更新："+body.Name)
				return nil
			}
		}
		return fmt.Errorf("订阅不存在")
	})
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	respond(w, 200, a.store.Snapshot())
}

func (a *App) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	err := a.store.Update(func(st *State) error {
		found := false
		for i, sub := range st.Subscriptions {
			if sub.ID == r.PathValue("id") {
				st.Subscriptions = append(st.Subscriptions[:i], st.Subscriptions[i+1:]...)
				st.event("info", "已删除订阅："+sub.Name)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("订阅不存在")
		}
		kept := []Node{}
		for _, n := range st.Nodes {
			if n.SubscriptionID != r.PathValue("id") {
				kept = append(kept, n)
			}
		}
		st.Nodes = kept
		st.Revision++
		return nil
	})
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	respond(w, 200, a.store.Snapshot())
}

func (a *App) convertNodes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, 400, err.Error())
		return
	}
	proxies, err := parseProxies([]byte(body.Content))
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	data, err := marshalImportedProxies(proxies)
	if err != nil {
		fail(w, 400, "无法生成节点配置")
		return
	}
	types := []string{}
	seen := map[string]bool{}
	for _, p := range proxies {
		typ := p["type"].(string)
		if !seen[typ] {
			types = append(types, typ)
			seen[typ] = true
		}
	}
	respond(w, 200, map[string]any{"yaml": string(data), "count": len(proxies), "types": types})
}

func (a *App) addNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
		YAML    string `json:"yaml"` // Kept for clients using the original single-node API.
		Port    int    `json:"port"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if body.Content != "" && body.YAML != "" {
		fail(w, 400, "content 与 yaml 只能提供一项")
		return
	}
	content := body.Content
	if content == "" {
		content = body.YAML
	}
	proxies, err := parseProxies([]byte(content))
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	err = a.store.Update(func(st *State) error {
		if body.Port != 0 {
			if err := checkPort(st, body.Port, ""); err != nil {
				return err
			}
		}
		for i, p := range proxies {
			port := body.Port + i
			if body.Port == 0 {
				var err error
				port, err = allocatePort(st)
				if err != nil {
					return err
				}
			}
			if err := checkPort(st, port, ""); err != nil {
				return fmt.Errorf("第 %d 个节点：%w", i+1, err)
			}
			st.Nodes = append(st.Nodes, Node{ID: randomID(), Name: p["name"].(string), SourceName: p["name"].(string), Type: p["type"].(string), Server: p["server"].(string), Port: port, Enabled: true, Available: true, Config: p})
		}
		st.Revision++
		st.event("success", fmt.Sprintf("已添加 %d 个独立节点", len(proxies)))
		return nil
	})
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	respond(w, 201, a.store.Snapshot())
}

func checkPort(st *State, port int, exceptID string) error {
	if port < st.Settings.PortStart || port > st.Settings.PortEnd {
		return fmt.Errorf("入口端口必须在配置的端口范围内")
	}
	u, _ := url.Parse(st.Settings.ControllerURL)
	if fmt.Sprint(port) == u.Port() {
		return fmt.Errorf("该端口已用于 Mihomo 控制器")
	}
	for _, n := range st.Nodes {
		if n.Port == port && n.ID != exceptID {
			return fmt.Errorf("该入口端口已被其他节点使用")
		}
	}
	return nil
}

func (a *App) editNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    *string `json:"name"`
		Port    *int    `json:"port"`
		Enabled *bool   `json:"enabled"`
		YAML    *string `json:"yaml"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, 400, err.Error())
		return
	}
	var proxy map[string]any
	if body.YAML != nil {
		if yaml.Unmarshal([]byte(*body.YAML), &proxy) != nil {
			fail(w, 400, "节点 YAML 格式有误")
			return
		}
		data, _ := yaml.Marshal(map[string]any{"proxies": []map[string]any{proxy}})
		if _, err := parseProxies(data); err != nil {
			fail(w, 400, err.Error())
			return
		}
	}
	err := a.store.Update(func(st *State) error {
		for i := range st.Nodes {
			n := &st.Nodes[i]
			if n.ID != r.PathValue("id") {
				continue
			}
			if body.Name != nil {
				if strings.TrimSpace(*body.Name) == "" || len(*body.Name) > 200 {
					return fmt.Errorf("节点名称不能为空且最多 200 字节")
				}
				n.Name = *body.Name
			}
			if body.Port != nil {
				if err := checkPort(st, *body.Port, n.ID); err != nil {
					return err
				}
				n.Port = *body.Port
			}
			if body.Enabled != nil {
				n.Enabled = *body.Enabled
			}
			if proxy != nil {
				if n.SubscriptionID != "" {
					return fmt.Errorf("订阅节点的上游配置请在订阅源中修改")
				}
				n.Config = proxy
				n.Type = fmt.Sprint(proxy["type"])
				n.Server = fmt.Sprint(proxy["server"])
				n.Latency = nil
				n.TestedAt = nil
				n.TestError = ""
			}
			st.Revision++
			st.event("info", "节点映射已更新："+n.Name)
			return nil
		}
		return fmt.Errorf("节点不存在")
	})
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	respond(w, 200, a.store.Snapshot())
}

func (a *App) deleteNode(w http.ResponseWriter, r *http.Request) {
	err := a.store.Update(func(st *State) error {
		for i, n := range st.Nodes {
			if n.ID == r.PathValue("id") {
				st.Nodes = append(st.Nodes[:i], st.Nodes[i+1:]...)
				st.Revision++
				st.event("info", "已删除节点："+n.Name)
				return nil
			}
		}
		return fmt.Errorf("节点不存在")
	})
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	respond(w, 200, a.store.Snapshot())
}

func (a *App) batchNodes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs     []string `json:"ids"`
		Enabled bool     `json:"enabled"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if len(body.IDs) == 0 {
		fail(w, 400, "请先选择节点")
		return
	}
	ids := map[string]bool{}
	for _, id := range body.IDs {
		ids[id] = true
	}
	err := a.store.Update(func(st *State) error {
		count := 0
		for i := range st.Nodes {
			if ids[st.Nodes[i].ID] {
				st.Nodes[i].Enabled = body.Enabled
				count++
			}
		}
		if count == 0 {
			return fmt.Errorf("所选节点不存在")
		}
		st.Revision++
		st.event("info", fmt.Sprintf("已批量更新 %d 个节点", count))
		return nil
	})
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	respond(w, 200, a.store.Snapshot())
}
