// Package browser 极简 CDP 客户端：浏览器级 websocket + Target.attachToTarget（flatten）单 session，命令按 id 匹配应答，并发安全。
// 只实现面板与工具用到的命令面（Page / Input / Runtime / Emulation），不引第三方 CDP 库。
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"WorkBaby/internal/pkg"

	"github.com/gorilla/websocket"
)

// CDP 浏览器级 websocket 客户端。
type CDP struct {
	conn *websocket.Conn

	mu       sync.Mutex
	nextID   int64
	pending  map[int64]chan cdpEnvelope
	sessions map[string]string // alias → 实际 sessionId（当前单页面固定一个）
}

// cdpEnvelope CDP websocket 消息（应答或事件共用壳）。
type cdpEnvelope struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *cdpError       `json:"error"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// cdpError CDP 协议错误。
type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Dial 建立 websocket 连接并启动读泵。
func Dial(ctx context.Context, wsURL string) (*CDP, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, pkg.Wrap(8702, "cdp dial", err)
	}
	c := &CDP{
		conn:     conn,
		pending:  map[int64]chan cdpEnvelope{},
		sessions: map[string]string{},
	}
	go c.readLoop()
	return c, nil
}

// readLoop 按 id 分发应答；事件（无 id）当前不消费，直接丢弃。
func (c *CDP) readLoop() {
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			c.failAll(err)
			return
		}
		var env cdpEnvelope
		if json.Unmarshal(raw, &env) != nil {
			continue
		}
		if env.ID == 0 {
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[env.ID]
		delete(c.pending, env.ID)
		c.mu.Unlock()
		if ok {
			ch <- env
		}
	}
}

// failAll 连接断开时唤醒全部在途命令。
func (c *CDP) failAll(err error) {
	c.mu.Lock()
	pending := c.pending
	c.pending = map[int64]chan cdpEnvelope{}
	c.mu.Unlock()
	for _, ch := range pending {
		ch <- cdpEnvelope{Error: &cdpError{Code: -1, Message: "connection closed: " + err.Error()}}
	}
}

// Attach 创建页面 target 并 flatten 挂载，返回 sessionId。
func (c *CDP) Attach(ctx context.Context, url string) (string, error) {
	var createRes struct {
		TargetID string `json:"targetId"`
	}
	if err := c.send(ctx, "", "Target.createTarget", map[string]any{"url": url}, &createRes); err != nil {
		return "", err
	}
	var attachRes struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.send(ctx, "", "Target.attachToTarget", map[string]any{
		"targetId": createRes.TargetID, "flatten": true,
	}, &attachRes); err != nil {
		return "", err
	}
	c.mu.Lock()
	c.sessions["page"] = attachRes.SessionID
	c.mu.Unlock()
	return attachRes.SessionID, nil
}

// Command 页面会话命令：result 反序列化到 out（nil = 忽略）。
func (c *CDP) Command(ctx context.Context, method string, params map[string]any, out any) error {
	c.mu.Lock()
	sid := c.sessions["page"]
	c.mu.Unlock()
	return c.send(ctx, sid, method, params, out)
}

// Close 断开连接（不杀浏览器进程；进程管理归 service 层）。
func (c *CDP) Close() {
	_ = c.conn.Close()
	c.failAll(fmt.Errorf("client closed"))
}

// send 底层命令发送 + 应答等待（带 ctx 超时）。
func (c *CDP) send(ctx context.Context, sessionID, method string, params map[string]any, out any) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan cdpEnvelope, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	msg := map[string]any{"id": id, "method": method, "params": params}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	if err := c.conn.WriteJSON(msg); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return pkg.Wrap(8703, "cdp write "+method, err)
	}
	select {
	case env := <-ch:
		if env.Error != nil {
			return pkg.New(8703, "cdp "+method+" failed: "+env.Error.Message, method)
		}
		if out != nil && len(env.Result) > 0 {
			if err := json.Unmarshal(env.Result, out); err != nil {
				return pkg.Wrap(8703, "cdp decode "+method, err)
			}
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return pkg.Wrap(8704, "cdp timeout "+method, ctx.Err())
	}
}
