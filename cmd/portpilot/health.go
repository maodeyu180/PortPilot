package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

func checkHealth(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("无效的面板监听地址")
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get("http://" + net.JoinHostPort(host, port) + "/api/session")
	if err != nil {
		return fmt.Errorf("管理面板尚未就绪")
	}
	defer res.Body.Close()
	var session struct {
		Authenticated *bool `json:"authenticated"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&session) != nil || session.Authenticated == nil {
		return fmt.Errorf("管理面板健康检查失败")
	}
	return nil
}
