package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
)

// HTTPNode 执行 HTTP 请求（method/url/headers/body 来自 cfg）。
//
// 输出 {status, body, headers}；body 截断 50KB 防 LLM 上下文爆。
type HTTPNode struct {
	client      *http.Client
	maxBodySize int64
}

// NewHTTPNode 构造；自动注册 schema。
func NewHTTPNode(timeout time.Duration) *HTTPNode {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	n := &HTTPNode{
		client:      &http.Client{Timeout: timeout},
		maxBodySize: 50 * 1024,
	}
	Register(n)
	return n
}

// Type 实现 Node 接口。
func (n *HTTPNode) Type() domain.WorkflowNodeType { return domain.WorkflowNodeHTTP }

// Schema 实现 Node 接口。
func (n *HTTPNode) Schema() Schema {
	return Schema{
		Type:     domain.WorkflowNodeHTTP,
		Required: []string{"url"},
		Optional: []string{"method", "headers", "body"},
	}
}

// Execute：cfg.method 默认 GET；body 字符串按原样发送。
// method 白名单仅 GET/POST（CLAUDE.md §2.12），其他返回 9104 节点配置错。
func (n *HTTPNode) Execute(ctx context.Context, inputs map[string]any, cfg map[string]any, upstream map[string]any) (map[string]any, error) {
	url, _ := cfg["url"].(string)
	if url == "" {
		return nil, pkg.New(9104, "HTTP 节点缺少 cfg.url", "")
	}
	rawMethod, _ := cfg["method"].(string)
	if rawMethod == "" {
		rawMethod = "GET"
	}
	method, ok := pkg.NormalizeMethod(rawMethod)
	if !ok {
		return nil, pkg.New(9104, fmt.Sprintf("HTTP 节点 method 不允许（允许 %s）", pkg.AllowedMethodCSV()), rawMethod)
	}

	var bodyReader io.Reader
	if b, ok := cfg["body"].(string); ok && b != "" {
		bodyReader = strings.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, pkg.Wrap(9105, "构建 HTTP 请求失败", err)
	}
	if h, ok := cfg["headers"].(map[string]any); ok {
		for k, v := range h {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, pkg.Wrap(9105, "HTTP 请求失败", err)
	}
	defer resp.Body.Close()

	// 截断读取，防止返回体过大
	buf := make([]byte, n.maxBodySize)
	nRead, _ := io.ReadFull(resp.Body, buf)
	body := string(buf[:nRead])

	headers := map[string]string{}
	for k, vs := range resp.Header {
		if len(vs) > 0 {
			headers[k] = vs[0]
		}
	}
	return map[string]any{
		"status":    resp.StatusCode,
		"body":      body,
		"headers":   headers,
		"truncated": int64(nRead) == n.maxBodySize,
	}, nil
}

// ensureBodyAsJSON 仅在 cfg.body 是 map 时序列化为 JSON 字符串。
func ensureBodyAsJSON(v any) (string, bool) {
	switch b := v.(type) {
	case string:
		return b, false
	case map[string]any, []any:
		bb, err := json.Marshal(b)
		if err != nil {
			return "", true
		}
		return string(bb), true
	}
	return "", false
}
