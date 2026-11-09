package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// GostResult 节点执行命令的结果（对应 Java 版 GostDto）
type GostResult struct {
	Msg  string
	Data map[string]any
}

func (g *GostResult) ok() bool { return g != nil && g.Msg == "OK" }

type wsConn struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	isNode  bool
	id      int64  // 节点 ID 或用户 ID
	secret  string // 节点密钥（用于加密）
	closed  bool
	version string
}

func (c *wsConn) send(msg []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return websocket.ErrCloseSent
	}
	if c.isNode {
		msg = encryptForNode(c.secret, msg)
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteMessage(websocket.TextMessage, msg)
}

func (c *wsConn) close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	_ = c.conn.Close()
}

type Hub struct {
	mu      sync.RWMutex
	nodes   map[int64]*wsConn
	admins  map[*wsConn]struct{}
	pending sync.Map // requestId -> chan *GostResult
}

var hub = &Hub{nodes: map[int64]*wsConn{}, admins: map[*wsConn]struct{}{}}

var upgrader = websocket.Upgrader{
	CheckOrigin:     func(*http.Request) bool { return true },
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

func (h *Hub) handleWS(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	secret := q.Get("secret")
	c := &wsConn{}
	if q.Get("type") == "1" {
		node, err := queryOne(scanNode, "SELECT "+nodeCols+" FROM node WHERE secret = ?", secret)
		if err != nil || node == nil || secret == "" {
			log.Printf("节点验证失败：未找到匹配的 secret，IP: %s", clientIP(r))
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		c.isNode, c.id, c.secret, c.version = true, node.ID, secret, q.Get("version")
	} else {
		claims, valid := parseToken(secret)
		if !valid {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		c.id = claims.UserID
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(16 << 20)
	c.conn = conn

	if c.isNode {
		h.nodeOnline(c, q.Get("http"), q.Get("tls"), q.Get("socks"))
	} else {
		h.mu.Lock()
		h.admins[c] = struct{}{}
		h.mu.Unlock()
	}

	defer h.onClose(c)
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if len(bytes.TrimSpace(msg)) > 0 {
			h.onMessage(c, msg)
		}
	}
}

func (h *Hub) nodeOnline(c *wsConn, httpS, tlsS, socksS string) {
	h.mu.Lock()
	old := h.nodes[c.id]
	h.nodes[c.id] = c
	h.mu.Unlock()
	if old != nil {
		log.Printf("节点 %d 已有连接，新连接将覆盖旧连接", c.id)
		old.close()
	}

	sets := "status = 1"
	args := []any{}
	if c.version != "" {
		sets += ", version = ?"
		args = append(args, c.version)
	}
	for col, v := range map[string]string{"http": httpS, "tls": tlsS, "socks": socksS} {
		if n, err := strconv.Atoi(v); err == nil {
			sets += ", " + col + " = ?"
			args = append(args, n)
		}
	}
	args = append(args, c.id)
	if _, err := db.Exec("UPDATE node SET "+sets+" WHERE id = ?", args...); err != nil {
		log.Printf("节点 %d 状态更新失败: %v", c.id, err)
		return
	}
	log.Printf("节点 %d 上线，版本: %s", c.id, c.version)
	h.broadcast(map[string]any{"id": strconv.FormatInt(c.id, 10), "type": "status", "data": 1})
}

func (h *Hub) onClose(c *wsConn) {
	c.close()
	if !c.isNode {
		h.mu.Lock()
		delete(h.admins, c)
		h.mu.Unlock()
		return
	}
	h.mu.Lock()
	current := h.nodes[c.id] == c
	if current {
		delete(h.nodes, c.id)
	}
	h.mu.Unlock()
	if !current {
		return // 已被新连接替换，不改状态
	}
	if _, err := db.Exec("UPDATE node SET status = 0 WHERE id = ?", c.id); err != nil {
		log.Printf("节点 %d 离线状态更新失败: %v", c.id, err)
	}
	log.Printf("节点 %d 离线", c.id)
	h.broadcast(map[string]any{"id": strconv.FormatInt(c.id, 10), "type": "status", "data": 0})
}

func (h *Hub) onMessage(c *wsConn, raw []byte) {
	payload := raw
	if c.isNode {
		payload = decryptIfNeeded(raw, c.secret)
	}
	if bytes.Contains(payload, []byte("memory_usage")) {
		_ = c.send([]byte(`{"type":"call"}`))
	} else if bytes.Contains(payload, []byte("requestId")) {
		var resp struct {
			RequestID string          `json:"requestId"`
			Message   *string         `json:"message"`
			Type      string          `json:"type"`
			Data      json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(payload, &resp); err == nil && resp.RequestID != "" {
			if ch, ok := h.pending.LoadAndDelete(resp.RequestID); ok {
				result := &GostResult{}
				if resp.Message != nil {
					result.Msg = *resp.Message
				} else if resp.Type == "PingResponse" {
					result.Msg = "OK"
				} else {
					result.Msg = "无响应消息"
				}
				var data map[string]any
				if len(resp.Data) > 0 && json.Unmarshal(resp.Data, &data) == nil {
					result.Data = data
				}
				ch.(chan *GostResult) <- result
			}
		}
		log.Printf("收到节点 %d 响应: %s", c.id, truncate(payload, 500))
	}

	if c.isNode {
		msg, _ := json.Marshal(map[string]any{"id": strconv.FormatInt(c.id, 10), "type": "info", "data": string(payload)})
		h.mu.RLock()
		admins := make([]*wsConn, 0, len(h.admins))
		for a := range h.admins {
			admins = append(admins, a)
		}
		h.mu.RUnlock()
		for _, a := range admins {
			_ = a.send(msg)
		}
	}
}

func (h *Hub) broadcast(v any) {
	msg, _ := json.Marshal(v)
	h.mu.RLock()
	admins := make([]*wsConn, 0, len(h.admins))
	for a := range h.admins {
		admins = append(admins, a)
	}
	h.mu.RUnlock()
	for _, a := range admins {
		_ = a.send(msg)
	}
}

func (h *Hub) closeAll() {
	h.mu.Lock()
	conns := []*wsConn{}
	for _, c := range h.nodes {
		conns = append(conns, c)
	}
	for c := range h.admins {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		c.close()
	}
}

// sendMsg 向节点发送命令并等待响应（最长 10 秒）
func sendMsg(nodeID int64, data any, typ string) *GostResult {
	hub.mu.RLock()
	c := hub.nodes[nodeID]
	hub.mu.RUnlock()
	if c == nil {
		return &GostResult{Msg: "节点不在线"}
	}
	requestID := newUUID()
	ch := make(chan *GostResult, 1)
	hub.pending.Store(requestID, ch)
	defer hub.pending.Delete(requestID)

	msg, _ := json.Marshal(map[string]any{"type": typ, "data": data, "requestId": requestID})
	if err := c.send(msg); err != nil {
		return &GostResult{Msg: "发送消息失败: " + err.Error()}
	}
	select {
	case r := <-ch:
		log.Printf("节点 %d 执行 %s: %s", nodeID, typ, r.Msg)
		return r
	case <-time.After(10 * time.Second):
		log.Printf("节点 %d 执行 %s 响应超时", nodeID, typ)
		return &GostResult{Msg: "等待响应超时"}
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
