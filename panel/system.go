package main

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ---------- 系统内部设置（system_setting 表，不经过公开的配置接口） ----------

func getSetting(name string) string {
	var v string
	if err := db.QueryRow("SELECT value FROM system_setting WHERE name = ?", name).Scan(&v); err != nil {
		return ""
	}
	return v
}

func setSetting(name, value string) {
	mustExec(`INSERT INTO system_setting (name, value) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET value = excluded.value`, name, value)
}

// ---------- 首次部署：设置管理员账号 ----------

const setupDoneKey = "admin_initialized"

var setupMu sync.Mutex

func needSetup() bool { return getSetting(setupDoneKey) != "true" }

// initSetupState 启动时调用：从旧版本升级上来、管理员已经改过默认账号密码的，直接视为已初始化
func initSetupState() {
	if getSetting(setupDoneKey) != "" {
		return
	}
	var user, pwd string
	err := db.QueryRow("SELECT user, pwd FROM user WHERE role_id = 0 ORDER BY id LIMIT 1").Scan(&user, &pwd)
	if err == nil && !(user == "admin_user" && pwd == md5Hex("admin_user")) {
		setSetting(setupDoneKey, "true")
	}
}

// backfillDailyFlow 升级后第一次启动：用最近 48 小时的整点统计补上每日流量，本月数据不会从 0 开始
func backfillDailyFlow() {
	if countOf("SELECT COUNT(*) FROM user_flow_daily") > 0 {
		return
	}
	stats, err := queryList(func(s scanner) (*StatisticsFlow, error) {
		f := &StatisticsFlow{}
		return f, s.Scan(&f.UserID, &f.Flow, &f.CreatedTime)
	}, "SELECT user_id, flow, created_time FROM statistics_flow WHERE flow > 0")
	must(err)
	sum := map[[2]any]int64{}
	for _, f := range stats {
		if f.CreatedTime == nil {
			continue
		}
		day := time.UnixMilli(*f.CreatedTime - 60000).Format("2006-01-02")
		sum[[2]any{f.UserID, day}] += f.Flow
	}
	for k, v := range sum {
		mustExec("INSERT OR IGNORE INTO user_flow_daily (user_id, day, flow) VALUES (?, ?, ?)", k[0], k[1], v)
	}
}

func setupStatus(c *Ctx) any {
	return ok(map[string]any{"needSetup": needSetup()})
}

func setupInit(c *Ctx) any {
	var d struct {
		Username Str `json:"username"`
		Password Str `json:"password"`
	}
	c.bind(&d)
	notBlank(d.Username, "管理员用户名不能为空")
	if !d.Password.Valid || d.Password.V == "" {
		return fail("管理员密码不能为空")
	}
	name := strings.TrimSpace(d.Username.V)
	setupMu.Lock()
	defer setupMu.Unlock()
	if !needSetup() {
		return fail("系统已经初始化过了，请直接登录")
	}
	if countOf("SELECT COUNT(*) FROM user WHERE user = ? AND role_id != 0", name) > 0 {
		return fail("该用户名已被普通用户使用")
	}
	var id int64
	err := db.QueryRow("SELECT id FROM user WHERE role_id = 0 ORDER BY id LIMIT 1").Scan(&id)
	now := nowMs()
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res := mustExec(`INSERT INTO user (user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status)
			VALUES (?, ?, 0, 2727251700000, 99999, 0, 0, 1, 99999, ?, ?, 1)`, name, md5Hex(d.Password.V), now, now)
		id, _ = res.LastInsertId()
	case err != nil:
		must(err)
	default:
		mustExec("UPDATE user SET user = ?, pwd = ?, status = 1, updated_time = ? WHERE id = ?", name, md5Hex(d.Password.V), now, id)
	}
	setSetting(setupDoneKey, "true")
	u := getUser(id)
	return ok(map[string]any{
		"token": generateToken(u), "name": u.User, "role_id": u.RoleID, "requirePasswordChange": false,
	})
}

// ---------- 数据总览（管理员首页） ----------

func dashboardOverview(c *Ctx) any {
	now := time.Now()
	today := now.Format("2006-01-02")
	month := now.Format("2006-01")

	var nodeTotal, nodeOnline int64
	must(db.QueryRow("SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = 1 THEN 1 ELSE 0 END), 0) FROM node").Scan(&nodeTotal, &nodeOnline))

	// 今日：今天各整点统计之和（00:00 那条统计的是昨天最后一小时，不算）
	todayMap, monthMap := map[int64]int64{}, map[int64]int64{}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	rows, err := db.Query("SELECT user_id, COALESCE(SUM(flow), 0) FROM statistics_flow WHERE created_time >= ? GROUP BY user_id", dayStart.UnixMilli()+60000)
	must(err)
	for rows.Next() {
		var uid, t int64
		if rows.Scan(&uid, &t) == nil {
			todayMap[uid] = t
		}
	}
	rows.Close()
	// 本月：本月之前各天的每日流量 + 今日
	rows, err = db.Query("SELECT user_id, COALESCE(SUM(flow), 0) FROM user_flow_daily WHERE day >= ? AND day < ? GROUP BY user_id", month+"-01", today)
	must(err)
	for rows.Next() {
		var uid, m int64
		if rows.Scan(&uid, &m) == nil {
			monthMap[uid] = m
		}
	}
	rows.Close()
	for uid, t := range todayMap {
		monthMap[uid] += t
	}
	// 最近一次整点统计时的累计流量（当前小时还没入账的部分 = 现在的累计 - 上次统计）
	lastTotal := map[int64]int64{}
	rows, err = db.Query("SELECT user_id, total_flow FROM statistics_flow WHERE id IN (SELECT MAX(id) FROM statistics_flow GROUP BY user_id)")
	must(err)
	for rows.Next() {
		var uid, t int64
		if rows.Scan(&uid, &t) == nil {
			lastTotal[uid] = t
		}
	}
	rows.Close()
	forwardCount := map[int64]int64{}
	rows, err = db.Query("SELECT user_id, COUNT(*) FROM forward GROUP BY user_id")
	must(err)
	for rows.Next() {
		var uid, n int64
		if rows.Scan(&uid, &n) == nil {
			forwardCount[uid] = n
		}
	}
	rows.Close()

	users, err := queryList(scanUser, "SELECT "+userCols+" FROM user ORDER BY id")
	must(err)
	var totalUsed, totalToday, totalMonth int64
	list := []map[string]any{}
	for _, u := range users {
		used := u.InFlow + u.OutFlow
		var pending int64
		if last, has := lastTotal[u.ID]; has {
			pending = used - last
			if pending < 0 { // 刚重置过流量
				pending = used
			}
		}
		t, m := todayMap[u.ID]+pending, monthMap[u.ID]+pending
		totalUsed += used
		totalToday += t
		totalMonth += m
		if u.RoleID == 0 {
			continue
		}
		list = append(list, map[string]any{
			"id": u.ID, "user": u.User, "status": u.Status, "flow": u.Flow, "num": u.Num,
			"usedFlow": used, "todayFlow": t, "monthFlow": m, "expTime": u.ExpTime,
			"forwardCount": forwardCount[u.ID], "flowResetTime": u.FlowResetTime,
		})
	}

	// 24 小时流量：全部用户按小时汇总
	flows := []map[string]any{}
	rows, err = db.Query("SELECT time, SUM(flow) FROM statistics_flow WHERE created_time > ? GROUP BY time", now.Add(-24*time.Hour).UnixMilli())
	must(err)
	for rows.Next() {
		var tm string
		var f int64
		if rows.Scan(&tm, &f) == nil {
			flows = append(flows, map[string]any{"time": tm, "flow": f})
		}
	}
	rows.Close()

	return ok(map[string]any{
		"nodeTotal": nodeTotal, "nodeOnline": nodeOnline,
		"totalUsedFlow": totalUsed, "todayFlow": totalToday, "monthFlow": totalMonth,
		"userCount": len(list), "users": list, "statisticsFlows": flows,
	})
}

// ---------- 面板指纹：8 位英文 + 数字，标识“哪个面板”，随数据库备份走 ----------
// 节点端安装时写进 config.json，重装时据此判断：同一个面板 → 覆盖更新；别的面板 → 共存安装。

const fingerprintKey = "panel_fingerprint"

var fingerprintRe = regexp.MustCompile(`^[A-Za-z0-9]{8}$`)

func newFingerprint() string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

// ensurePanelFingerprint 启动时调用：全新安装或旧版本升级上来时生成一次，之后保持不变
func ensurePanelFingerprint() {
	if fingerprintRe.MatchString(getSetting(fingerprintKey)) {
		return
	}
	setSetting(fingerprintKey, newFingerprint())
}

func panelFingerprint() string {
	ensurePanelFingerprint()
	return getSetting(fingerprintKey)
}

func fingerprintGet(c *Ctx) any {
	return ok(map[string]any{"fingerprint": panelFingerprint()})
}

func fingerprintSave(c *Ctx) any {
	var d struct {
		Fingerprint Str  `json:"fingerprint"`
		Regenerate  bool `json:"regenerate"`
	}
	c.bind(&d)
	fp := strings.TrimSpace(d.Fingerprint.V)
	if d.Regenerate {
		fp = newFingerprint()
	}
	if !fingerprintRe.MatchString(fp) {
		return fail("指纹必须是 8 位英文字母或数字")
	}
	setSetting(fingerprintKey, fp)
	return ok(map[string]any{"fingerprint": fp})
}

// ---------- 品牌：ForwardZ ----------

// releaseRepo 发布 Release（安装脚本、程序文件）的 GitHub 仓库。仓库改名后改这里即可（GitHub 会自动跳转旧地址）
const releaseRepo = "qqlikegi/flux-panel"

// migrateBranding 旧版本默认的应用名 flux 改为 ForwardZ；用户自己改过的名字不动
func migrateBranding() {
	mustExec("UPDATE vite_config SET value = 'ForwardZ', time = ? WHERE name = 'app_name' AND value = 'flux'", nowMs())
}

// defaultDBPath 未设置 DB_PATH 时的数据库位置：data/forwardz.db；改名前的 data/gost.db 仍存在时继续使用它
func defaultDBPath() string {
	const def, legacy = "data/forwardz.db", "data/gost.db"
	if _, err := os.Stat(def); err != nil {
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	return def
}
