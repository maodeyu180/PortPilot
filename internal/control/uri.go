package control

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

type uriParams map[string]string

func paramKey(key string) string {
	return strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(key))
}

func parseURIParams(raw string) (uriParams, error) {
	params := uriParams{}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		key, value, _ := strings.Cut(pair, "=")
		key, keyErr := url.PathUnescape(key)
		value, valueErr := url.PathUnescape(value)
		if keyErr != nil || valueErr != nil {
			return nil, fmt.Errorf("分享链接参数编码无效")
		}
		// '+' in a URI password, key or path is literal, not a form-encoded space.
		params[paramKey(key)] = value
	}
	return params, nil
}

func (q uriParams) get(keys ...string) string {
	for _, key := range keys {
		if value := q[paramKey(key)]; value != "" {
			return value
		}
	}
	return ""
}

func (q uriParams) copy(p map[string]any, field string, aliases ...string) {
	if value := q.get(append([]string{field}, aliases...)...); value != "" {
		p[field] = value
	}
}

func (q uriParams) flag(p map[string]any, field string, aliases ...string) error {
	for _, key := range append([]string{field}, aliases...) {
		if raw, exists := q[paramKey(key)]; exists {
			switch strings.ToLower(raw) {
			case "", "true", "1", "yes", "on":
				p[field] = true
			case "false", "0", "no", "off":
				p[field] = false
			default:
				return fmt.Errorf("%s 参数须为 true / false 或 1 / 0", field)
			}
			break
		}
	}
	return nil
}

func (q uriParams) options(p map[string]any, flags, integers []string) error {
	for _, field := range flags {
		if err := q.flag(p, field); err != nil {
			return err
		}
	}
	for _, field := range integers {
		if value, exists := q[paramKey(field)]; exists {
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return fmt.Errorf("%s 参数须为非负整数", field)
			}
			p[field] = n
		}
	}
	return nil
}

func parseShareURI(raw string) (map[string]any, error) {
	scheme, payload, ok := strings.Cut(raw, "://")
	if !ok {
		return nil, fmt.Errorf("须一行一个完整的节点分享链接")
	}
	scheme = strings.ToLower(scheme)
	switch scheme {
	case "vmess":
		return parseVMess(payload)
	case "ss":
		return parseShadowsocks(payload)
	case "ssr":
		return parseShadowsocksR(payload)
	case "vless", "trojan", "anytls", "hysteria2", "hy2", "hysteria", "hy", "tuic", "http", "https", "socks", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("暂不支持此分享链接协议，请改用 Mihomo YAML 配置")
	}
	defaultPort := 443
	if scheme == "http" {
		defaultPort = 80
	} else if strings.HasPrefix(scheme, "socks") {
		defaultPort = 1080
	} else if scheme == "vless" {
		defaultPort = 0
	}
	u, q, p, err := parseProxyURL(scheme+"://"+payload, defaultPort)
	if err != nil {
		return nil, err
	}
	password := userInfoText(u.User)
	switch scheme {
	case "vless":
		if u.User == nil || password == "" || strings.Contains(password, ":") {
			return nil, fmt.Errorf("VLESS 链接缺少有效的用户 ID")
		}
		p["uuid"] = password
		q.copy(p, "flow")
		q.copy(p, "packet-encoding")
		if encryption := q.get("encryption"); encryption != "" && encryption != "none" {
			p["encryption"] = encryption
		}
		if err = applyTransport(p, q); err == nil {
			err = applyTLS(p, q, "servername")
		}
	case "trojan":
		if password == "" {
			return nil, fmt.Errorf("Trojan 链接缺少密码")
		}
		p["password"] = password
		err = applyTransport(p, q)
		if err == nil {
			err = applyTLS(p, q, "sni")
		}
	case "anytls", "hy2", "hysteria2":
		if scheme == "anytls" && u.User != nil {
			if pass, hasPassword := u.User.Password(); hasPassword {
				password = pass
			}
		}
		if password == "" {
			return nil, fmt.Errorf("分享链接缺少认证密码")
		}
		p["password"] = password
		if err = applyTLS(p, q, "sni"); err != nil {
			break
		}
		if scheme == "anytls" {
			if q.get("security") == "reality" || q.get("pbk", "public-key") != "" {
				return nil, fmt.Errorf("Mihomo 不支持 AnyTLS + Reality")
			}
			err = q.options(p, nil, []string{"idle-session-check-interval", "idle-session-timeout", "min-idle-session"})
			q.copy(p, "client-metadata")
		} else {
			p["type"] = "hysteria2"
			q.copy(p, "ports", "mport")
			q.copy(p, "hop-interval")
			q.copy(p, "up", "upmbps")
			q.copy(p, "down", "downmbps")
			if obfs := q.get("obfs"); obfs != "" && obfs != "none" {
				p["obfs"] = obfs
				if q.get("obfs-password") == "" {
					return nil, fmt.Errorf("混淆已启用，但缺少 obfs-password")
				}
				q.copy(p, "obfs-password")
			}
		}
	case "hysteria", "hy":
		p["type"] = "hysteria"
		p["protocol"] = "udp"
		q.copy(p, "auth-str", "auth")
		q.copy(p, "protocol")
		q.copy(p, "ports", "mport")
		q.copy(p, "obfs", "obfsParam")
		q.copy(p, "up", "upmbps")
		q.copy(p, "down", "downmbps")
		err = applyTLS(p, q, "sni")
		if err == nil {
			err = q.options(p, []string{"fast-open", "disable-mtu-discovery"}, []string{"recv-window", "recv-window-conn"})
		}
	case "tuic":
		if u.User == nil {
			return nil, fmt.Errorf("TUIC 链接缺少认证信息")
		}
		pass, hasPassword := u.User.Password()
		if !hasPassword || u.User.Username() == "" || pass == "" {
			return nil, fmt.Errorf("请使用包含 UUID 和密码的 TUIC v5 分享链接")
		}
		p["uuid"], p["password"] = u.User.Username(), pass
		for _, field := range []string{"ip", "udp-relay-mode"} {
			q.copy(p, field)
		}
		q.copy(p, "congestion-controller", "congestion-control")
		err = applyTLS(p, q, "sni")
		if err == nil {
			err = q.options(p, []string{"disable-sni", "reduce-rtt", "fast-open"}, []string{"heartbeat-interval", "request-timeout", "max-udp-relay-packet-size", "max-open-streams"})
		}
	case "http", "https", "socks", "socks5", "socks5h":
		if u.Path != "" && u.Path != "/" {
			return nil, fmt.Errorf("HTTP / SOCKS 链接不能包含网页路径；订阅地址请使用「导入订阅」")
		}
		p["type"] = "http"
		if strings.HasPrefix(scheme, "socks") {
			p["type"] = "socks5"
		}
		if u.User != nil {
			p["username"] = u.User.Username()
			p["password"], _ = u.User.Password()
		}
		if scheme == "https" {
			p["tls"] = true
		}
		err = applyTLS(p, q, "sni")
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func parseProxyURL(raw string, defaultPort int) (*url.URL, uriParams, map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || strings.IndexFunc(u.Hostname(), unicode.IsSpace) >= 0 {
		return nil, nil, nil, fmt.Errorf("分享链接地址无效，请检查服务器和端口")
	}
	server := u.Hostname()
	if strings.Contains(server, ":") && net.ParseIP(server) == nil {
		return nil, nil, nil, fmt.Errorf("IPv6 服务器地址无效，请使用方括号包围地址")
	}
	port := defaultPort
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
	}
	if err != nil || port < 1 || port > 65535 || strings.HasSuffix(u.Host, ":") {
		return nil, nil, nil, fmt.Errorf("上游端口须为 1–65535")
	}
	q, err := parseURIParams(u.RawQuery)
	if err != nil {
		return nil, nil, nil, err
	}
	name := strings.TrimSpace(u.Fragment)
	if name == "" {
		name = strings.ToUpper(u.Scheme) + " " + net.JoinHostPort(server, strconv.Itoa(port))
	}
	p := map[string]any{"name": name, "type": strings.ToLower(u.Scheme), "server": server, "port": port, "udp": true}
	if err = q.flag(p, "udp"); err == nil {
		err = q.flag(p, "tfo", "fastopen")
	}
	return u, q, p, err
}

func userInfoText(user *url.Userinfo) string {
	if user == nil {
		return ""
	}
	if password, exists := user.Password(); exists {
		return user.Username() + ":" + password
	}
	return user.Username()
}

func applyTLS(p map[string]any, q uriParams, serverNameField string) error {
	security := strings.ToLower(q.get("security"))
	if security != "" {
		switch security {
		case "none":
			p["tls"] = false
		case "tls", "reality":
			p["tls"] = true
		default:
			return fmt.Errorf("暂不支持此 security 参数，请改用 Mihomo YAML 配置")
		}
	}
	if err := q.flag(p, "tls"); err != nil {
		return err
	}
	q.copy(p, serverNameField, "sni", "peer", "servername")
	q.copy(p, "client-fingerprint", "fp")
	q.copy(p, "fingerprint", "pinSHA256")
	q.copy(p, "ip-version")
	if alpn := q.get("alpn"); alpn != "" {
		p["alpn"] = splitNonempty(alpn, ",")
	}
	if security == "reality" || q.get("pbk", "public-key") != "" {
		key := q.get("pbk", "public-key")
		if key == "" {
			return fmt.Errorf("Reality 链接缺少公钥 pbk")
		}
		opts := map[string]any{"public-key": key}
		q.copy(opts, "short-id", "sid")
		p["reality-opts"] = opts
		p["tls"] = true
	}
	return q.flag(p, "skip-cert-verify", "allow-insecure", "insecure")
}

func applyTransport(p map[string]any, q uriParams) error {
	network := strings.ToLower(q.get("type", "network", "net"))
	if network == "" {
		network = "tcp"
	}
	if network == "tcp" && q.get("headerType") == "http" {
		network = "http"
	}
	host, path := q.get("host"), q.get("path")
	opts := map[string]any{}
	switch network {
	case "tcp":
		return nil
	case "ws", "websocket", "httpupgrade":
		if network == "httpupgrade" {
			opts["v2ray-http-upgrade"] = true
		}
		network = "ws"
		if host != "" {
			opts["headers"] = map[string]any{"Host": host}
		}
		if path != "" {
			opts["path"] = path
		}
		if err := q.options(opts, nil, []string{"max-early-data"}); err != nil {
			return err
		}
		q.copy(opts, "early-data-header-name")
	case "grpc":
		if service := q.get("serviceName", "grpc-service-name", "path"); service != "" {
			opts["grpc-service-name"] = service
		}
	case "h2":
		if host != "" {
			opts["host"] = splitNonempty(host, ",")
		}
		if path != "" {
			opts["path"] = path
		}
	case "http":
		if host != "" {
			opts["headers"] = map[string]any{"Host": splitNonempty(host, ",")}
		}
		if path != "" {
			opts["path"] = splitNonempty(path, ",")
		}
	default:
		return fmt.Errorf("暂不支持此传输方式，请改用 Mihomo YAML 配置")
	}
	p["network"] = network
	if len(opts) > 0 {
		p[network+"-opts"] = opts
	}
	return nil
}

func splitNonempty(value, separator string) []string {
	parts := []string{}
	for _, part := range strings.Split(value, separator) {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func parseVMess(payload string) (map[string]any, error) {
	decoded, err := decodeBase64Text(payload)
	if err != nil {
		return nil, fmt.Errorf("VMess 内容不是有效的 Base64 JSON")
	}
	var doc map[string]any
	decoder := json.NewDecoder(strings.NewReader(decoded))
	decoder.UseNumber()
	if decoder.Decode(&doc) != nil || doc == nil {
		return nil, fmt.Errorf("暂只支持 VMess 的 Base64 JSON 分享格式")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("VMess JSON 后包含无效内容")
	}
	text := func(key string) string {
		switch value := doc[key].(type) {
		case string:
			return value
		case json.Number:
			return string(value)
		case bool:
			return strconv.FormatBool(value)
		}
		return ""
	}
	port, portErr := strconv.Atoi(text("port"))
	server := strings.Trim(text("add"), "[]")
	if server == "" || text("id") == "" || portErr != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("VMess 配置须包含 add、有效 port 和 id")
	}
	if strings.IndexFunc(server, unicode.IsSpace) >= 0 || strings.ContainsAny(server, "/@?#") || strings.Contains(server, ":") && net.ParseIP(server) == nil {
		return nil, fmt.Errorf("VMess 服务器地址无效")
	}
	p := map[string]any{"type": "vmess", "server": server, "port": port, "uuid": text("id"), "name": text("ps"), "cipher": "auto", "alterId": 0, "udp": true}
	if strings.TrimSpace(text("ps")) == "" {
		p["name"] = "VMESS " + net.JoinHostPort(server, text("port"))
	}
	if cipher := text("scy"); cipher != "" {
		p["cipher"] = cipher
	}
	q := uriParams{}
	for key := range doc {
		q[paramKey(key)] = text(key)
	}
	// VMess JSON uses 'type' for TCP header camouflage, and 'net' for transport.
	q[paramKey("headerType")] = text("type")
	q["type"] = text("net")
	if q["type"] == "" {
		q["type"] = "tcp"
	}
	q["alterid"] = text("aid")
	if q["alterid"] == "" {
		q["alterid"] = "0"
	}
	if err = q.options(p, nil, []string{"alterId"}); err != nil {
		return nil, err
	}
	if text("tls") == "tls" {
		q["tls"] = "true"
	}
	if text("tls") == "none" {
		q["tls"] = "false"
	}
	if text("tls") == "" {
		q["tls"] = "false"
	}
	delete(q, "security") // 'security' in older VMess JSON denotes a cipher.
	if err = applyTransport(p, q); err == nil {
		err = applyTLS(p, q, "servername")
	}
	if err != nil {
		return nil, err
	}
	if value := text("verify_cert"); value != "" {
		check := map[string]any{}
		if err = (uriParams{"verifycert": value}).flag(check, "verify-cert"); err != nil {
			return nil, err
		}
		p["skip-cert-verify"] = !check["verify-cert"].(bool)
	}
	return p, nil
}

func parseShadowsocks(payload string) (map[string]any, error) {
	// SIP002 encodes userinfo only; legacy links encode the full authority.
	authority, suffix, _ := strings.Cut(payload, "#")
	fragment := ""
	if suffix != "" {
		fragment = "#" + suffix
	}
	authority, query, hasQuery := strings.Cut(authority, "?")
	querySuffix := ""
	if hasQuery {
		querySuffix = "?" + query
	}
	authority = strings.TrimSuffix(authority, "/")
	if !strings.Contains(authority, "@") {
		var err error
		authority, err = decodeBase64Text(authority)
		if err != nil {
			return nil, fmt.Errorf("Shadowsocks 的 Base64 内容无效")
		}
	}
	at := strings.LastIndex(authority, "@")
	if at < 1 {
		return nil, fmt.Errorf("Shadowsocks 链接缺少认证或服务器地址")
	}
	auth, err := url.PathUnescape(authority[:at])
	if err != nil {
		return nil, fmt.Errorf("Shadowsocks 认证编码无效")
	}
	if !strings.Contains(auth, ":") {
		auth, err = decodeBase64Text(auth)
		if err != nil {
			return nil, fmt.Errorf("Shadowsocks 认证的 Base64 内容无效")
		}
	}
	cipher, password, ok := strings.Cut(auth, ":")
	if !ok || cipher == "" || password == "" {
		return nil, fmt.Errorf("Shadowsocks 链接缺少加密方式或密码")
	}
	_, q, p, err := parseProxyURL("ss://"+authority[at+1:]+querySuffix+fragment, 0)
	if err != nil {
		return nil, err
	}
	if cipher == "chacha20-poly1305" {
		cipher = "chacha20-ietf-poly1305"
	}
	p["cipher"], p["password"] = cipher, password
	if err := q.flag(p, "udp-over-tcp", "uot"); err != nil {
		return nil, err
	}
	if plugin := q.get("plugin"); plugin != "" {
		parts := strings.Split(plugin, ";")
		opts := map[string]any{}
		for _, part := range parts[1:] {
			key, value, hasValue := strings.Cut(part, "=")
			if key == "tls" || key == "skip-cert-verify" || key == "mux" {
				if !hasValue {
					value = "true"
				}
				if err := (uriParams{paramKey(key): value}).flag(opts, key); err != nil {
					return nil, err
				}
			} else if hasValue {
				opts[key] = value
			}
		}
		switch parts[0] {
		case "obfs-local", "simple-obfs", "obfs":
			if opts["obfs"] != "http" && opts["obfs"] != "tls" {
				return nil, fmt.Errorf("Shadowsocks obfs 插件须指定 obfs=http 或 obfs=tls")
			}
			p["plugin"] = "obfs"
			obfs := map[string]any{"mode": opts["obfs"]}
			if host, ok := opts["obfs-host"]; ok {
				obfs["host"] = host
			}
			p["plugin-opts"] = obfs
		case "v2ray-plugin":
			p["plugin"] = "v2ray-plugin"
			if opts["mode"] == nil {
				opts["mode"] = "websocket"
			}
			p["plugin-opts"] = opts
		default:
			return nil, fmt.Errorf("暂不支持此 Shadowsocks 插件，请改用 Mihomo YAML 配置")
		}
	}
	return p, nil
}

func parseShadowsocksR(payload string) (map[string]any, error) {
	decoded, err := decodeBase64Text(payload)
	if err != nil {
		return nil, fmt.Errorf("ShadowsocksR 的 Base64 内容无效")
	}
	authority, query, _ := strings.Cut(decoded, "/?")
	// Split from the right: an IPv6 address itself contains colons.
	parts := make([]string, 6)
	for i := 5; i > 0; i-- {
		pos := strings.LastIndex(authority, ":")
		if pos < 0 {
			return nil, fmt.Errorf("ShadowsocksR 链接字段不完整")
		}
		parts[i], authority = authority[pos+1:], authority[:pos]
	}
	parts[0] = strings.Trim(authority, "[]")
	password, err := decodeBase64Text(parts[5])
	if err != nil {
		return nil, fmt.Errorf("ShadowsocksR 密码编码无效")
	}
	_, q, p, err := parseProxyURL("ssr://"+net.JoinHostPort(parts[0], parts[1])+"?"+query, 0)
	if err != nil {
		return nil, err
	}
	if parts[2] == "" || parts[3] == "" || parts[4] == "" {
		return nil, fmt.Errorf("ShadowsocksR 链接缺少协议、加密或混淆设置")
	}
	p["protocol"], p["cipher"], p["obfs"], p["password"] = parts[2], parts[3], parts[4], password
	for key, field := range map[string]string{"remarks": "name", "protoparam": "protocol-param", "obfsparam": "obfs-param"} {
		if value := q.get(key); value != "" {
			text, err := decodeBase64Text(value)
			if err != nil {
				return nil, fmt.Errorf("ShadowsocksR 的 %s 编码无效", key)
			}
			p[field] = text
		}
	}
	return p, nil
}
