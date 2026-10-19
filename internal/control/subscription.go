package control

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const maxSubscriptionSize = 5 << 20

// Some providers encode account notices as copies of a real proxy. Their names
// change on every refresh and must not consume persistent listener ports.
func separateSubscriptionInfo(proxies []map[string]any) ([]map[string]any, []string) {
	nodes := []map[string]any{}
	info := []string{}
	for _, proxy := range proxies {
		name, _ := proxy["name"].(string)
		if strings.HasPrefix(name, "剩余流量") || strings.HasPrefix(name, "距离下次重置剩余") || strings.HasPrefix(name, "套餐到期") {
			info = append(info, name)
		} else {
			nodes = append(nodes, proxy)
		}
	}
	return nodes, info
}

func parseProxies(data []byte) ([]map[string]any, error) {
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("无法解析为 Clash YAML 配置，请检查订阅格式")
	}
	if len(doc.Proxies) == 0 {
		return nil, fmt.Errorf("未找到 proxies 节点；请选择 Clash / Mihomo 格式的订阅")
	}
	if len(doc.Proxies) > 2000 {
		return nil, fmt.Errorf("单次最多导入 2000 个节点")
	}
	seen := map[string]bool{}
	for _, p := range doc.Proxies {
		name, _ := p["name"].(string)
		typ, _ := p["type"].(string)
		server, _ := p["server"].(string)
		if strings.TrimSpace(name) == "" || typ == "" || server == "" {
			return nil, fmt.Errorf("每个节点都须包含 name、type 和 server")
		}
		if seen[name] {
			return nil, fmt.Errorf("订阅中存在重名节点，请先在来源处修正")
		}
		seen[name] = true
		if _, exists := p["dialer-proxy"]; exists {
			return nil, fmt.Errorf("暂不支持包含 dialer-proxy 的链式节点，请使用独立节点")
		}
	}
	return doc.Proxies, nil
}

func allocatePort(st *State) (int, error) {
	used := map[int]bool{}
	if u, err := url.Parse(st.Settings.ControllerURL); err == nil {
		port, _ := strconv.Atoi(u.Port())
		used[port] = true
	}
	for _, n := range st.Nodes {
		used[n.Port] = true
	}
	for p := st.Settings.PortStart; p <= st.Settings.PortEnd; p++ {
		if !used[p] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("端口范围已用尽，请在运行配置中扩大范围")
}

func reconcile(st *State, subID string, proxies []map[string]any) error {
	existing := map[string]int{}
	for i := range st.Nodes {
		if st.Nodes[i].SubscriptionID == subID {
			existing[st.Nodes[i].SourceName] = i
			st.Nodes[i].Available = false
		}
	}
	for _, p := range proxies {
		name := p["name"].(string)
		if i, ok := existing[name]; ok {
			n := &st.Nodes[i]
			n.Config = p
			n.Type = fmt.Sprint(p["type"])
			n.Server = fmt.Sprint(p["server"])
			n.Available = true
			n.Latency = nil
			n.TestedAt = nil
			n.TestError = ""
		} else {
			port, err := allocatePort(st)
			if err != nil {
				return err
			}
			st.Nodes = append(st.Nodes, Node{ID: randomID(), SubscriptionID: subID, SourceName: name, Name: name, Type: fmt.Sprint(p["type"]), Server: fmt.Sprint(p["server"]), Port: port, Enabled: true, Available: true, Config: p})
		}
	}
	sort.Slice(st.Nodes, func(i, j int) bool { return st.Nodes[i].Port < st.Nodes[j].Port })
	st.Revision++
	return nil
}

func fetchSubscription(ctx context.Context, rawURL string) ([]map[string]any, error) {
	if !validHTTP(rawURL) {
		return nil, fmt.Errorf("请提供有效的 HTTP / HTTPS 订阅地址")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("订阅地址无效")
	}
	req.Header.Set("User-Agent", "clash.meta/PortPilot")
	req.Header.Set("Accept", "application/yaml, text/yaml, text/plain, */*")
	client := &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !validHTTP(req.URL.String()) {
			return fmt.Errorf("redirect rejected")
		}
		return nil
	}}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法获取订阅，请检查地址及服务器网络")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("订阅服务器返回 HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxSubscriptionSize+1))
	if err != nil {
		return nil, fmt.Errorf("读取订阅失败")
	}
	if len(data) > maxSubscriptionSize {
		return nil, fmt.Errorf("订阅内容超过 5 MiB 限制")
	}
	return parseProxies(data)
}

// Synchronizer serializes refreshes so background and manual requests cannot overwrite one another.
type Synchronizer struct {
	store *Store
	mu    sync.Mutex
}

func (s *Synchronizer) Sync(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sub *Subscription
	st := s.store.Snapshot()
	for i := range st.Subscriptions {
		if st.Subscriptions[i].ID == id {
			sub = &st.Subscriptions[i]
			break
		}
	}
	if sub == nil {
		return fmt.Errorf("订阅不存在")
	}
	proxies, fetchErr := fetchSubscription(ctx, sub.URL)
	proxies, info := separateSubscriptionInfo(proxies)
	if fetchErr == nil && len(proxies) == 0 {
		fetchErr = fmt.Errorf("订阅中没有实际代理节点")
	}
	updateErr := s.store.Update(func(state *State) error {
		for i := range state.Subscriptions {
			current := &state.Subscriptions[i]
			if current.ID != id {
				continue
			}
			if current.URL != sub.URL {
				return fmt.Errorf("订阅地址已变更，请重新同步")
			}
			attempted := time.Now().UTC()
			current.AttemptedAt = &attempted
			if fetchErr != nil {
				current.Error = fetchErr.Error()
				state.event("error", "订阅同步失败："+current.Name)
				return nil
			}
			if err := reconcile(state, id, proxies); err != nil {
				return err
			}
			now := time.Now().UTC()
			current.UpdatedAt = &now
			current.Error = ""
			current.NodeCount = len(proxies)
			current.Info = info
			state.event("success", fmt.Sprintf("订阅 %s 同步完成，获取 %d 个节点", current.Name, len(proxies)))
			return nil
		}
		return fmt.Errorf("订阅已被删除")
	})
	if updateErr != nil {
		return updateErr
	}
	return fetchErr
}

func (s *Synchronizer) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, sub := range s.store.Snapshot().Subscriptions {
				last := sub.AttemptedAt
				if last == nil {
					last = sub.UpdatedAt
				}
				if sub.Interval > 0 && (last == nil || time.Since(*last) >= time.Duration(sub.Interval)*time.Hour) {
					_ = s.Sync(ctx, sub.ID)
				}
			}
		}
	}
}
