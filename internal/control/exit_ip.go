package control

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"time"
)

const exitIPServiceURL = "https://api64.ipify.org?format=json"

func (n *Node) clearExitIP() {
	n.ExitIP, n.ExitIPError, n.ExitIPCheckedAt = "", "", nil
}

// Use the node's authenticated local mixed listener, never the panel's direct
// Internet connection or its public address (which may require NAT loopback).
func fetchExitIP(ctx context.Context, settings Settings, node Node, endpoint string) (string, error) {
	host := settings.ListenHost
	switch host {
	case "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	proxy := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(node.Port)), User: url.UserPassword(settings.Username, settings.Password)}
	transport := &http.Transport{
		Proxy:                 http.ProxyURL(proxy),
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("出口 IP 查询地址无效")
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("无法通过该节点查询出口 IP，请检查节点或稍后重试")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("出口 IP 查询返回 HTTP %d，请稍后重试", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	var result struct {
		IP string `json:"ip"`
	}
	if err != nil || len(body) > 4096 || json.Unmarshal(body, &result) != nil {
		return "", fmt.Errorf("出口 IP 查询响应无效")
	}
	address, err := netip.ParseAddr(result.IP)
	if err != nil || address.Zone() != "" || !address.IsGlobalUnicast() || address.IsPrivate() {
		return "", fmt.Errorf("查询服务未返回有效的公网 IP")
	}
	return address.Unmap().String(), nil
}

func (a *App) nodeExitIP(w http.ResponseWriter, r *http.Request) {
	st := a.store.Snapshot()
	var target *Node
	for i := range st.Nodes {
		if st.Nodes[i].ID == r.PathValue("id") {
			target = &st.Nodes[i]
			break
		}
	}
	if target == nil {
		fail(w, http.StatusNotFound, "节点不存在")
		return
	}
	if !target.Enabled || !target.Available {
		fail(w, http.StatusBadRequest, "请先启用来源有效的节点并应用配置")
		return
	}
	if st.Revision != st.AppliedRevision {
		fail(w, http.StatusBadRequest, "存在未应用的配置，请先应用再查询出口 IP")
		return
	}
	ip, lookupErr := fetchExitIP(r.Context(), st.Settings, *target, a.exitIPURL)
	now := time.Now().UTC()
	err := a.store.Update(func(state *State) error {
		if state.Revision != st.Revision || state.AppliedRevision != st.AppliedRevision {
			return fmt.Errorf("配置在查询期间已变更，请重新查询")
		}
		for i := range state.Nodes {
			node := &state.Nodes[i]
			if node.ID != target.ID {
				continue
			}
			node.ExitIP, node.ExitIPCheckedAt, node.ExitIPError = ip, &now, ""
			if lookupErr != nil {
				node.ExitIPError = lookupErr.Error()
			}
			return nil
		}
		return fmt.Errorf("节点已被删除，请刷新列表")
	})
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if lookupErr != nil {
		fail(w, http.StatusBadRequest, lookupErr.Error())
		return
	}
	respond(w, http.StatusOK, map[string]any{"ip": ip, "checkedAt": now})
}
