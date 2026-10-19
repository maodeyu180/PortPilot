package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

type CoreStatus struct {
	Connected   bool   `json:"connected"`
	Version     string `json:"version,omitempty"`
	Error       string `json:"error,omitempty"`
	Memory      uint64 `json:"memory"`
	Connections int    `json:"connections"`
	Upload      uint64 `json:"upload"`
	Download    uint64 `json:"download"`
}

func coreRequest(ctx context.Context, settings Settings, method, path string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(settings.ControllerURL, "/")+path, body)
	if err != nil {
		return nil, fmt.Errorf("控制器地址无效")
	}
	req.Header.Set("Authorization", "Bearer "+settings.ControllerSecret)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("未连接 Mihomo，请确认内核已启动且控制器地址、密钥一致")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("读取内核响应失败")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("Mihomo 返回 HTTP %d，请检查配置和控制器密钥", res.StatusCode)
	}
	return data, nil
}

func getCoreStatus(ctx context.Context, settings Settings) CoreStatus {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	data, err := coreRequest(ctx, settings, "GET", "/version", nil)
	if err != nil {
		return CoreStatus{Error: err.Error()}
	}
	var version struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &version) != nil || version.Version == "" {
		return CoreStatus{Error: "控制器响应无效"}
	}
	status := CoreStatus{Connected: true, Version: version.Version}
	if data, err = coreRequest(ctx, settings, "GET", "/connections", nil); err == nil {
		var metrics struct {
			Memory      uint64            `json:"memory"`
			Upload      uint64            `json:"uploadTotal"`
			Download    uint64            `json:"downloadTotal"`
			Connections []json.RawMessage `json:"connections"`
		}
		if json.Unmarshal(data, &metrics) == nil {
			status.Memory = metrics.Memory
			status.Upload = metrics.Upload
			status.Download = metrics.Download
			status.Connections = len(metrics.Connections)
		}
	}
	return status
}

func (a *App) apply(ctx context.Context) error {
	a.applyMu.Lock()
	defer a.applyMu.Unlock()
	st := a.store.Snapshot()
	config, err := GenerateConfig(st)
	if err != nil {
		return err
	}
	// payload avoids dependence on the core's SAFE_PATHS. Keep the accepted config
	// on disk too; point Mihomo's -f flag to this file for persistence across restarts.
	if _, err = coreRequest(ctx, st.Settings, "PUT", "/configs?force=true", map[string]string{"payload": string(config)}); err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(a.store.dir, "mihomo.yaml"), config); err != nil {
		return fmt.Errorf("内核已接受配置，但持久化失败，请重新应用")
	}
	now := time.Now().UTC()
	return a.store.Update(func(state *State) error {
		state.AppliedRevision = st.Revision
		state.AppliedAt = &now
		state.event("success", "运行配置已应用到 Mihomo")
		return nil
	})
}

func (a *App) testNodes(ctx context.Context, ids map[string]bool) (int, error) {
	a.testMu.Lock()
	defer a.testMu.Unlock()
	st := a.store.Snapshot()
	if !getCoreStatus(ctx, st.Settings).Connected {
		return 0, fmt.Errorf("请先启动 Mihomo 并应用当前配置，再测试节点")
	}
	if st.AppliedRevision != st.Revision {
		return 0, fmt.Errorf("存在未应用的配置，请先应用再测试")
	}
	tested := 0
	for _, n := range st.Nodes {
		if !n.Enabled || !n.Available || (len(ids) > 0 && !ids[n.ID]) {
			continue
		}
		if ctx.Err() != nil {
			return tested, ctx.Err()
		}
		path := "/proxies/" + url.PathEscape(configName(n)) + "/delay?" + url.Values{"url": {st.Settings.TestURL}, "timeout": {"5000"}}.Encode()
		data, testErr := coreRequest(ctx, st.Settings, "GET", path, nil)
		var result struct {
			Delay *int `json:"delay"`
		}
		if testErr == nil {
			if json.Unmarshal(data, &result) != nil || result.Delay == nil {
				testErr = fmt.Errorf("内核未返回有效延迟")
			}
		}
		now := time.Now().UTC()
		err := a.store.Update(func(state *State) error {
			if state.Revision != st.Revision {
				return fmt.Errorf("配置在测试期间已变更，请重新测试")
			}
			for i := range state.Nodes {
				if state.Nodes[i].ID == n.ID {
					state.Nodes[i].TestedAt = &now
					state.Nodes[i].Latency = result.Delay
					state.Nodes[i].TestError = ""
					if testErr != nil {
						state.Nodes[i].Latency = nil
						state.Nodes[i].TestError = "连接测试失败"
					}
				}
			}
			return nil
		})
		if err != nil {
			return tested, err
		}
		tested++
	}
	if err := a.store.Update(func(state *State) error {
		state.event("info", fmt.Sprintf("已完成 %d 个节点的连接测试", tested))
		return nil
	}); err != nil {
		return tested, err
	}
	return tested, nil
}
