// flux-panel 面板后端（Go 版）。
// 单个二进制同时提供：前端页面、/api/v1 接口、节点 WebSocket(/system-info)、节点流量上报(/flow/*)。
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // 内置时区数据，精简系统上也能使用 TZ=Asia/Shanghai
)

// version 发布时通过 -ldflags "-X main.version=..." 注入
var version = "dev"

type Config struct {
	Port      string
	JWTSecret string
	DBPath    string
	LogDir    string
	WebDir    string // 非空时从磁盘目录提供前端页面（开发调试用），否则使用内嵌页面
}

var cfg Config

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func main() {
	showVersion := flag.Bool("v", false, "显示版本")
	resetAdmin := flag.Bool("reset-admin", false, "把管理员账号重置为 admin_user / admin_user")
	flag.Parse()
	if *showVersion {
		fmt.Println("flux-panel", version)
		return
	}

	cfg = Config{
		Port:      env("SERVER_PORT", "6365"),
		JWTSecret: env("JWT_SECRET", ""),
		DBPath:    env("DB_PATH", "data/gost.db"),
		LogDir:    env("LOG_DIR", "logs"),
		WebDir:    env("WEB_DIR", ""),
	}
	setupLogger(cfg.LogDir)

	if err := openDB(cfg.DBPath); err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer closeDB()

	if *resetAdmin {
		if err := resetAdminAccount(); err != nil {
			log.Fatalf("重置管理员失败: %v", err)
		}
		fmt.Println("管理员账号已重置为 admin_user / admin_user，请登录后立即修改密码")
		return
	}

	if cfg.JWTSecret == "" {
		cfg.JWTSecret = loadOrCreateSecret(filepath.Join(filepath.Dir(cfg.DBPath), "jwt.secret"))
	}

	startTasks()

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newRouter(),
		ReadHeaderTimeout: 15 * time.Second,
	}
	go func() {
		log.Printf("flux-panel %s 已启动，监听端口 %s，数据库 %s", version, cfg.Port, cfg.DBPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP 服务启动失败: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("正在关闭面板...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hub.closeAll()
	_ = srv.Shutdown(ctx)
	checkpoint()
	log.Println("面板已关闭")
}

// loadOrCreateSecret 未配置 JWT_SECRET 时，生成随机密钥并保存，重启后保持不变（已登录用户不会被踢下线）
func loadOrCreateSecret(path string) string {
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 16 {
		return strings.TrimSpace(string(b))
	}
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	s := hex.EncodeToString(buf)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		log.Printf("保存 JWT 密钥失败（重启后需重新登录）: %v", err)
	}
	return s
}
