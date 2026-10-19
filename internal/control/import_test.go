package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const importUUID = "12345678-1234-1234-1234-123456789abc"

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func TestImportConfigurationShapesAndBase64(t *testing.T) {
	single := "name: Example\ntype: http\nserver: proxy.example\nport: 8080\n"
	links := "# sample nodes\n\nvless://" + importUUID + "@vless.example:443?security=tls#VLESS\r\nanytls://secret@anytls.example:8443#AnyTLS"
	for name, input := range map[string]string{
		"yaml config": fixture,
		"yaml single": single,
		"yaml list":   "- {name: Example, type: http, server: proxy.example, port: 8080}",
		"json single": `{"name":"Example","type":"http","server":"proxy.example","port":8080}`,
		"json config": `{"proxies":[{"name":"Example","type":"http","server":"proxy.example","port":8080}]}`,
		"json list":   `[{"name":"Example","type":"http","server":"proxy.example","port":8080}]`,
		"uris":        links,
		"bom":         "\uFEFF" + links,
		"base64 yaml": b64(fixture),
		"base64 BOM":  b64("\uFEFF" + links),
		"base64 uris": base64.StdEncoding.EncodeToString([]byte(links)),
		"url base64":  b64(links),
		"wrapped base64": func() string {
			encoded := base64.StdEncoding.EncodeToString([]byte(links))
			return encoded[:10] + "\r\n\t" + encoded[10:35] + " \n" + encoded[35:]
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			proxies, err := parseProxies([]byte(input))
			if err != nil || len(proxies) == 0 {
				t.Fatalf("parse: %v", err)
			}
			canonical, err := marshalImportedProxies(proxies)
			if err != nil {
				t.Fatal(err)
			}
			roundtrip, err := parseProxies(canonical)
			if err != nil || len(roundtrip) != len(proxies) {
				t.Fatalf("canonical YAML did not roundtrip: %v", err)
			}
		})
	}
	proxies, err := parseProxies([]byte(b64(links)))
	if err != nil || len(proxies) != 2 || proxies[0]["name"] != "VLESS" || proxies[1]["name"] != "AnyTLS" {
		t.Fatalf("base64 subscription lost nodes: %v", err)
	}
}

func TestImportShareProtocolOptions(t *testing.T) {
	tests := []struct {
		name, uri string
		want      map[string]any
	}{
		{"vless reality IPv6", "VLESS://" + importUUID + "@[2001:db8::1]:443?security=reality&pbk=public+key&sid=abcd&fp=chrome&flow=xtls-rprx-vision&sni=example.com&alpn=h2,http%2F1.1#%E9%A6%99%E6%B8%AF%20A", map[string]any{
			"name": "香港 A", "type": "vless", "server": "2001:db8::1", "port": 443, "uuid": importUUID, "tls": true, "servername": "example.com", "client-fingerprint": "chrome", "flow": "xtls-rprx-vision", "reality-opts": map[string]any{"public-key": "public+key", "short-id": "abcd"}, "alpn": []string{"h2", "http/1.1"},
		}},
		{"vless websocket", "vless://" + importUUID + "@v.example:8443?security=tls&type=ws&host=cdn.example&path=%2Fws%3Fed%3D2048&allowInsecure=0#WS", map[string]any{
			"network": "ws", "ws-opts": map[string]any{"path": "/ws?ed=2048", "headers": map[string]any{"Host": "cdn.example"}}, "skip-cert-verify": false,
		}},
		{"vless grpc", "vless://" + importUUID + "@v.example:443?type=grpc&serviceName=my%2Bservice&security=tls#GRPC", map[string]any{
			"grpc-opts": map[string]any{"grpc-service-name": "my+service"},
		}},
		{"http upgrade", "vless://" + importUUID + "@v.example:443?type=httpupgrade&path=%2Fupgrade#Upgrade", map[string]any{
			"network": "ws", "ws-opts": map[string]any{"path": "/upgrade", "v2ray-http-upgrade": true},
		}},
		{"vmess JSON", "vmess://" + b64(`{"v":"2","ps":"VMess","add":"vm.example","port":"443","id":"`+importUUID+`","aid":"0","scy":"auto","net":"ws","type":"none","host":"cdn.example","path":"/websocket","tls":"tls","sni":"vm.example","verify_cert":true}`), map[string]any{
			"name": "VMess", "type": "vmess", "port": 443, "alterId": 0, "tls": true, "skip-cert-verify": false, "network": "ws", "ws-opts": map[string]any{"path": "/websocket", "headers": map[string]any{"Host": "cdn.example"}},
		}},
		{"vmess plain numeric", "vmess://" + b64(`{"ps":"VMess plain","add":"vm.example","port":8080,"id":"`+importUUID+`","aid":0,"net":"tcp","tls":""}`), map[string]any{
			"port": 8080, "tls": false, "alterId": 0,
		}},
		{"vmess IPv6", "vmess://" + b64(`{"add":"[2001:db8::1]","port":443,"id":"`+importUUID+`","tls":true}`), map[string]any{
			"server": "2001:db8::1", "tls": true,
		}},
		{"ss SIP002", "ss://" + b64("aes-128-gcm:p@ss:word+中文") + "@ss.example:8388/?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dcdn.example#SS", map[string]any{
			"type": "ss", "password": "p@ss:word+中文", "cipher": "aes-128-gcm", "plugin": "obfs", "plugin-opts": map[string]any{"mode": "http", "host": "cdn.example"},
		}},
		{"ss legacy IPv6", "ss://" + b64("chacha20-poly1305:password@[2001:db8::2]:8388") + "#SS%20IPv6", map[string]any{
			"server": "2001:db8::2", "port": 8388, "cipher": "chacha20-ietf-poly1305", "password": "password",
		}},
		{"ss percent auth", "ss://aes-128-gcm:p%40ss%3Aword@ss.example:8388#Percent", map[string]any{
			"password": "p@ss:word",
		}},
		{"ss v2ray plugin", "ss://" + b64("aes-128-gcm:password") + "@ss.example:8388?plugin=v2ray-plugin%3Btls%3Bhost%3Dcdn.example%3Bpath%3D%2Fws#Plugin", map[string]any{
			"plugin": "v2ray-plugin", "plugin-opts": map[string]any{"mode": "websocket", "tls": true, "host": "cdn.example", "path": "/ws"},
		}},
		{"ssr", "ssr://" + b64("[2001:db8::3]:8388:auth_aes128_md5:aes-128-cfb:tls1.2_ticket_auth:"+b64("p@ssword")+"/?remarks="+b64("SSR 节点")+"&protoparam="+b64("42:secret")+"&obfsparam="+b64("cdn.example")), map[string]any{
			"type": "ssr", "name": "SSR 节点", "server": "2001:db8::3", "password": "p@ssword", "protocol": "auth_aes128_md5", "cipher": "aes-128-cfb", "obfs": "tls1.2_ticket_auth", "protocol-param": "42:secret", "obfs-param": "cdn.example",
		}},
		{"trojan", "trojan://p%40ss%3Aword@trojan.example:443?type=grpc&serviceName=trojan&peer=sni.example&allowInsecure=false#Trojan", map[string]any{
			"type": "trojan", "password": "p@ss:word", "sni": "sni.example", "skip-cert-verify": false, "grpc-opts": map[string]any{"grpc-service-name": "trojan"},
		}},
		{"anytls", "anytls://ignored:p%40ss%2Bword@any.example?sni=sni.example&idle_session_timeout=45&min_idle_session=0&insecure=1#AnyTLS", map[string]any{
			"type": "anytls", "password": "p@ss+word", "port": 443, "sni": "sni.example", "idle-session-timeout": 45, "min-idle-session": 0, "skip-cert-verify": true,
		}},
		{"hy2", "hy2://user:pass@hy.example:443?obfs=salamander&obfs-password=obfs%2Bsecret&mport=443,8443-8450&sni=sni.example&pinSHA256=fingerprint&insecure=false#HY2", map[string]any{
			"type": "hysteria2", "password": "user:pass", "obfs": "salamander", "obfs-password": "obfs+secret", "ports": "443,8443-8450", "sni": "sni.example", "fingerprint": "fingerprint", "skip-cert-verify": false,
		}},
		{"hysteria", "hysteria://hy.example:443?auth=secret&upmbps=20&downmbps=100&peer=sni.example&obfsParam=obfs#HY", map[string]any{
			"type": "hysteria", "auth-str": "secret", "up": "20", "down": "100", "sni": "sni.example", "obfs": "obfs", "protocol": "udp",
		}},
		{"tuic", "tuic://" + importUUID + ":password@tuic.example:443?congestion_control=bbr&congestion-controller=cubic&udp_relay_mode=native&reduce_rtt=true&alpn=h3#TUIC", map[string]any{
			"type": "tuic", "uuid": importUUID, "password": "password", "congestion-controller": "cubic", "udp-relay-mode": "native", "reduce-rtt": true, "alpn": []string{"h3"},
		}},
		{"tuic congestion alias", "tuic://" + importUUID + ":password@tuic.example:443?congestion_control=bbr#TUIC-alias", map[string]any{
			"congestion-controller": "bbr",
		}},
		{"http", "http://user:p%40ss%2Bword@http.example:8080#HTTP", map[string]any{
			"type": "http", "username": "user", "password": "p@ss+word", "port": 8080,
		}},
		{"https", "https://user:password@http.example#HTTPS", map[string]any{
			"type": "http", "tls": true, "port": 443,
		}},
		{"socks5h", "socks5h://user:password@[2001:db8::4]#SOCKS", map[string]any{
			"type": "socks5", "server": "2001:db8::4", "port": 1080, "username": "user", "password": "password",
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proxies, err := parseProxies([]byte(test.uri))
			if err != nil || len(proxies) != 1 {
				t.Fatalf("parse: %v", err)
			}
			for key, want := range test.want {
				if got := proxies[0][key]; !reflect.DeepEqual(got, want) {
					t.Errorf("%s = %#v; want %#v", key, got, want)
				}
			}
		})
	}
}

func TestImportRejectsMalformedInputWithoutLeakingCredentials(t *testing.T) {
	secret := "secret-should-not-leak"
	for _, input := range []string{
		"", "not base64 !", b64("not a configuration"), "proxies: {}", "name: One\ntype: http\nserver: example\n---\nproxies: []",
		"unsupported://" + secret + "@example:443",
		"vless://" + secret + "@example:0", "vless://" + secret + "@example:65536", "vless://" + secret + "@example:443?security=reality",
		"vless://" + secret + "@example:443?type=quic", "vless://" + secret + "@example:443?insecure=maybe", "vless://" + secret + "@example:443?security=xtls",
		"vless://" + secret + "@example:443?path=%ZZ", "vless://@example:443", "vless://" + secret + "@[bad:ipv6]:443",
		"vmess://invalid!", "vmess://" + b64(`{"add":"example","id":"`+secret+`","port":-1}`),
		"vmess://" + b64(`{"add":"example","id":"`+secret+`","port":443,"aid":"invalid"}`),
		"vmess://" + b64(`{"add":"example","id":"`+secret+`","port":443} trailing`),
		"vmess://" + b64(`{"add":"bad host","id":"`+secret+`","port":443}`),
		"ss://" + b64("aes-128-gcm:"+secret) + "@example:8388?plugin=unknown-plugin",
		"ss://" + b64("aes-128-gcm:"+secret) + "@example:8388?plugin=obfs-local",
		"ssr://" + b64("example:8388:origin:aes-128-cfb:plain:invalid!"),
		"anytls://" + secret + "@example?security=reality&pbk=public-key", "anytls://" + secret + "@example?min-idle-session=-1",
		"hy2://" + secret + "@example?obfs=salamander", "tuic://" + secret + "@example:443",
		"http://example/subscribe?token=" + secret,
		"anytls://" + secret + "@example:443#Duplicate\nanytls://" + secret + "@example:8443#Duplicate",
		"# comment\nanytls://" + secret + "@example:443\nnot-a-link",
	} {
		_, err := parseProxies([]byte(input))
		if err == nil {
			t.Errorf("malformed input was accepted: %q", input)
		} else if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaked credentials: %v", err)
		}
	}
	_, err := parseProxies([]byte("anytls://password@example:443#First\n\n# comment\nvless://bad@example:70000"))
	if err == nil || !strings.Contains(err.Error(), "第 4 行") {
		t.Fatalf("missing source line number: %v", err)
	}
	for _, input := range []string{strings.Repeat("x", maxSubscriptionSize+1), strings.Repeat("anytls://password@example:443\n", maxImportNodes+1)} {
		if _, err := parseProxies([]byte(input)); err == nil {
			t.Fatal("oversized import accepted")
		}
	}
}

func TestConversionAndBatchImportAreAtomic(t *testing.T) {
	a, h := authenticatedTestHandler(t)
	content := "anytls://password@a.example:443#A\nvless://" + importUUID + "@b.example:443?security=tls#B"
	w := request(h, "POST", "/api/nodes/convert", map[string]any{"content": b64(content)})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var converted struct {
		YAML  string   `json:"yaml"`
		Count int      `json:"count"`
		Types []string `json:"types"`
	}
	if json.Unmarshal(w.Body.Bytes(), &converted) != nil || converted.Count != 2 || !reflect.DeepEqual(converted.Types, []string{"anytls", "vless"}) {
		t.Fatal("invalid conversion result")
	}
	if len(a.store.Snapshot().Nodes) != 0 {
		t.Fatal("conversion changed stored nodes")
	}
	for _, input := range []string{content + "\nbroken-link", "anytls://password@a.example:443#A\nanytls://password@b.example:443#A"} {
		w = request(h, "POST", "/api/nodes", map[string]any{"content": input})
		if w.Code != 400 || len(a.store.Snapshot().Nodes) != 0 {
			t.Fatal("invalid batch was partially saved")
		}
	}
	if err := a.store.Update(func(st *State) error { st.Settings.PortEnd = st.Settings.PortStart; return nil }); err != nil {
		t.Fatal(err)
	}
	before := a.store.Snapshot()
	w = request(h, "POST", "/api/nodes", map[string]any{"content": converted.YAML})
	if w.Code != 400 || len(a.store.Snapshot().Nodes) != 0 || a.store.Snapshot().Revision != before.Revision {
		t.Fatal("port exhaustion did not roll back batch")
	}
	if err := a.store.Update(func(st *State) error { st.Settings.PortEnd += 5; return nil }); err != nil {
		t.Fatal(err)
	}
	w = request(h, "POST", "/api/nodes", map[string]any{"content": converted.YAML, "port": before.Settings.PortStart + 1})
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	st := a.store.Snapshot()
	if len(st.Nodes) != 2 || st.Nodes[0].Port != before.Settings.PortStart+1 || st.Nodes[1].Port != before.Settings.PortStart+2 || st.Nodes[0].SubscriptionID != "" {
		t.Fatal("batch did not use consecutive independent ports")
	}
	// The first requested port is free, but the second one is occupied.
	w = request(h, "POST", "/api/nodes", map[string]any{"content": content, "port": before.Settings.PortStart})
	if w.Code != 400 || len(a.store.Snapshot().Nodes) != 2 || a.store.Snapshot().Revision != st.Revision {
		t.Fatal("collision did not roll back the entire batch")
	}
	w = request(h, "POST", "/api/nodes", map[string]any{"yaml": "{name: Legacy, type: http, server: example, port: 8080}"})
	if w.Code != 201 || a.store.Snapshot().Nodes[2].Port != before.Settings.PortStart {
		t.Fatal("legacy API or automatic allocation regressed")
	}
	_, protected := testHandler(t, "long-admin-password")
	if w = request(protected, "POST", "/api/nodes/convert", map[string]string{"content": content}); w.Code != 401 {
		t.Fatal("converter must require authentication")
	}
}

func TestBase64SubscriptionRefreshPreservesPorts(t *testing.T) {
	content := "anytls://password@a.example:443#A\nvless://" + importUUID + "@b.example:443#B"
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, b64(content)) }))
	defer remote.Close()
	a, h := authenticatedTestHandler(t)
	w := request(h, "POST", "/api/subscriptions", map[string]any{"name": "URI subscription", "url": remote.URL, "interval": 24})
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	before := a.store.Snapshot()
	content = "vless://" + importUUID + "@updated.example:8443#B\nanytls://new-password@a.example:443#A"
	if err := a.syncer.Sync(context.Background(), before.Subscriptions[0].ID); err != nil {
		t.Fatal(err)
	}
	after := a.store.Snapshot()
	if len(after.Nodes) != 2 || after.Nodes[0].Port != before.Nodes[0].Port || after.Nodes[1].ID != before.Nodes[1].ID || after.Nodes[1].Config["server"] != "updated.example" {
		t.Fatal("refresh lost fixed mapping or updated upstream")
	}
	w = request(h, "POST", "/api/subscriptions", map[string]any{"name": "Pasted Base64", "content": b64(content), "interval": 0})
	if w.Code != 201 || len(a.store.Snapshot().Nodes) != 4 {
		t.Fatal("pasted Base64 subscription failed")
	}
	// Configuration conversion must retain nested security options in YAML.
	var config map[string]any
	data, err := marshalImportedProxies([]map[string]any{after.Nodes[0].Config})
	if err != nil || yaml.Unmarshal(data, &config) != nil || config["password"] != "new-password" {
		t.Fatal("converted YAML lost upstream credentials")
	}
}

func TestImportMihomoCompatibility(t *testing.T) {
	binary := os.Getenv("PORTPILOT_MIHOMO_TEST_BINARY")
	if binary == "" {
		t.Skip("set PORTPILOT_MIHOMO_TEST_BINARY to validate with a local Mihomo binary; Docker CI also covers this")
	}
	data, err := os.ReadFile("../../tests/fixtures/import-uris.txt")
	if err != nil {
		t.Fatal(err)
	}
	proxies, err := parseProxies(data)
	if err != nil {
		t.Fatal(err)
	}
	state := initialState()
	state.Settings.ListenHost = "127.0.0.1"
	if err = reconcile(&state, "test-fixtures", proxies); err != nil {
		t.Fatal(err)
	}
	config, err := GenerateConfig(state)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "mihomo.yaml")
	if err = os.WriteFile(path, config, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(binary, "-t", "-d", dir, "-f", path).CombinedOutput()
	if err != nil {
		t.Fatalf("Mihomo rejected converted fixtures: %s", output)
	}
}
