package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"runtime/debug"
	"strings"
	"time"
)

// 前端构建产物放在 web/dist，编译时打包进二进制
//
//go:embed all:web
var embeddedWeb embed.FS

type Ctx struct {
	W      http.ResponseWriter
	Req    *http.Request
	Body   []byte
	Claims *Claims
}

// bind 解析 JSON 请求体
func (c *Ctx) bind(v any) {
	if len(bytes.TrimSpace(c.Body)) == 0 {
		throw("请求体不能为空")
	}
	if err := json.Unmarshal(c.Body, v); err != nil {
		throw("请求参数格式错误: " + err.Error())
	}
}

func (c *Ctx) params() Params {
	p := Params{}
	if len(bytes.TrimSpace(c.Body)) == 0 {
		return p
	}
	dec := json.NewDecoder(bytes.NewReader(c.Body))
	dec.UseNumber()
	if err := dec.Decode(&p); err != nil {
		throw("请求参数格式错误: " + err.Error())
	}
	return p
}

func (c *Ctx) isAdmin() bool { return c.Claims != nil && c.Claims.RoleID == 0 }

func (c *Ctx) userID() int64 { return c.Claims.UserID }

// text 直接返回纯文本（/flow/* 接口）
type text string

// rawJSON 原样输出的 JSON（验证码接口使用 tianai-captcha 的返回格式）
type rawJSON struct{ v any }

type handler func(c *Ctx) any

type route struct {
	auth  bool // 需要登录
	admin bool // 需要管理员
	h     handler
}

func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		return strings.TrimSpace(strings.Split(xf, ",")[0])
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return xr
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	_ = json.NewEncoder(w).Encode(v)
}

func (rt route) serve(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	body, _ := io.ReadAll(io.LimitReader(r.Body, 50<<20))
	c := &Ctx{W: w, Req: r, Body: body}
	var result any

	func() {
		defer func() {
			if e := recover(); e != nil {
				switch t := e.(type) {
				case bizErr:
					result = failCode(-2, string(t))
				case validErr:
					result = failCode(500, string(t))
				default:
					log.Printf("接口异常 %s: %v\n%s", r.URL.Path, e, debug.Stack())
					result = failCode(-2, fmt.Sprint(e))
				}
			}
		}()
		if rt.auth {
			token := r.Header.Get("Authorization")
			if strings.TrimSpace(token) == "" {
				result = failCode(401, "未登录或token已过期")
				return
			}
			claims, valid := parseToken(token)
			if !valid {
				result = failCode(401, "无效的token或token已过期")
				return
			}
			c.Claims = claims
			if rt.admin && claims.RoleID != 0 {
				result = failCode(403, "权限不足，仅管理员可操作")
				return
			}
		} else if token := r.Header.Get("Authorization"); token != "" {
			c.Claims, _ = parseToken(token)
		}
		result = rt.h(c)
	}()

	code := 0
	switch v := result.(type) {
	case R:
		code = v.Code
		writeJSON(w, v)
	case text:
		w.Header().Set("Content-Type", "text/plain;charset=UTF-8")
		_, _ = io.WriteString(w, string(v))
	case rawJSON:
		writeJSON(w, v.v)
	case nil:
	default:
		writeJSON(w, v)
	}

	if !strings.HasPrefix(r.URL.Path, "/flow/") {
		uid := "未登录"
		if c.Claims != nil {
			uid = fmt.Sprint(c.Claims.UserID)
		}
		log.Printf("[请求] 用户:%s IP:%s %s %s code=%d 耗时=%s", uid, clientIP(r), r.Method, r.URL.Path, code, time.Since(start).Round(time.Millisecond))
	}
}

func newRouter() http.Handler {
	mux := http.NewServeMux()
	public := func(h handler) route { return route{h: h} }
	user := func(h handler) route { return route{auth: true, h: h} }
	admin := func(h handler) route { return route{auth: true, admin: true, h: h} }

	routes := map[string]route{
		// 用户
		"POST /api/v1/user/login":          public(userLogin),
		"POST /api/v1/user/create":         admin(userCreate),
		"POST /api/v1/user/list":           admin(userList),
		"POST /api/v1/user/update":         admin(userUpdate),
		"POST /api/v1/user/delete":         admin(userDelete),
		"POST /api/v1/user/package":        user(userPackage),
		"POST /api/v1/user/updatePassword": user(userUpdatePassword),
		"POST /api/v1/user/reset":          admin(userReset),
		// 节点
		"POST /api/v1/node/create":  admin(nodeCreate),
		"POST /api/v1/node/list":    admin(nodeList),
		"POST /api/v1/node/update":  admin(nodeUpdate),
		"POST /api/v1/node/delete":  admin(nodeDelete),
		"POST /api/v1/node/install": admin(nodeInstall),
		// 隧道
		"POST /api/v1/tunnel/create":      admin(tunnelCreate),
		"POST /api/v1/tunnel/list":        admin(tunnelList),
		"POST /api/v1/tunnel/update":      admin(tunnelUpdate),
		"POST /api/v1/tunnel/delete":      admin(tunnelDelete),
		"POST /api/v1/tunnel/user/assign": admin(userTunnelAssign),
		"POST /api/v1/tunnel/user/list":   admin(userTunnelList),
		"POST /api/v1/tunnel/user/remove": admin(userTunnelRemove),
		"POST /api/v1/tunnel/user/update": admin(userTunnelUpdate),
		"POST /api/v1/tunnel/user/tunnel": user(tunnelForUser),
		"POST /api/v1/tunnel/diagnose":    admin(tunnelDiagnose),
		// 转发
		"POST /api/v1/forward/create":       user(forwardCreate),
		"POST /api/v1/forward/list":         user(forwardList),
		"POST /api/v1/forward/update":       user(forwardUpdate),
		"POST /api/v1/forward/delete":       user(forwardDelete),
		"POST /api/v1/forward/force-delete": user(forwardForceDelete),
		"POST /api/v1/forward/pause":        user(forwardPause),
		"POST /api/v1/forward/resume":       user(forwardResume),
		"POST /api/v1/forward/diagnose":     user(forwardDiagnose),
		"POST /api/v1/forward/update-order": user(forwardUpdateOrder),
		// 限速
		"POST /api/v1/speed-limit/create":  admin(speedCreate),
		"POST /api/v1/speed-limit/list":    admin(speedList),
		"POST /api/v1/speed-limit/update":  admin(speedUpdate),
		"POST /api/v1/speed-limit/delete":  admin(speedDelete),
		"POST /api/v1/speed-limit/tunnels": admin(tunnelList),
		// 首次部署设置管理员
		"POST /api/v1/setup/status": public(setupStatus),
		"POST /api/v1/setup/init":   public(setupInit),
		// 数据总览
		"POST /api/v1/dashboard/overview": admin(dashboardOverview),
		// 云存储备份
		"POST /api/v1/backup/get":       admin(backupGet),
		"POST /api/v1/backup/save":      admin(backupSave),
		"POST /api/v1/backup/test":      admin(backupTest),
		"POST /api/v1/backup/run":       admin(backupRun),
		"POST /api/v1/backup/list":      admin(backupList),
		"POST /api/v1/fingerprint/get":  admin(fingerprintGet),
		"POST /api/v1/fingerprint/save": admin(fingerprintSave),
		// 系统配置
		"POST /api/v1/config/list":          user(configList),
		"POST /api/v1/config/get":           public(configGet),
		"POST /api/v1/config/update":        admin(configUpdate),
		"POST /api/v1/config/update-single": admin(configUpdateSingle),
		// 验证码
		"POST /api/v1/captcha/check":    public(captchaCheck),
		"POST /api/v1/captcha/generate": public(captchaGenerate),
		"POST /api/v1/captcha/verify":   public(captchaVerify),
		// 订阅信息
		"GET /api/v1/open_api/sub_store": public(openSubStore),
		// 节点上报
		"POST /flow/config": public(flowConfig),
		"/flow/upload":      public(flowUpload),
		"/flow/test":        public(func(*Ctx) any { return text("test") }),
	}
	for pattern, rt := range routes {
		mux.HandleFunc(pattern, rt.serve)
	}
	mux.HandleFunc("/system-info", hub.handleWS)
	mux.HandleFunc("/api/", notFound)
	mux.HandleFunc("/", staticHandler())
	return cors(mux)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Headers", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		h.Set("Access-Control-Expose-Headers", "Authorization")
		h.Set("Access-Control-Max-Age", "3600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

const notFoundHTML = `<!DOCTYPE html><html lang='zh-CN'><head><meta charset='UTF-8'><meta name='viewport' content='width=device-width, initial-scale=1.0'><title>错误 404</title><style>body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Arial,sans-serif;margin:0;min-height:100vh;display:flex;flex-direction:column;justify-content:center;align-items:center;text-align:center}.c{font-size:6rem;color:#333;font-weight:300;margin:0}.t{font-size:1.2rem;color:#666;margin-top:20px}</style></head><body><div class='c'>404</div><div class='t'>你推开了后端的大门，却发现里面只有寂寞。</div></body></html>`

func notFound(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, notFoundHTML)
}

// staticHandler 提供前端页面；前端路由（如 /dashboard）刷新时回退到 index.html
func staticHandler() http.HandlerFunc {
	var webRoot fs.FS
	if cfg.WebDir != "" {
		webRoot = os.DirFS(cfg.WebDir)
		log.Printf("前端页面目录: %s", cfg.WebDir)
	} else if sub, err := fs.Sub(embeddedWeb, "web/dist"); err == nil {
		webRoot = sub
	}
	hasIndex := false
	if webRoot != nil {
		if _, err := fs.Stat(webRoot, "index.html"); err == nil {
			hasIndex = true
		}
	}
	if !hasIndex {
		log.Printf("未找到前端页面，只提供接口服务")
	}
	fileServer := http.FileServer(http.FS(webRoot))

	return func(w http.ResponseWriter, r *http.Request) {
		if !hasIndex || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			notFound(w, r)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if st, err := fs.Stat(webRoot, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
			if path.Ext(name) != "" {
				notFound(w, r)
				return
			}
		}
		index, err := fs.ReadFile(webRoot, "index.html")
		if err != nil {
			notFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	}
}
