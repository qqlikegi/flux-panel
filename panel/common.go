package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

func nowMs() int64 { return time.Now().UnixMilli() }

// ---------- 统一返回结构，与 Java 版 R 一致 ----------

type R struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Ts   int64  `json:"ts"`
	Data any    `json:"data"`
}

func ok(data ...any) R {
	r := R{Code: 0, Msg: "操作成功", Ts: nowMs()}
	if len(data) > 0 {
		r.Data = data[0]
	}
	return r
}

func fail(msg string) R { return R{Code: -1, Msg: msg, Ts: nowMs()} }

func failCode(code int, msg string) R { return R{Code: code, Msg: msg, Ts: nowMs()} }

// bizErr 业务异常，对应 Java 里抛出的 RuntimeException（统一返回 code=-2）
type bizErr string

func (e bizErr) Error() string { return string(e) }

func throw(msg string) { panic(bizErr(msg)) }

// validErr 参数校验失败（Java 版返回 code=500）
type validErr string

// ---------- 宽松的 JSON 数字类型：兼容 1 / "1" / null / "" ----------

type Num struct {
	Valid bool
	V     float64
}

func (n *Num) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		*n = Num{}
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		str = strings.TrimSpace(str)
		if str == "" {
			*n = Num{}
			return nil
		}
		s = str
	}
	if s == "true" || s == "false" {
		return fmt.Errorf("不是数字: %s", s)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("不是数字: %s", s)
	}
	*n = Num{Valid: true, V: v}
	return nil
}

func (n Num) Int() int64 { return int64(n.V) }

func (n Num) Ptr() *int64 {
	if !n.Valid {
		return nil
	}
	v := int64(n.V)
	return &v
}

// Str 宽松字符串：兼容数字等非字符串值
type Str struct {
	Valid bool
	V     string
}

func (s *Str) UnmarshalJSON(b []byte) error {
	t := strings.TrimSpace(string(b))
	if t == "null" {
		*s = Str{}
		return nil
	}
	if strings.HasPrefix(t, `"`) {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = Str{Valid: true, V: v}
		return nil
	}
	*s = Str{Valid: true, V: t}
	return nil
}

func (s Str) Blank() bool { return !s.Valid || strings.TrimSpace(s.V) == "" }

func (s Str) Ptr() *string {
	if !s.Valid {
		return nil
	}
	v := s.V
	return &v
}

// 参数校验辅助（消息与 Java 版注解保持一致）
func notBlank(s Str, msg string) {
	if s.Blank() {
		panic(validErr(msg))
	}
}

func notNull(n Num, msg string) {
	if !n.Valid {
		panic(validErr(msg))
	}
}

func minVal(n Num, min float64, msg string) {
	if n.Valid && n.V < min {
		panic(validErr(msg))
	}
}

// ---------- 宽松的 Map 参数读取（对应 Java 里 params.get("id").toString()） ----------

type Params map[string]any

func (p Params) str(key string) string {
	v, ok := p[key]
	if !ok || v == nil {
		throw("参数 " + key + " 不能为空")
	}
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func (p Params) int(key string) int64 {
	s := strings.TrimSpace(p.str(key))
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		throw("参数 " + key + " 格式错误")
	}
	return v
}

// ---------- 加解密 ----------

func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// simpleUUID 不带横线的 UUID（节点密钥）
func simpleUUID() string { return strings.ReplaceAll(newUUID(), "-", "") }

// AES-256-GCM，密钥为 SHA256(secret)，密文格式 base64(iv[12] + ciphertext + tag)，与节点端一致
var gcmCache sync.Map

func gcmFor(secret string) (cipher.AEAD, error) {
	if v, ok := gcmCache.Load(secret); ok {
		return v.(cipher.AEAD), nil
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	gcmCache.Store(secret, g)
	return g, nil
}

func aesEncrypt(secret string, plain []byte) (string, error) {
	g, err := gcmFor(secret)
	if err != nil {
		return "", err
	}
	iv := make([]byte, 12)
	_, _ = rand.Read(iv)
	out := g.Seal(iv, iv, plain, nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

func aesDecrypt(secret, data string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, err
	}
	if len(raw) < 12 {
		return nil, errors.New("加密数据长度不足")
	}
	g, err := gcmFor(secret)
	if err != nil {
		return nil, err
	}
	return g.Open(nil, raw[:12], raw[12:], nil)
}

// decryptIfNeeded 节点发来的数据可能是 {"encrypted":true,"data":"..."}，解密失败时按原文处理
func decryptIfNeeded(raw []byte, secret string) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' || secret == "" {
		return raw
	}
	var wrap struct {
		Encrypted bool   `json:"encrypted"`
		Data      string `json:"data"`
	}
	if err := json.Unmarshal(trimmed, &wrap); err != nil || !wrap.Encrypted || wrap.Data == "" {
		return raw
	}
	plain, err := aesDecrypt(secret, wrap.Data)
	if err != nil {
		return raw
	}
	return plain
}

func encryptForNode(secret string, msg []byte) []byte {
	if secret == "" {
		return msg
	}
	enc, err := aesEncrypt(secret, msg)
	if err != nil {
		return msg
	}
	out, _ := json.Marshal(map[string]any{"encrypted": true, "data": enc, "timestamp": nowMs()})
	return out
}

// ---------- JWT（HmacSHA256，与 Java 版格式一致，有效期 90 天） ----------

type Claims struct {
	UserID int64
	RoleID int64
	Name   string
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func jwtSign(content string) string {
	m := hmac.New(sha256.New, []byte(cfg.JWTSecret))
	m.Write([]byte(content))
	return b64url(m.Sum(nil))
}

func generateToken(u *User) string {
	now := time.Now()
	header, _ := json.Marshal(map[string]any{"alg": "HmacSHA256", "typ": "JWT"})
	payload, _ := json.Marshal(map[string]any{
		"sub":     strconv.FormatInt(u.ID, 10),
		"iat":     now.Unix(),
		"exp":     now.Add(90 * 24 * time.Hour).Unix(),
		"user":    u.User,
		"name":    u.User,
		"role_id": u.RoleID,
	})
	content := b64url(header) + "." + b64url(payload)
	return content + "." + jwtSign(content)
}

// parseToken 校验签名和有效期，返回用户信息
func parseToken(token string) (*Claims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	if !hmac.Equal([]byte(jwtSign(parts[0]+"."+parts[1])), []byte(parts[2])) {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil, false
	}
	var p map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&p); err != nil {
		return nil, false
	}
	toInt := func(v any) (int64, bool) {
		switch t := v.(type) {
		case json.Number:
			i, err := t.Int64()
			return i, err == nil
		case string:
			i, err := strconv.ParseInt(t, 10, 64)
			return i, err == nil
		}
		return 0, false
	}
	exp, ok1 := toInt(p["exp"])
	sub, ok2 := toInt(p["sub"])
	role, ok3 := toInt(p["role_id"])
	if !ok1 || !ok2 || !ok3 || exp <= time.Now().Unix() {
		return nil, false
	}
	name, _ := p["name"].(string)
	return &Claims{UserID: sub, RoleID: role, Name: name}, true
}

// processServerAddress IPv6 地址加方括号
func processServerAddress(addr string) string {
	if strings.TrimSpace(addr) == "" || strings.HasPrefix(addr, "[") {
		return addr
	}
	idx := strings.LastIndex(addr, ":")
	if idx == -1 {
		if isIPv6(addr) {
			return "[" + addr + "]"
		}
		return addr
	}
	host, port := addr[:idx], addr[idx:]
	if isIPv6(host) {
		return "[" + host + "]" + port
	}
	return addr
}

func isIPv6(s string) bool { return strings.Count(s, ":") >= 2 }
