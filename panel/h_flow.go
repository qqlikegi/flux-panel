package main

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// flowMu 串行处理流量上报，保证同一用户/隧道的累计流量不丢失
var flowMu sync.Mutex

func nodeBySecret(secret string) *Node {
	if secret == "" {
		return nil
	}
	n, err := queryOne(scanNode, "SELECT "+nodeCols+" FROM node WHERE secret = ?", secret)
	if err != nil {
		return nil
	}
	return n
}

// flowUpload 节点上报流量：[{n: "转发id_用户id_用户隧道id", u: 上行, d: 下行}]
func flowUpload(c *Ctx) any {
	secret := c.Req.URL.Query().Get("secret")
	if nodeBySecret(secret) == nil {
		return text("ok")
	}
	data := decryptIfNeeded(c.Body, secret)
	var items []struct {
		N string `json:"n"`
		U Num    `json:"u"`
		D Num    `json:"d"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		log.Printf("流量数据解析失败: %v", err)
		return text("ok")
	}
	for _, it := range items {
		if it.N == "web_api" {
			continue
		}
		func() {
			defer func() {
				if e := recover(); e != nil {
					log.Printf("处理流量数据 %s 失败: %v", it.N, e)
				}
			}()
			processFlow(it.N, it.U.Int(), it.D.Int())
		}()
	}
	return text("ok")
}

func processFlow(serviceName string, up, down int64) {
	parts := strings.Split(serviceName, "_")
	if len(parts) < 3 {
		return
	}
	forwardID, err1 := strconv.ParseInt(parts[0], 10, 64)
	userID, err2 := strconv.ParseInt(parts[1], 10, 64)
	userTunnelID, err3 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return
	}

	// 流量倍率及单双向计算
	if f := getForward(forwardID); f != nil {
		if t := getTunnel(f.TunnelID); t != nil {
			down = int64(float64(down)*t.TrafficRatio) * t.Flow
			up = int64(float64(up)*t.TrafficRatio) * t.Flow
		}
	}

	flowMu.Lock()
	mustExec("UPDATE forward SET in_flow = in_flow + ?, out_flow = out_flow + ? WHERE id = ?", down, up, forwardID)
	mustExec("UPDATE user SET in_flow = in_flow + ?, out_flow = out_flow + ? WHERE id = ?", down, up, userID)
	if userTunnelID != 0 {
		mustExec("UPDATE user_tunnel SET in_flow = in_flow + ?, out_flow = out_flow + ? WHERE id = ?", down, up, userTunnelID)
	}
	flowMu.Unlock()

	if userTunnelID == 0 {
		return // 管理员的转发不做流量限制
	}
	checkUserLimits(userID)
	checkUserTunnelLimits(userTunnelID, userID)
}

// checkUserLimits 用户总流量超限 / 到期 / 被禁用时暂停该用户的所有转发
func checkUserLimits(userID int64) {
	u := getUser(userID)
	if u == nil {
		return
	}
	if u.Flow*bytesPerGB < u.InFlow+u.OutFlow || u.ExpTime <= nowMs() || u.Status != 1 {
		pauseForwards(listForwards("WHERE user_id = ? AND status = 1", userID))
	}
}

// checkUserTunnelLimits 隧道流量超限 / 到期 / 被禁用时暂停该用户在此隧道下的转发
func checkUserTunnelLimits(userTunnelID, userID int64) {
	ut := getUserTunnelByID(userTunnelID)
	if ut == nil {
		return
	}
	if ut.InFlow+ut.OutFlow >= ut.Flow*bytesPerGB || ut.ExpTime <= nowMs() || ut.Status != 1 {
		pauseForwards(listForwards("WHERE tunnel_id = ? AND user_id = ? AND status = 1", ut.TunnelID, userID))
	}
}

// pauseForwards 在入口节点上暂停转发服务，并把状态置为已暂停
func pauseForwards(list []*Forward) {
	for _, f := range list {
		name := serviceName(f.ID, f.UserID, getUserTunnel(f.UserID, f.TunnelID))
		for _, ct := range entryChains(f.TunnelID) {
			gostPauseResume(ct.NodeID, name, "PauseService")
		}
		mustExec("UPDATE forward SET status = 0 WHERE id = ?", f.ID)
	}
}

// flowConfig 节点上报当前配置（启动时、每次连上面板、之后每 10 分钟），按数据库对账：
// 缺的补上、不一致的更新、多余的删除、暂停状态纠正。换新机器重装后会自动恢复全部隧道和转发。
func flowConfig(c *Ctx) any {
	secret := c.Req.URL.Query().Get("secret")
	node := nodeBySecret(secret)
	if node == nil {
		return text("ok")
	}
	actual, err := stateFromReport(decryptIfNeeded(c.Body, secret))
	if err != nil {
		log.Printf("处理节点 %d 配置数据失败: %v", node.ID, err)
		return text("ok")
	}
	go func() {
		defer func() {
			if e := recover(); e != nil {
				log.Printf("节点 %d 配置对账异常: %v", node.ID, e)
			}
		}()
		reconcileNode(node.ID, actual)
	}()
	return text("ok")
}

// ---------- 定时任务 ----------

func startTasks() {
	// 每 5 分钟做一次 WAL checkpoint
	go func() {
		for range time.Tick(5 * time.Minute) {
			checkpoint()
		}
	}()
	// 每小时整点统计流量
	go func() {
		for {
			now := time.Now()
			next := now.Truncate(time.Hour).Add(time.Hour)
			time.Sleep(time.Until(next))
			runTask("流量统计", statisticsFlow)
		}
	}()
	// 每天 00:00:05 重置流量并处理到期账号/隧道
	go func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 5, 0, now.Location())
			if !next.After(now) {
				next = next.AddDate(0, 0, 1)
			}
			time.Sleep(time.Until(next))
			runTask("流量重置", resetFlowTask)
		}
	}()
}

func runTask(name string, fn func()) {
	defer func() {
		if e := recover(); e != nil {
			log.Printf("定时任务[%s]执行失败: %v", name, e)
		}
	}()
	fn()
}

func statisticsFlow() {
	now := time.Now()
	hour := now.Truncate(time.Hour).Format("15:04")
	day := now.Add(-time.Minute).Format("2006-01-02")
	ms := now.UnixMilli()
	mustExec("DELETE FROM statistics_flow WHERE created_time < ?", ms-48*3600*1000)
	users, err := queryList(scanUser, "SELECT "+userCols+" FROM user")
	must(err)
	for _, u := range users {
		total := u.InFlow + u.OutFlow
		inc := total
		var last int64
		if err := db.QueryRow("SELECT total_flow FROM statistics_flow WHERE user_id = ? ORDER BY id DESC LIMIT 1", u.ID).Scan(&last); err == nil {
			inc = total - last
			if inc < 0 {
				inc = total
			}
		}
		mustExec("INSERT INTO statistics_flow (user_id, flow, total_flow, time, created_time) VALUES (?, ?, ?, ?, ?)", u.ID, inc, total, hour, ms)
		// 整点统计的是上一个小时的流量，计入上一小时所在的日期
		if inc > 0 {
			mustExec(`INSERT INTO user_flow_daily (user_id, day, flow) VALUES (?, ?, ?)
				ON CONFLICT(user_id, day) DO UPDATE SET flow = flow + excluded.flow`, u.ID, day, inc)
		}
	}
	mustExec("DELETE FROM user_flow_daily WHERE day < ?", now.AddDate(0, 0, -400).Format("2006-01-02"))
}

func resetFlowTask() {
	now := time.Now()
	day := now.Day()
	lastDay := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location()).Day()
	// flow_reset_time: 0 不重置，1-31 每月几号重置；月末时重置日大于当月天数的也一并重置
	cond := "flow_reset_time != 0 AND flow_reset_time = ?"
	args := []any{day}
	if day == lastDay {
		cond = "flow_reset_time != 0 AND (flow_reset_time = ? OR flow_reset_time > ?)"
		args = append(args, lastDay)
	}
	r1 := mustExec("UPDATE user SET in_flow = 0, out_flow = 0 WHERE "+cond, args...)
	r2 := mustExec("UPDATE user_tunnel SET in_flow = 0, out_flow = 0 WHERE "+cond, args...)
	n1, _ := r1.RowsAffected()
	n2, _ := r2.RowsAffected()
	log.Printf("流量重置完成：用户 %d 个，用户隧道 %d 个", n1, n2)

	ms := nowMs()
	// 到期账号：暂停其所有转发并停用账号
	users, err := queryList(scanUser, "SELECT "+userCols+" FROM user WHERE role_id != 0 AND status = 1 AND exp_time < ?", ms)
	must(err)
	for _, u := range users {
		pauseForwards(listForwards("WHERE user_id = ? AND status = 1", u.ID))
		mustExec("UPDATE user SET status = 0 WHERE id = ?", u.ID)
	}
	// 到期隧道权限：暂停对应转发并停用权限
	uts, err := queryList(scanUserTunnel, "SELECT "+userTunnelCols+" FROM user_tunnel WHERE status = 1 AND exp_time < ?", ms)
	must(err)
	for _, ut := range uts {
		pauseForwards(listForwards("WHERE tunnel_id = ? AND user_id = ? AND status = 1", ut.TunnelID, ut.UserID))
		mustExec("UPDATE user_tunnel SET status = 0 WHERE id = ?", ut.ID)
	}
	log.Printf("到期处理完成：账号 %d 个，隧道权限 %d 个", len(users), len(uts))
}
