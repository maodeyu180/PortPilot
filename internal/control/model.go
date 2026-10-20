package control

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

type Settings struct {
	PublicHost       string `json:"publicHost"`
	ListenHost       string `json:"listenHost"`
	PortStart        int    `json:"portStart"`
	PortEnd          int    `json:"portEnd"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	ControllerURL    string `json:"controllerUrl"`
	ControllerSecret string `json:"controllerSecret"`
	TestURL          string `json:"testUrl"`
	UDP              bool   `json:"udp"`
}

type Subscription struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	URL         string     `json:"url"`
	Interval    int        `json:"interval"`
	UpdatedAt   *time.Time `json:"updatedAt"`
	AttemptedAt *time.Time `json:"attemptedAt,omitempty"`
	Error       string     `json:"error,omitempty"`
	NodeCount   int        `json:"nodeCount"`
	Info        []string   `json:"info,omitempty"`
}

type Node struct {
	ID              string         `json:"id"`
	SubscriptionID  string         `json:"subscriptionId"`
	SourceName      string         `json:"sourceName"`
	Name            string         `json:"name"`
	Type            string         `json:"type"`
	Server          string         `json:"server"`
	Port            int            `json:"port"`
	Enabled         bool           `json:"enabled"`
	Available       bool           `json:"available"`
	Latency         *int           `json:"latency"`
	TestedAt        *time.Time     `json:"testedAt"`
	TestError       string         `json:"testError,omitempty"`
	ExitIP          string         `json:"exitIp,omitempty"`
	ExitIPCheckedAt *time.Time     `json:"exitIpCheckedAt,omitempty"`
	ExitIPError     string         `json:"exitIpError,omitempty"`
	Config          map[string]any `json:"config"`
}

type Event struct {
	ID      string    `json:"id"`
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"`
	Message string    `json:"message"`
}

type State struct {
	Version         int            `json:"version"`
	Revision        int            `json:"revision"`
	AppliedRevision int            `json:"appliedRevision"`
	AppliedAt       *time.Time     `json:"appliedAt"`
	Settings        Settings       `json:"settings"`
	Subscriptions   []Subscription `json:"subscriptions"`
	Nodes           []Node         `json:"nodes"`
	Events          []Event        `json:"events"`
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func initialState() State {
	return State{Version: 1, Revision: 1,
		Settings: Settings{PublicHost: "127.0.0.1", ListenHost: "0.0.0.0", PortStart: 20001, PortEnd: 20999,
			Username: "portpilot", Password: randomID()[:24], ControllerURL: "http://127.0.0.1:9090", ControllerSecret: randomID(), TestURL: "https://www.gstatic.com/generate_204"},
		Subscriptions: []Subscription{}, Nodes: []Node{}, Events: []Event{},
	}
}

func (s *State) event(kind, message string) {
	s.Events = append([]Event{{ID: randomID(), Time: time.Now().UTC(), Kind: kind, Message: message}}, s.Events...)
	if len(s.Events) > 100 {
		s.Events = s.Events[:100]
	}
}

func validHTTP(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

func validateSettings(s Settings) error {
	if s.PortStart < 1024 || s.PortEnd > 65535 || s.PortStart > s.PortEnd {
		return fmt.Errorf("端口范围必须在 1024–65535 内，且起始端口不大于结束端口")
	}
	if strings.TrimSpace(s.PublicHost) == "" || strings.ContainsAny(s.PublicHost, "/@?# \t\n\r") || (strings.Contains(s.PublicHost, ":") && net.ParseIP(s.PublicHost) == nil) {
		return fmt.Errorf("入口地址请填写 IP 或域名，不包含协议和端口")
	}
	if net.ParseIP(s.ListenHost) == nil {
		return fmt.Errorf("监听地址必须是有效的 IP")
	}
	if len(s.Username) < 1 || len(s.Password) < 8 || len(s.Username) > 255 || len(s.Password) > 255 || strings.ContainsAny(s.Username+s.Password, "\r\n\x00") {
		return fmt.Errorf("代理用户名不能为空，密码需为 8–255 位，用户名最长 255 字节")
	}
	u, err := url.Parse(s.ControllerURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") || u.Port() == "" {
		return fmt.Errorf("Mihomo 控制器须为带端口的本机 HTTP 地址，例如 http://127.0.0.1:9090")
	}
	if len(s.ControllerSecret) < 16 || strings.ContainsAny(s.ControllerSecret, "\r\n") {
		return fmt.Errorf("控制器密钥至少需要 16 位")
	}
	if !validHTTP(s.TestURL) {
		return fmt.Errorf("延迟测试地址必须是 HTTP 或 HTTPS 链接")
	}
	return nil
}

func configName(n Node) string { return "pp-" + n.ID }

func proxyURL(s Settings, n Node, protocol string) string {
	u := url.URL{Scheme: protocol, User: url.UserPassword(s.Username, s.Password), Host: net.JoinHostPort(s.PublicHost, fmt.Sprint(n.Port))}
	return u.String()
}
