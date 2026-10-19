package control

import (
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const maxImportNodes = 2000

func parseProxies(data []byte) ([]map[string]any, error) {
	if len(data) > maxSubscriptionSize {
		return nil, fmt.Errorf("节点内容超过 5 MiB 限制")
	}
	proxies, err := parseProxyInput(strings.TrimPrefix(string(data), "\uFEFF"), 0)
	if err != nil {
		return nil, err
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("未找到代理节点")
	}
	if len(proxies) > maxImportNodes {
		return nil, fmt.Errorf("单次最多导入 %d 个节点", maxImportNodes)
	}
	seen := map[string]bool{}
	for i, p := range proxies {
		name, _ := p["name"].(string)
		typ, _ := p["type"].(string)
		server, _ := p["server"].(string)
		if strings.TrimSpace(name) == "" || strings.TrimSpace(typ) == "" || strings.TrimSpace(server) == "" {
			return nil, fmt.Errorf("第 %d 个节点须包含 name、type 和 server", i+1)
		}
		if seen[name] {
			return nil, fmt.Errorf("存在重名节点，请先修改节点名称")
		}
		seen[name] = true
		if _, exists := p["dialer-proxy"]; exists {
			return nil, fmt.Errorf("暂不支持包含 dialer-proxy 的链式节点，请使用独立节点")
		}
	}
	return proxies, nil
}

func parseProxyInput(content string, depth int) ([]map[string]any, error) {
	content = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(content), "\uFEFF"))
	if content == "" {
		return nil, fmt.Errorf("请粘贴节点分享链接、Base64 内容或 YAML / JSON 配置")
	}
	// A subscription is either structured configuration or one URI per line.
	// Check the first non-comment line so URI query strings never pass through YAML.
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if pos := strings.Index(line, "://"); pos > 0 && isURIScheme(line[:pos]) {
			return parseShareLines(content)
		}
		break
	}

	var doc yaml.Node
	decoder := yaml.NewDecoder(strings.NewReader(content))
	if err := decoder.Decode(&doc); err == nil && len(doc.Content) == 1 {
		root := doc.Content[0]
		if root.Kind == yaml.MappingNode || root.Kind == yaml.SequenceNode {
			var extra yaml.Node
			if decoder.Decode(&extra) != io.EOF {
				return nil, fmt.Errorf("请合并为一个 YAML 文档后导入")
			}
			var proxies []map[string]any
			if root.Kind == yaml.SequenceNode {
				if root.Decode(&proxies) != nil {
					return nil, fmt.Errorf("节点列表格式有误")
				}
				return proxies, nil
			}
			var config map[string]any
			if root.Decode(&config) != nil {
				return nil, fmt.Errorf("YAML / JSON 配置格式有误")
			}
			if _, ok := config["proxies"]; ok {
				var wrapper struct {
					Proxies []map[string]any `yaml:"proxies"`
				}
				if root.Decode(&wrapper) != nil {
					return nil, fmt.Errorf("proxies 必须是节点列表")
				}
				return wrapper.Proxies, nil
			}
			return []map[string]any{config}, nil
		}
	}
	if depth < 3 {
		if decoded, err := decodeBase64Text(content); err == nil {
			return parseProxyInput(decoded, depth+1)
		}
	}
	return nil, fmt.Errorf("无法识别内容，请使用节点分享链接、Base64 订阅或 Clash / Mihomo YAML / JSON")
}

func isURIScheme(s string) bool {
	for i, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (i == 0 || (c < '0' || c > '9') && c != '+' && c != '-' && c != '.') {
			return false
		}
	}
	return s != ""
}

func parseShareLines(content string) ([]map[string]any, error) {
	proxies := []map[string]any{}
	for i, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(proxies) >= maxImportNodes {
			return nil, fmt.Errorf("单次最多导入 %d 个节点", maxImportNodes)
		}
		p, err := parseShareURI(line)
		if err != nil {
			// Parser errors are deliberately independent of input: URIs contain secrets.
			return nil, fmt.Errorf("第 %d 行：%w", i+1, err)
		}
		proxies = append(proxies, p)
	}
	return proxies, nil
}

func decodeBase64Text(raw string) (string, error) {
	compact := strings.Map(func(c rune) rune {
		if unicode.IsSpace(c) {
			return -1
		}
		return c
	}, raw)
	compact = strings.NewReplacer("-", "+", "_", "/").Replace(compact)
	if len(compact) == 0 || len(compact)-len(strings.TrimRight(compact, "=")) > 2 {
		return "", fmt.Errorf("Base64 内容无效")
	}
	data, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(compact, "="))
	if err != nil || !utf8.Valid(data) || len(data) == 0 {
		return "", fmt.Errorf("Base64 内容无效")
	}
	for _, c := range string(data) {
		if unicode.IsControl(c) && c != '\n' && c != '\r' && c != '\t' {
			return "", fmt.Errorf("Base64 内容不是文本节点配置")
		}
	}
	return string(data), nil
}

func marshalImportedProxies(proxies []map[string]any) ([]byte, error) {
	if len(proxies) == 1 {
		return yaml.Marshal(proxies[0])
	}
	return yaml.Marshal(map[string]any{"proxies": proxies})
}
