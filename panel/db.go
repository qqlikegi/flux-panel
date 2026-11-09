package main

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// 表结构与 Java 版完全一致
//
//go:embed sql/schema.sql
var schemaSQL string

//go:embed sql/data.sql
var dataSQL string

var db *sql.DB

func openDB(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)" +
		"&_pragma=temp_store(MEMORY)&_pragma=cache_size(-64000)&_txlock=immediate"
	var err error
	db, err = sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	if err = db.Ping(); err != nil {
		return err
	}
	for _, script := range []string{schemaSQL, dataSQL} {
		for _, stmt := range splitSQL(script) {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("初始化数据库失败: %w\n%s", err, stmt)
			}
		}
	}
	return nil
}

func splitSQL(script string) []string {
	var out []string
	for _, s := range strings.Split(script, ";") {
		var lines []string
		for _, l := range strings.Split(s, "\n") {
			if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "--") {
				lines = append(lines, l)
			}
		}
		if len(lines) > 0 {
			out = append(out, strings.Join(lines, "\n"))
		}
	}
	return out
}

func checkpoint() {
	if db == nil {
		return
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		log.Printf("SQLite checkpoint 失败: %v", err)
	}
}

func closeDB() {
	if db != nil {
		_ = db.Close()
	}
}

func resetAdminAccount() error {
	if _, err := db.Exec("UPDATE user SET user = 'admin_user', pwd = ?, status = 1, updated_time = ? WHERE role_id = 0", md5Hex("admin_user"), nowMs()); err != nil {
		return err
	}
	// 重置后重新进入首次设置流程：打开面板时设置新的管理员用户名和密码
	_, err := db.Exec("DELETE FROM system_setting WHERE name = ?", setupDoneKey)
	return err
}

// ---------- 实体（JSON 字段名与 Java 版保持一致，前端无需改动） ----------

type User struct {
	ID            int64  `json:"id"`
	CreatedTime   int64  `json:"createdTime"`
	UpdatedTime   *int64 `json:"updatedTime"`
	Status        int64  `json:"status"`
	User          string `json:"user"`
	Pwd           string `json:"-"`
	RoleID        int64  `json:"roleId"`
	ExpTime       int64  `json:"expTime"`
	Flow          int64  `json:"flow"`
	InFlow        int64  `json:"inFlow"`
	OutFlow       int64  `json:"outFlow"`
	Num           int64  `json:"num"`
	FlowResetTime int64  `json:"flowResetTime"`
}

const userCols = "id, created_time, updated_time, status, user, pwd, role_id, exp_time, flow, in_flow, out_flow, num, flow_reset_time"

func scanUser(s scanner) (*User, error) {
	u := &User{}
	return u, s.Scan(&u.ID, &u.CreatedTime, &u.UpdatedTime, &u.Status, &u.User, &u.Pwd, &u.RoleID, &u.ExpTime, &u.Flow, &u.InFlow, &u.OutFlow, &u.Num, &u.FlowResetTime)
}

type Node struct {
	ID            int64   `json:"id"`
	CreatedTime   int64   `json:"createdTime"`
	UpdatedTime   *int64  `json:"updatedTime"`
	Status        int64   `json:"status"`
	Name          string  `json:"name"`
	Secret        *string `json:"secret"`
	ServerIP      string  `json:"serverIp"`
	Version       *string `json:"version"`
	Port          string  `json:"port"`
	HTTP          int64   `json:"http"`
	TLS           int64   `json:"tls"`
	Socks         int64   `json:"socks"`
	InterfaceName *string `json:"interfaceName"`
	TCPListenAddr string  `json:"tcpListenAddr"`
	UDPListenAddr string  `json:"udpListenAddr"`
}

const nodeCols = "id, created_time, updated_time, status, name, secret, server_ip, version, port, http, tls, socks, interface_name, tcp_listen_addr, udp_listen_addr"

func scanNode(s scanner) (*Node, error) {
	n := &Node{}
	return n, s.Scan(&n.ID, &n.CreatedTime, &n.UpdatedTime, &n.Status, &n.Name, &n.Secret, &n.ServerIP, &n.Version, &n.Port, &n.HTTP, &n.TLS, &n.Socks, &n.InterfaceName, &n.TCPListenAddr, &n.UDPListenAddr)
}

func (n *Node) secret() string {
	if n.Secret == nil {
		return ""
	}
	return *n.Secret
}

func (n *Node) iface() string {
	if n.InterfaceName == nil {
		return ""
	}
	return strings.TrimSpace(*n.InterfaceName)
}

type Tunnel struct {
	ID           int64   `json:"id"`
	CreatedTime  int64   `json:"createdTime"`
	UpdatedTime  int64   `json:"updatedTime"`
	Status       int64   `json:"status"`
	Name         string  `json:"name"`
	Type         int64   `json:"type"`
	Flow         int64   `json:"flow"`
	TrafficRatio float64 `json:"trafficRatio"`
	InIP         *string `json:"inIp"`
}

const tunnelCols = "id, created_time, updated_time, status, name, type, flow, traffic_ratio, in_ip"

func scanTunnel(s scanner) (*Tunnel, error) {
	t := &Tunnel{}
	return t, s.Scan(&t.ID, &t.CreatedTime, &t.UpdatedTime, &t.Status, &t.Name, &t.Type, &t.Flow, &t.TrafficRatio, &t.InIP)
}

func (t *Tunnel) inIP() string {
	if t.InIP == nil {
		return ""
	}
	return *t.InIP
}

type ChainTunnel struct {
	ID        *int64  `json:"id"`
	TunnelID  int64   `json:"tunnelId"`
	ChainType int64   `json:"chainType"`
	NodeID    int64   `json:"nodeId"`
	Port      *int64  `json:"port"`
	Strategy  *string `json:"strategy"`
	Inx       *int64  `json:"inx"`
	Protocol  *string `json:"protocol"`
}

const chainCols = "id, tunnel_id, chain_type, node_id, port, strategy, inx, protocol"

func scanChain(s scanner) (*ChainTunnel, error) {
	c := &ChainTunnel{}
	return c, s.Scan(&c.ID, &c.TunnelID, &c.ChainType, &c.NodeID, &c.Port, &c.Strategy, &c.Inx, &c.Protocol)
}

func (c *ChainTunnel) port() int64 {
	if c.Port == nil {
		return 0
	}
	return *c.Port
}

func (c *ChainTunnel) inx() int64 {
	if c.Inx == nil {
		return 0
	}
	return *c.Inx
}

type Forward struct {
	ID          int64  `json:"id"`
	CreatedTime int64  `json:"createdTime"`
	UpdatedTime int64  `json:"updatedTime"`
	Status      int64  `json:"status"`
	UserID      int64  `json:"userId"`
	UserName    string `json:"userName"`
	Name        string `json:"name"`
	TunnelID    int64  `json:"tunnelId"`
	RemoteAddr  string `json:"remoteAddr"`
	Strategy    string `json:"strategy"`
	InFlow      int64  `json:"inFlow"`
	OutFlow     int64  `json:"outFlow"`
	Inx         int64  `json:"inx"`
}

const forwardCols = "id, created_time, updated_time, status, user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, inx"

func scanForward(s scanner) (*Forward, error) {
	f := &Forward{}
	return f, s.Scan(&f.ID, &f.CreatedTime, &f.UpdatedTime, &f.Status, &f.UserID, &f.UserName, &f.Name, &f.TunnelID, &f.RemoteAddr, &f.Strategy, &f.InFlow, &f.OutFlow, &f.Inx)
}

type ForwardPort struct {
	ID        int64 `json:"id"`
	ForwardID int64 `json:"forwardId"`
	NodeID    int64 `json:"nodeId"`
	Port      int64 `json:"port"`
}

const forwardPortCols = "id, forward_id, node_id, port"

func scanForwardPort(s scanner) (*ForwardPort, error) {
	p := &ForwardPort{}
	return p, s.Scan(&p.ID, &p.ForwardID, &p.NodeID, &p.Port)
}

type SpeedLimit struct {
	ID          int64  `json:"id"`
	CreatedTime int64  `json:"createdTime"`
	UpdatedTime *int64 `json:"updatedTime"`
	Status      int64  `json:"status"`
	Name        string `json:"name"`
	Speed       int64  `json:"speed"`
	TunnelID    int64  `json:"tunnelId"`
	TunnelName  string `json:"tunnelName"`
}

const speedCols = "id, created_time, updated_time, status, name, speed, tunnel_id, tunnel_name"

func scanSpeed(s scanner) (*SpeedLimit, error) {
	l := &SpeedLimit{}
	return l, s.Scan(&l.ID, &l.CreatedTime, &l.UpdatedTime, &l.Status, &l.Name, &l.Speed, &l.TunnelID, &l.TunnelName)
}

type UserTunnel struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"userId"`
	TunnelID      int64  `json:"tunnelId"`
	Flow          int64  `json:"flow"`
	InFlow        int64  `json:"inFlow"`
	OutFlow       int64  `json:"outFlow"`
	FlowResetTime int64  `json:"flowResetTime"`
	ExpTime       int64  `json:"expTime"`
	SpeedID       *int64 `json:"speedId"`
	Num           int64  `json:"num"`
	Status        int64  `json:"status"`
}

const userTunnelCols = "id, user_id, tunnel_id, flow, in_flow, out_flow, flow_reset_time, exp_time, speed_id, num, status"

func scanUserTunnel(s scanner) (*UserTunnel, error) {
	u := &UserTunnel{}
	return u, s.Scan(&u.ID, &u.UserID, &u.TunnelID, &u.Flow, &u.InFlow, &u.OutFlow, &u.FlowResetTime, &u.ExpTime, &u.SpeedID, &u.Num, &u.Status)
}

type ViteConfig struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Value string `json:"value"`
	Time  int64  `json:"time"`
}

type StatisticsFlow struct {
	ID          *int64 `json:"id"`
	UserID      int64  `json:"userId"`
	Flow        int64  `json:"flow"`
	TotalFlow   int64  `json:"totalFlow"`
	Time        string `json:"time"`
	CreatedTime *int64 `json:"createdTime"`
}

// ---------- 通用查询 ----------

type scanner interface{ Scan(dest ...any) error }

// queryList 执行查询并把结果全部读出后再返回，避免在遍历结果时占用连接去做其它事
func queryList[T any](scan func(scanner) (*T, error), query string, args ...any) ([]*T, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	return list, rows.Err()
}

// queryOne 查询单条记录，不存在时返回 nil, nil
func queryOne[T any](scan func(scanner) (*T, error), query string, args ...any) (*T, error) {
	v, err := scan(db.QueryRow(query, args...))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return v, err
}

func countOf(query string, args ...any) int64 {
	var n int64
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		log.Printf("count 查询失败: %v", err)
	}
	return n
}

func mustExec(query string, args ...any) sql.Result {
	r, err := db.Exec(query, args...)
	if err != nil {
		panic(bizErr(fmt.Sprintf("数据库操作失败: %v", err)))
	}
	return r
}

func getUser(id int64) *User {
	u, err := queryOne(scanUser, "SELECT "+userCols+" FROM user WHERE id = ?", id)
	must(err)
	return u
}

func getNode(id int64) *Node {
	n, err := queryOne(scanNode, "SELECT "+nodeCols+" FROM node WHERE id = ?", id)
	must(err)
	return n
}

func getTunnel(id int64) *Tunnel {
	t, err := queryOne(scanTunnel, "SELECT "+tunnelCols+" FROM tunnel WHERE id = ?", id)
	must(err)
	return t
}

func getForward(id int64) *Forward {
	f, err := queryOne(scanForward, "SELECT "+forwardCols+" FROM forward WHERE id = ?", id)
	must(err)
	return f
}

func getUserTunnelByID(id int64) *UserTunnel {
	u, err := queryOne(scanUserTunnel, "SELECT "+userTunnelCols+" FROM user_tunnel WHERE id = ?", id)
	must(err)
	return u
}

func getUserTunnel(userID, tunnelID int64) *UserTunnel {
	u, err := queryOne(scanUserTunnel, "SELECT "+userTunnelCols+" FROM user_tunnel WHERE user_id = ? AND tunnel_id = ? ORDER BY id LIMIT 1", userID, tunnelID)
	must(err)
	return u
}

func getSpeedLimit(id int64) *SpeedLimit {
	l, err := queryOne(scanSpeed, "SELECT "+speedCols+" FROM speed_limit WHERE id = ?", id)
	must(err)
	return l
}

func listChains(query string, args ...any) []*ChainTunnel {
	l, err := queryList(scanChain, "SELECT "+chainCols+" FROM chain_tunnel "+query, args...)
	must(err)
	return l
}

func entryChains(tunnelID int64) []*ChainTunnel {
	return listChains("WHERE tunnel_id = ? AND chain_type = 1 ORDER BY id", tunnelID)
}

func listForwards(query string, args ...any) []*Forward {
	l, err := queryList(scanForward, "SELECT "+forwardCols+" FROM forward "+query, args...)
	must(err)
	return l
}

func listForwardPorts(query string, args ...any) []*ForwardPort {
	l, err := queryList(scanForwardPort, "SELECT "+forwardPortCols+" FROM forward_port "+query, args...)
	must(err)
	return l
}

func getConfigValue(name string) (string, bool) {
	var v string
	err := db.QueryRow("SELECT value FROM vite_config WHERE name = ?", name).Scan(&v)
	if err != nil {
		return "", false
	}
	return v, true
}

func must(err error) {
	if err != nil {
		panic(bizErr(fmt.Sprintf("数据库操作失败: %v", err)))
	}
}
