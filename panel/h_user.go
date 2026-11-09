package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func userLogin(c *Ctx) any {
	var d struct {
		Username  Str `json:"username"`
		Password  Str `json:"password"`
		CaptchaID Str `json:"captchaId"`
	}
	c.bind(&d)
	notBlank(d.Username, "用户名不能为空")
	notBlank(d.Password, "密码不能为空")
	if needSetup() {
		return fail("系统尚未初始化，请刷新页面后先设置管理员账号")
	}

	if v, _ := getConfigValue("captcha_enabled"); v == "true" {
		if d.CaptchaID.Blank() || !captchaSecondaryVerify(d.CaptchaID.V) {
			return fail("验证码校验失败")
		}
	}
	u, err := queryOne(scanUser, "SELECT "+userCols+" FROM user WHERE user = ? LIMIT 1", d.Username.V)
	must(err)
	if u == nil || u.Pwd != md5Hex(d.Password.V) {
		return fail("账号或密码错误")
	}
	if u.Status == 0 {
		return fail("账号被停用")
	}
	return ok(map[string]any{
		"token":                 generateToken(u),
		"name":                  u.User,
		"role_id":               u.RoleID,
		"requirePasswordChange": false, // 首次部署时已由管理员自行设置账号密码
	})
}

type userForm struct {
	ID            Num `json:"id"`
	User          Str `json:"user"`
	Pwd           Str `json:"pwd"`
	Flow          Num `json:"flow"`
	Num           Num `json:"num"`
	ExpTime       Num `json:"expTime"`
	FlowResetTime Num `json:"flowResetTime"`
	Status        Num `json:"status"`
}

func (d *userForm) validate(update bool) {
	if update {
		notNull(d.ID, "用户ID不能为空")
	}
	notBlank(d.User, "用户名不能为空")
	if !update {
		notBlank(d.Pwd, "密码不能为空")
	}
	notNull(d.Flow, "流量不能为空")
	minVal(d.Flow, 0, "流量不能小于0")
	notNull(d.Num, "转发数量不能为空")
	minVal(d.Num, 0, "转发数量不能小于0")
	notNull(d.ExpTime, "过期时间不能为空")
	notNull(d.FlowResetTime, "流量重置时间不能为空")
}

func userCreate(c *Ctx) any {
	var d userForm
	c.bind(&d)
	d.validate(false)
	if countOf("SELECT COUNT(*) FROM user WHERE user = ?", d.User.V) > 0 {
		return fail("用户名已存在")
	}
	now := nowMs()
	mustExec(`INSERT INTO user (user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status)
		VALUES (?, ?, 1, ?, ?, 0, 0, ?, ?, ?, ?, 1)`,
		d.User.V, md5Hex(d.Pwd.V), d.ExpTime.Int(), d.Flow.Int(), d.FlowResetTime.Int(), d.Num.Int(), now, now)
	return ok()
}

func userList(c *Ctx) any {
	list, err := queryList(scanUser, "SELECT "+userCols+" FROM user WHERE role_id != 0")
	must(err)
	return ok(list)
}

func userUpdate(c *Ctx) any {
	var d userForm
	c.bind(&d)
	d.validate(true)
	u := getUser(d.ID.Int())
	if u == nil {
		return fail("用户不存在")
	}
	if u.RoleID == 0 {
		return fail("请不要作死")
	}
	if countOf("SELECT COUNT(*) FROM user WHERE user = ? AND id != ?", d.User.V, d.ID.Int()) > 0 {
		return fail("用户名已存在")
	}
	sets := []string{"user = ?", "flow = ?", "num = ?", "exp_time = ?", "flow_reset_time = ?", "updated_time = ?"}
	args := []any{d.User.V, d.Flow.Int(), d.Num.Int(), d.ExpTime.Int(), d.FlowResetTime.Int(), nowMs()}
	if !d.Pwd.Blank() {
		sets = append(sets, "pwd = ?")
		args = append(args, md5Hex(d.Pwd.V))
	}
	if d.Status.Valid {
		sets = append(sets, "status = ?")
		args = append(args, d.Status.Int())
	}
	args = append(args, d.ID.Int())
	mustExec("UPDATE user SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	// 修改了账号到期时间：同步到该用户已有的全部隧道权限（单独改某个隧道的到期时间请在“用户隧道”里编辑）
	if d.ExpTime.Int() != u.ExpTime {
		mustExec("UPDATE user_tunnel SET exp_time = ? WHERE user_id = ?", d.ExpTime.Int(), u.ID)
	}
	return ok()
}

func userDelete(c *Ctx) any {
	id := c.params().int("id")
	u := getUser(id)
	if u == nil {
		return fail("用户不存在")
	}
	if u.RoleID == 0 {
		return fail("请不要作死")
	}
	for _, f := range listForwards("WHERE user_id = ?", id) {
		deleteForwardInternal(f)
	}
	mustExec("DELETE FROM forward WHERE user_id = ?", id)
	mustExec("DELETE FROM user_tunnel WHERE user_id = ?", id)
	mustExec("DELETE FROM statistics_flow WHERE user_id = ?", id)
	mustExec("DELETE FROM user WHERE id = ?", id)
	return ok()
}

func userPackage(c *Ctx) any {
	u := getUser(c.userID())
	if u == nil {
		return fail("用户不存在")
	}
	userInfo := map[string]any{
		"id": u.ID, "name": nil, "user": u.User, "status": u.Status, "flow": u.Flow,
		"inFlow": u.InFlow, "outFlow": u.OutFlow, "num": u.Num, "expTime": u.ExpTime,
		"flowResetTime": u.FlowResetTime, "createdTime": u.CreatedTime, "updatedTime": u.UpdatedTime,
	}

	// 隧道权限
	rows, err := db.Query(`SELECT ut.id, ut.user_id, ut.tunnel_id, t.name, t.flow, ut.flow, ut.in_flow, ut.out_flow, ut.num,
			ut.flow_reset_time, ut.exp_time, ut.speed_id, sl.name, sl.speed
		FROM user_tunnel ut LEFT JOIN tunnel t ON ut.tunnel_id = t.id LEFT JOIN speed_limit sl ON ut.speed_id = sl.id
		WHERE ut.user_id = ? ORDER BY ut.id`, u.ID)
	must(err)
	tunnels := []map[string]any{}
	for rows.Next() {
		var id, userID, tunnelID, flow, inFlow, outFlow, num int64
		var tunnelName, speedName *string
		var tunnelFlow, resetTime, expTime, speedID, speed *int64
		if err := rows.Scan(&id, &userID, &tunnelID, &tunnelName, &tunnelFlow, &flow, &inFlow, &outFlow, &num, &resetTime, &expTime, &speedID, &speedName, &speed); err != nil {
			rows.Close()
			must(err)
		}
		tunnels = append(tunnels, map[string]any{
			"id": id, "userId": userID, "tunnelId": tunnelID, "tunnelName": tunnelName, "tunnelFlow": tunnelFlow,
			"flow": flow, "inFlow": inFlow, "outFlow": outFlow, "num": num, "flowResetTime": resetTime,
			"expTime": expTime, "speedId": speedID, "speedLimitName": speedName, "speed": speed,
		})
	}
	rows.Close()

	// 转发
	rows, err = db.Query(`SELECT f.id, f.name, f.tunnel_id, t.name, t.in_ip, f.remote_addr, f.in_flow, f.out_flow, f.status, f.created_time
		FROM forward f LEFT JOIN tunnel t ON f.tunnel_id = t.id WHERE f.user_id = ? ORDER BY f.created_time DESC`, u.ID)
	must(err)
	type fw struct {
		m        map[string]any
		id       int64
		tunnelID int64
	}
	var fws []fw
	for rows.Next() {
		var id, tunnelID, inFlow, outFlow, status, created int64
		var name, remote string
		var tunnelName, inIP *string
		if err := rows.Scan(&id, &name, &tunnelID, &tunnelName, &inIP, &remote, &inFlow, &outFlow, &status, &created); err != nil {
			rows.Close()
			must(err)
		}
		fws = append(fws, fw{id: id, tunnelID: tunnelID, m: map[string]any{
			"id": id, "name": name, "tunnelId": tunnelID, "tunnelName": tunnelName, "inIp": inIP, "inPort": nil,
			"remoteAddr": remote, "inFlow": inFlow, "outFlow": outFlow, "status": status, "createdTime": created,
		}})
	}
	rows.Close()
	forwards := []map[string]any{}
	for _, f := range fws {
		if inIP, inPort, found := forwardEntryAddress(f.id, f.tunnelID); found {
			f.m["inPort"] = inPort
			if inIP != "" {
				f.m["inIp"] = inIP
			}
		}
		forwards = append(forwards, f.m)
	}

	return ok(map[string]any{
		"userInfo":          userInfo,
		"tunnelPermissions": tunnels,
		"forwards":          forwards,
		"statisticsFlows":   last24HoursFlow(u.ID),
	})
}

// forwardEntryAddress 计算转发的入口地址：隧道配置了入口 IP 时取 IP×端口 的组合，否则用各入口节点的 IP:端口
func forwardEntryAddress(forwardID, tunnelID int64) (inIP string, inPort *int64, found bool) {
	tunnel := getTunnel(tunnelID)
	if tunnel == nil {
		return "", nil, false
	}
	ports := listForwardPorts("WHERE forward_id = ? ORDER BY id", forwardID)
	if len(ports) == 0 {
		return "", nil, false
	}
	var set []string
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			set = append(set, s)
		}
	}
	if strings.TrimSpace(tunnel.inIP()) != "" {
		var ips []string
		ipSeen := map[string]bool{}
		for _, ip := range strings.Split(tunnel.inIP(), ",") {
			ip = strings.TrimSpace(ip)
			if ip != "" && !ipSeen[ip] {
				ipSeen[ip] = true
				ips = append(ips, ip)
			}
		}
		var uniq []int64
		portSeen := map[int64]bool{}
		for _, p := range ports {
			if !portSeen[p.Port] {
				portSeen[p.Port] = true
				uniq = append(uniq, p.Port)
			}
		}
		for _, ip := range ips {
			for _, p := range uniq {
				add(ip + ":" + strconv.FormatInt(p, 10))
			}
		}
		if len(uniq) > 0 {
			inPort = &uniq[0]
		}
	} else {
		for _, p := range ports {
			if n := getNode(p.NodeID); n != nil {
				add(n.ServerIP + ":" + strconv.FormatInt(p.Port, 10))
			}
		}
		first := ports[0].Port
		inPort = &first
	}
	return strings.Join(set, ","), inPort, true
}

// last24HoursFlow 最近 24 条小时流量统计，不足 24 条时向前补零
func last24HoursFlow(userID int64) []*StatisticsFlow {
	list, err := queryList(func(s scanner) (*StatisticsFlow, error) {
		f := &StatisticsFlow{}
		return f, s.Scan(&f.ID, &f.UserID, &f.Flow, &f.TotalFlow, &f.Time, &f.CreatedTime)
	}, "SELECT id, user_id, flow, total_flow, time, created_time FROM statistics_flow WHERE user_id = ? ORDER BY id DESC LIMIT 24", userID)
	must(err)
	if len(list) < 24 {
		hour := time.Now().Hour()
		if len(list) > 0 {
			hour = parseHour(list[len(list)-1].Time) - 1
		}
		for len(list) < 24 {
			if hour < 0 {
				hour = 23
			}
			list = append(list, &StatisticsFlow{UserID: userID, Time: fmt.Sprintf("%02d:00", hour)})
			hour--
		}
	}
	return list
}

func parseHour(s string) int {
	if i := strings.Index(s, ":"); i > 0 {
		if h, err := strconv.Atoi(s[:i]); err == nil {
			return h
		}
	}
	return time.Now().Hour()
}

func userUpdatePassword(c *Ctx) any {
	var d struct {
		NewUsername     Str `json:"newUsername"`
		CurrentPassword Str `json:"currentPassword"`
		NewPassword     Str `json:"newPassword"`
		ConfirmPassword Str `json:"confirmPassword"`
	}
	c.bind(&d)
	notBlank(d.NewUsername, "新用户名不能为空")
	notBlank(d.CurrentPassword, "当前密码不能为空")
	notBlank(d.NewPassword, "新密码不能为空")
	notBlank(d.ConfirmPassword, "确认密码不能为空")
	u := getUser(c.userID())
	if u == nil {
		return fail("用户不存在")
	}
	if d.NewPassword.V != d.ConfirmPassword.V {
		return fail("新密码和确认密码不匹配")
	}
	if u.Pwd != md5Hex(d.CurrentPassword.V) {
		return fail("当前密码错误")
	}
	if u.User != d.NewUsername.V && countOf("SELECT COUNT(*) FROM user WHERE user = ? AND id != ?", d.NewUsername.V, u.ID) > 0 {
		return fail("用户名已存在")
	}
	mustExec("UPDATE user SET user = ?, pwd = ?, updated_time = ? WHERE id = ?", d.NewUsername.V, md5Hex(d.NewPassword.V), nowMs(), u.ID)
	return ok()
}

func userReset(c *Ctx) any {
	var d struct {
		ID   Num `json:"id"`
		Type Num `json:"type"`
	}
	c.bind(&d)
	notNull(d.ID, "重置账号id不能为空")
	notNull(d.Type, "重置类型不能为空")
	if d.Type.Int() == 1 {
		if getUser(d.ID.Int()) == nil {
			return fail("用户不存在")
		}
		mustExec("UPDATE user SET in_flow = 0, out_flow = 0 WHERE id = ?", d.ID.Int())
	} else {
		if getUserTunnelByID(d.ID.Int()) == nil {
			return fail("隧道不存在")
		}
		mustExec("UPDATE user_tunnel SET in_flow = 0, out_flow = 0 WHERE id = ?", d.ID.Int())
	}
	return ok()
}
