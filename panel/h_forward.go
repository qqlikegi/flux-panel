package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

const bytesPerGB = int64(1024 * 1024 * 1024)

// forwardMu 串行化同一转发的修改，避免并发修改时端口/配置错乱
var forwardMu sync.Mutex

type actor struct {
	userID int64
	roleID int64
	name   string
}

func actorOf(c *Ctx) actor { return actor{c.Claims.UserID, c.Claims.RoleID, c.Claims.Name} }

var adminActor = actor{roleID: 0}

func serviceName(forwardID, userID int64, ut *UserTunnel) string {
	var utID int64
	if ut != nil {
		utID = ut.ID
	}
	return fmt.Sprintf("%d_%d_%d", forwardID, userID, utID)
}

// ownForward 查询转发；普通用户只能操作自己的转发
func ownForward(id int64, a actor) *Forward {
	f := getForward(id)
	if f == nil || (a.roleID != 0 && a.userID != f.UserID) {
		return nil
	}
	return f
}

func forwardList(c *Ctx) any {
	q := `SELECT f.id, f.user_id, f.name, f.tunnel_id, f.remote_addr, f.status, f.created_time, f.updated_time, f.user_name,
			f.in_flow, f.out_flow, f.strategy, f.inx, t.name
		FROM forward f LEFT JOIN tunnel t ON f.tunnel_id = t.id`
	args := []any{}
	if !c.isAdmin() {
		q += " WHERE f.user_id = ?"
		args = append(args, c.userID())
	}
	rows, err := db.Query(q+" ORDER BY f.created_time DESC", args...)
	must(err)
	type item struct {
		m        map[string]any
		id       int64
		tunnelID int64
	}
	var items []item
	for rows.Next() {
		var id, userID, tunnelID, status, created, updated, inFlow, outFlow, inx int64
		var name, remote, userName, strategy string
		var tunnelName *string
		if err := rows.Scan(&id, &userID, &name, &tunnelID, &remote, &status, &created, &updated, &userName, &inFlow, &outFlow, &strategy, &inx, &tunnelName); err != nil {
			rows.Close()
			must(err)
		}
		items = append(items, item{id: id, tunnelID: tunnelID, m: map[string]any{
			"id": id, "name": name, "inIp": nil, "inPort": nil, "remoteAddr": remote, "status": status,
			"createdTime": created, "updatedTime": updated, "tunnelName": tunnelName, "userName": userName,
			"userId": userID, "tunnelId": tunnelID, "inFlow": inFlow, "outFlow": outFlow, "strategy": strategy, "inx": inx,
		}})
	}
	rows.Close()
	out := []map[string]any{}
	for _, it := range items {
		if inIP, inPort, found := forwardEntryAddress(it.id, it.tunnelID); found {
			it.m["inPort"] = inPort
			if inIP != "" {
				it.m["inIp"] = inIP
			}
		}
		out = append(out, it.m)
	}
	return ok(out)
}

// checkPermission 普通用户创建/修改转发前的权限与配额检查，返回限速器 ID 和用户隧道
func checkPermission(a actor, tunnel *Tunnel, excludeForwardID int64) (limiter *int64, ut *UserTunnel, errMsg string) {
	if a.roleID == 0 {
		return nil, nil, ""
	}
	u := getUser(a.userID)
	if u == nil {
		return nil, nil, "用户不存在"
	}
	now := nowMs()
	if u.ExpTime <= now {
		return nil, nil, "当前账号已到期"
	}
	ut = getUserTunnel(a.userID, tunnel.ID)
	if ut == nil {
		return nil, nil, "你没有该隧道权限"
	}
	if ut.Status != 1 {
		return nil, nil, "隧道被禁用"
	}
	if ut.ExpTime <= now {
		return nil, nil, "该隧道权限已到期"
	}
	if u.Flow <= 0 {
		return nil, nil, "用户总流量已用完"
	}
	if ut.Flow <= 0 {
		return nil, nil, "该隧道流量已用完"
	}
	if n := countOf("SELECT COUNT(*) FROM forward WHERE user_id = ? AND id != ?", a.userID, excludeForwardID); n >= u.Num {
		return nil, nil, fmt.Sprintf("用户总转发数量已达上限，当前限制：%d个", u.Num)
	}
	if n := countOf("SELECT COUNT(*) FROM forward WHERE user_id = ? AND tunnel_id = ? AND id != ?", a.userID, tunnel.ID, excludeForwardID); n >= ut.Num {
		return nil, nil, fmt.Sprintf("该隧道转发数量已达上限，当前限制：%d个", ut.Num)
	}
	return ut.SpeedID, ut, ""
}

// availablePorts 节点上可用于转发的端口
func availablePorts(nodeID, forwardID int64) []int64 {
	node := getNode(nodeID)
	if node == nil {
		throw("节点不存在")
	}
	used := usedPorts(nodeID, forwardID)
	var out []int64
	for _, p := range parsePorts(node.Port) {
		if !used[p] {
			out = append(out, p)
		}
	}
	return out
}

// pickPorts 为每个入口节点分配端口：指定端口时所有节点都必须可用；否则优先取所有节点都空闲的最小端口
func pickPorts(chains []*ChainTunnel, inPort *int64, forwardID int64) {
	var lists [][]int64
	for _, ct := range chains {
		ports := availablePorts(ct.NodeID, forwardID)
		if len(ports) == 0 {
			throw("暂无可用端口")
		}
		lists = append(lists, ports)
	}
	if inPort != nil {
		for _, ports := range lists {
			found := false
			for _, p := range ports {
				if p == *inPort {
					found = true
					break
				}
			}
			if !found {
				throw(fmt.Sprintf("指定端口 %d 不可用（并非所有节点都有此端口）", *inPort))
			}
		}
		for _, ct := range chains {
			p := *inPort
			ct.Port = &p
		}
		return
	}
	common := map[int64]bool{}
	for _, p := range lists[0] {
		common[p] = true
	}
	for _, ports := range lists[1:] {
		next := map[int64]bool{}
		for _, p := range ports {
			if common[p] {
				next[p] = true
			}
		}
		common = next
	}
	if len(common) > 0 {
		var min int64 = -1
		for p := range common {
			if min == -1 || p < min {
				min = p
			}
		}
		for _, ct := range chains {
			p := min
			ct.Port = &p
		}
		return
	}
	for i, ct := range chains {
		p := lists[i][0]
		ct.Port = &p
	}
}

type forwardForm struct {
	ID         Num `json:"id"`
	UserID     Num `json:"userId"`
	Name       Str `json:"name"`
	TunnelID   Num `json:"tunnelId"`
	RemoteAddr Str `json:"remoteAddr"`
	Strategy   Str `json:"strategy"`
	InPort     Num `json:"inPort"`
}

func forwardCreate(c *Ctx) any {
	var d forwardForm
	c.bind(&d)
	notBlank(d.Name, "转发名称不能为空")
	notNull(d.TunnelID, "隧道ID不能为空")
	notBlank(d.RemoteAddr, "远程地址不能为空")
	forwardMu.Lock()
	defer forwardMu.Unlock()

	a := actorOf(c)
	tunnel := getTunnel(d.TunnelID.Int())
	if tunnel == nil {
		return fail("隧道不存在")
	}
	if tunnel.Status != 1 {
		return fail("隧道已禁用，无法创建转发")
	}
	limiter, ut, msg := checkPermission(a, tunnel, 0)
	if msg != "" {
		return fail(msg)
	}
	chains := entryChains(tunnel.ID)
	if len(chains) == 0 {
		return fail("隧道没有入口节点")
	}
	pickPorts(chains, d.InPort.Ptr(), 0)
	nodes := map[int64]*Node{}
	for _, ct := range chains {
		n := getNode(ct.NodeID)
		if n == nil {
			return fail("部分节点不存在")
		}
		nodes[n.ID] = n
	}

	now := nowMs()
	strategy := d.Strategy.V
	if !d.Strategy.Valid {
		strategy = "fifo"
	}
	res := mustExec(`INSERT INTO forward (user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES (?, ?, ?, ?, ?, ?, 0, 0, ?, ?, 1, 0)`, a.userID, a.name, d.Name.V, tunnel.ID, d.RemoteAddr.V, strategy, now, now)
	id, _ := res.LastInsertId()
	f := getForward(id)
	name := serviceName(id, a.userID, ut)

	var okNodes []int64
	for _, ct := range chains {
		node := nodes[ct.NodeID]
		mustExec("INSERT INTO forward_port (forward_id, node_id, port) VALUES (?, ?, ?)", id, node.ID, ct.port())
		r := agentAddOrUpdateService(name, limiter, node, f, ct.port(), tunnel, "AddService")
		if !r.ok() {
			// 回滚：删除已下发到节点的服务和数据库记录
			for _, n := range okNodes {
				agentDeleteService(n, forwardServiceNames(name))
			}
			mustExec("DELETE FROM forward_port WHERE forward_id = ?", id)
			mustExec("DELETE FROM forward WHERE id = ?", id)
			return fail(r.Msg)
		}
		okNodes = append(okNodes, node.ID)
	}
	return ok()
}

func forwardUpdate(c *Ctx) any {
	var d forwardForm
	c.bind(&d)
	notNull(d.ID, "ID不能为空")
	notNull(d.UserID, "用户ID不能为空")
	notBlank(d.Name, "转发名称不能为空")
	notBlank(d.RemoteAddr, "远程地址不能为空")
	forwardMu.Lock()
	defer forwardMu.Unlock()

	a := actorOf(c)
	f := ownForward(d.ID.Int(), a)
	if f == nil {
		return fail("转发不存在")
	}
	f.Name, f.RemoteAddr = d.Name.V, d.RemoteAddr.V
	if d.Strategy.Valid {
		f.Strategy = d.Strategy.V
	}
	if msg := applyForwardUpdate(f, a, d.InPort.Ptr()); msg != "" {
		return fail(msg)
	}
	return ok()
}

// syncForward 按当前数据重新下发转发配置（限速规则变化后调用），端口与暂停状态保持不变
func syncForward(f *Forward, inPort *int64) (errMsg string) {
	forwardMu.Lock()
	defer forwardMu.Unlock()
	defer func() {
		if e := recover(); e != nil {
			if be, isBiz := e.(bizErr); isBiz {
				errMsg = string(be)
				return
			}
			panic(e)
		}
	}()
	return applyForwardUpdate(f, adminActor, inPort)
}

// applyForwardUpdate 保存转发修改并下发到各入口节点。f 中的名称/目标地址/策略为新值
func applyForwardUpdate(f *Forward, a actor, inPort *int64) string {
	tunnel := getTunnel(f.TunnelID)
	if tunnel == nil {
		return "隧道不存在"
	}
	if _, _, msg := checkPermission(a, tunnel, f.ID); msg != "" {
		return msg
	}
	ut := getUserTunnel(f.UserID, tunnel.ID)
	if a.roleID != 0 && ut == nil {
		return "你没有该隧道权限"
	}
	var limiter *int64
	if ut != nil {
		limiter = ut.SpeedID // 限速取转发所属用户的隧道权限
	}
	name := serviceName(f.ID, f.UserID, ut)
	chains := entryChains(tunnel.ID)
	if len(chains) == 0 {
		return "隧道没有入口节点"
	}

	existing := listForwardPorts("WHERE forward_id = ? ORDER BY id", f.ID)
	byNode := map[int64]*ForwardPort{}
	for _, p := range existing {
		byNode[p.NodeID] = p
	}
	var current *int64
	if len(existing) > 0 {
		current = &existing[0].Port
	}
	unchanged := inPort == nil || (current != nil && *inPort == *current)
	allHave := true
	for _, ct := range chains {
		if byNode[ct.NodeID] == nil {
			allHave = false
		}
	}
	if unchanged && allHave {
		for _, ct := range chains {
			p := byNode[ct.NodeID].Port
			ct.Port = &p
		}
	} else {
		pickPorts(chains, inPort, f.ID)
	}
	nodes := map[int64]*Node{}
	for _, ct := range chains {
		n := getNode(ct.NodeID)
		if n == nil {
			return "部分节点不存在"
		}
		nodes[n.ID] = n
	}

	wasPaused := f.Status == 0
	if f.Strategy == "" {
		f.Strategy = "fifo"
	}
	mustExec("UPDATE forward SET name = ?, remote_addr = ?, strategy = ?, updated_time = ? WHERE id = ?", f.Name, f.RemoteAddr, f.Strategy, nowMs(), f.ID)

	for _, ct := range chains {
		node := nodes[ct.NodeID]
		r := agentAddOrUpdateService(name, limiter, node, f, ct.port(), tunnel, "UpdateService")
		if strings.Contains(r.Msg, "not found") {
			r = agentAddOrUpdateService(name, limiter, node, f, ct.port(), tunnel, "AddService")
		}
		if !r.ok() {
			return r.Msg
		}
		if fp := byNode[node.ID]; fp != nil {
			mustExec("UPDATE forward_port SET port = ? WHERE id = ?", ct.port(), fp.ID)
		} else {
			mustExec("INSERT INTO forward_port (forward_id, node_id, port) VALUES (?, ?, ?)", f.ID, node.ID, ct.port())
		}
		if wasPaused {
			agentPauseResume(node.ID, name, "PauseService")
		}
	}
	return ""
}

func forwardDelete(c *Ctx) any {
	id := c.params().int("id")
	forwardMu.Lock()
	defer forwardMu.Unlock()
	f := ownForward(id, actorOf(c))
	if f == nil {
		return fail("转发不存在")
	}
	deleteForwardInternal(f)
	return ok()
}

// deleteForwardInternal 从实际下发过的节点上删除服务，再删数据库记录
func deleteForwardInternal(f *Forward) {
	name := serviceName(f.ID, f.UserID, getUserTunnel(f.UserID, f.TunnelID))
	var nodeIDs []int64
	seen := map[int64]bool{}
	for _, p := range listForwardPorts("WHERE forward_id = ? ORDER BY id", f.ID) {
		if !seen[p.NodeID] {
			seen[p.NodeID] = true
			nodeIDs = append(nodeIDs, p.NodeID)
		}
	}
	if len(nodeIDs) == 0 {
		for _, ct := range entryChains(f.TunnelID) {
			nodeIDs = append(nodeIDs, ct.NodeID)
		}
	}
	for _, n := range nodeIDs {
		agentDeleteService(n, forwardServiceNames(name))
	}
	mustExec("DELETE FROM forward_port WHERE forward_id = ?", f.ID)
	mustExec("DELETE FROM forward WHERE id = ?", f.ID)
}

func forwardForceDelete(c *Ctx) any {
	id := c.params().int("id")
	if ownForward(id, actorOf(c)) == nil {
		return fail("端口转发不存在")
	}
	mustExec("DELETE FROM forward WHERE id = ?", id)
	mustExec("DELETE FROM forward_port WHERE forward_id = ?", id)
	return ok()
}

func forwardPause(c *Ctx) any { return changeForwardStatus(c, c.params().int("id"), 0, "PauseService") }
func forwardResume(c *Ctx) any {
	return changeForwardStatus(c, c.params().int("id"), 1, "ResumeService")
}

func changeForwardStatus(c *Ctx, id int64, target int64, method string) any {
	a := actorOf(c)
	if a.roleID != 0 {
		u := getUser(a.userID)
		if u == nil {
			return fail("用户不存在")
		}
		if u.Status == 0 {
			return fail("用户已到期或被禁用")
		}
	}
	f := ownForward(id, a)
	if f == nil {
		return fail("转发不存在")
	}
	tunnel := getTunnel(f.TunnelID)
	if tunnel == nil {
		return fail("隧道不存在")
	}
	var ut *UserTunnel
	if target == 1 {
		if tunnel.Status != 1 {
			return fail("隧道已禁用，无法恢复服务")
		}
		if a.roleID != 0 {
			if msg := checkFlowLimits(a.userID, tunnel); msg != "" {
				return fail(msg)
			}
			ut = getUserTunnel(a.userID, tunnel.ID)
			if ut == nil {
				return fail("你没有该隧道权限")
			}
			if ut.Status != 1 {
				return fail("隧道被禁用")
			}
		}
	}
	if a.roleID != 0 && ut == nil {
		if ut = getUserTunnel(a.userID, tunnel.ID); ut == nil {
			return fail("你没有该隧道权限")
		}
	}
	if ut == nil {
		ut = getUserTunnel(f.UserID, tunnel.ID)
	}
	name := serviceName(f.ID, f.UserID, ut)
	for _, ct := range entryChains(tunnel.ID) {
		node := getNode(ct.NodeID)
		if node == nil {
			return fail("部分节点不存在")
		}
		if r := agentPauseResume(node.ID, name, method); !r.ok() {
			return fail(r.Msg)
		}
	}
	mustExec("UPDATE forward SET status = ?, updated_time = ? WHERE id = ?", target, nowMs(), f.ID)
	return ok()
}

func checkFlowLimits(userID int64, tunnel *Tunnel) string {
	u := getUser(userID)
	if u == nil {
		return "用户不存在"
	}
	now := nowMs()
	if u.ExpTime <= now {
		return "当前账号已到期"
	}
	ut := getUserTunnel(userID, tunnel.ID)
	if ut == nil {
		return "你没有该隧道权限"
	}
	if ut.ExpTime <= now {
		return "该隧道权限已到期，无法恢复服务"
	}
	if u.Flow*bytesPerGB <= u.InFlow+u.OutFlow {
		return "用户总流量已用完，无法恢复服务"
	}
	if ut.Flow*bytesPerGB <= ut.InFlow+ut.OutFlow {
		return "该隧道流量已用完，无法恢复服务"
	}
	return ""
}

func splitHostPort(addr string) (string, int64) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", -1
	}
	host := addr
	if strings.HasPrefix(addr, "[") {
		if end := strings.Index(addr, "]"); end > 1 {
			host = addr[1:end]
			if end+1 < len(addr) && addr[end+1] == ':' {
				p, err := strconv.ParseInt(addr[end+2:], 10, 64)
				if err != nil {
					return host, -1
				}
				return host, p
			}
		}
	}
	idx := strings.LastIndex(addr, ":")
	if idx <= 0 {
		return host, -1
	}
	if !strings.HasPrefix(addr, "[") {
		host = addr[:idx]
	}
	p, err := strconv.ParseInt(addr[idx+1:], 10, 64)
	if err != nil {
		return host, -1
	}
	return host, p
}

func forwardDiagnose(c *Ctx) any {
	id := c.params().int("forwardId")
	f := ownForward(id, actorOf(c))
	if f == nil {
		return fail("转发不存在")
	}
	tunnel := getTunnel(f.TunnelID)
	if tunnel == nil {
		return fail("隧道不存在")
	}
	chains := listChains("WHERE tunnel_id = ? ORDER BY id", tunnel.ID)
	if len(chains) == 0 {
		return fail("隧道配置不完整")
	}
	remotes := strings.Split(f.RemoteAddr, ",")
	results := diagnoseChain(tunnel, chains, func(n *Node, from int64) []*diagResult {
		label := "入口"
		if from == 3 {
			label = "出口"
		}
		var out []*diagResult
		for _, ra := range remotes {
			ip, port := splitHostPort(ra)
			if ip == "" || port == -1 {
				continue
			}
			r := tcpPing(n, ip, port, label+"("+n.Name+")->目标("+ra+")")
			r.FromChainType = i64(from)
			out = append(out, r)
		}
		return out
	})
	return ok(map[string]any{
		"forwardId": id, "forwardName": f.Name, "tunnelType": tunnelTypeName(tunnel.Type),
		"results": results, "timestamp": nowMs(),
	})
}

func forwardUpdateOrder(c *Ctx) any {
	p := c.params()
	raw, exists := p["forwards"]
	if !exists {
		return fail("缺少forwards参数")
	}
	list, _ := raw.([]any)
	if len(list) == 0 {
		return fail("forwards参数不能为空")
	}
	type order struct{ id, inx int64 }
	var orders []order
	for _, item := range list {
		m, isMap := item.(map[string]any)
		if !isMap {
			throw("forwards参数格式错误")
		}
		orders = append(orders, order{Params(m).int("id"), Params(m).int("inx")})
	}
	if !c.isAdmin() {
		ids := make([]string, len(orders))
		for i, o := range orders {
			ids[i] = strconv.FormatInt(o.id, 10)
		}
		n := countOf("SELECT COUNT(*) FROM forward WHERE user_id = ? AND id IN ("+strings.Join(ids, ",")+")", c.userID())
		if n != int64(len(orders)) {
			return fail("只能更新自己的转发排序")
		}
	}
	tx, err := db.Begin()
	must(err)
	for _, o := range orders {
		if _, err := tx.Exec("UPDATE forward SET inx = ? WHERE id = ?", o.inx, o.id); err != nil {
			_ = tx.Rollback()
			must(err)
		}
	}
	must(tx.Commit())
	return ok()
}

// ---------- 限速规则 ----------

func speedCreate(c *Ctx) any {
	var d struct {
		Name       Str `json:"name"`
		Speed      Num `json:"speed"`
		TunnelID   Num `json:"tunnelId"`
		TunnelName Str `json:"tunnelName"`
	}
	c.bind(&d)
	notBlank(d.Name, "限速规则名称不能为空")
	notNull(d.Speed, "速度限制不能为空")
	minVal(d.Speed, 1, "速度限制必须大于0")
	notNull(d.TunnelID, "隧道ID不能为空")
	notBlank(d.TunnelName, "隧道名称不能为空")
	if getTunnel(d.TunnelID.Int()) == nil {
		return fail("隧道不存在")
	}
	now := nowMs()
	res := mustExec("INSERT INTO speed_limit (name, speed, tunnel_id, tunnel_name, created_time, updated_time, status) VALUES (?, ?, ?, ?, ?, ?, 1)",
		d.Name.V, d.Speed.Int(), d.TunnelID.Int(), d.TunnelName.V, now, now)
	id, _ := res.LastInsertId()
	speed := speedToMBps(d.Speed.Int())
	var okNodes []int64
	for _, nodeID := range tunnelNodeIDs(d.TunnelID.Int()) {
		r := agentAddLimiter(nodeID, id, speed)
		if !r.ok() {
			mustExec("DELETE FROM speed_limit WHERE id = ?", id)
			for _, n := range okNodes {
				agentDeleteLimiter(n, id)
			}
			return fail(r.Msg)
		}
		okNodes = append(okNodes, nodeID)
	}
	return ok()
}

// tunnelNodeIDs 隧道涉及的所有（仍存在的）节点
func tunnelNodeIDs(tunnelID int64) []int64 {
	var ids []int64
	for _, ct := range listChains("WHERE tunnel_id = ? ORDER BY id", tunnelID) {
		if getNode(ct.NodeID) != nil {
			ids = append(ids, ct.NodeID)
		}
	}
	return ids
}

func speedList(c *Ctx) any {
	list, err := queryList(scanSpeed, "SELECT "+speedCols+" FROM speed_limit")
	must(err)
	return ok(list)
}

func speedUpdate(c *Ctx) any {
	var d struct {
		ID    Num `json:"id"`
		Name  Str `json:"name"`
		Speed Num `json:"speed"`
	}
	c.bind(&d)
	notNull(d.ID, "ID不能为空")
	notBlank(d.Name, "限速规则名称不能为空")
	notNull(d.Speed, "速度限制不能为空")
	minVal(d.Speed, 1, "速度限制必须大于0")
	l := getSpeedLimit(d.ID.Int())
	if l == nil {
		return fail("限速不存在")
	}
	speed := speedToMBps(d.Speed.Int())
	for _, nodeID := range tunnelNodeIDs(l.TunnelID) {
		if r := agentUpdateLimiter(nodeID, l.ID, speed); !r.ok() {
			return fail(r.Msg)
		}
	}
	mustExec("UPDATE speed_limit SET name = ?, speed = ?, updated_time = ? WHERE id = ?", d.Name.V, d.Speed.Int(), nowMs(), l.ID)
	return ok()
}

func speedDelete(c *Ctx) any {
	id := c.params().int("id")
	l := getSpeedLimit(id)
	if l == nil {
		return fail("限速规则不存在")
	}
	if countOf("SELECT COUNT(*) FROM user_tunnel WHERE speed_id = ?", id) != 0 {
		return fail("该限速规则还有用户在使用 请先取消分配")
	}
	for _, nodeID := range tunnelNodeIDs(l.TunnelID) {
		if r := agentDeleteLimiter(nodeID, id); !r.ok() {
			return fail(r.Msg)
		}
	}
	mustExec("DELETE FROM speed_limit WHERE id = ?", id)
	return ok()
}

// ---------- 订阅信息（sub-store） ----------

func openSubStore(c *Ctx) any {
	q := c.Req.URL.Query()
	user, pwd, tunnel := q.Get("user"), q.Get("pwd"), q.Get("tunnel")
	if tunnel == "" {
		tunnel = "-1"
	}
	if user == "" {
		return fail("用户不能为空")
	}
	if pwd == "" {
		return fail("密码不能为空")
	}
	u, err := queryOne(scanUser, "SELECT "+userCols+" FROM user WHERE user = ? LIMIT 1", user)
	must(err)
	if u == nil || u.Pwd != md5Hex(pwd) {
		return fail("鉴权失败")
	}
	// 与 Java 版保持一致：upload 取下行流量、download 取上行流量
	header := func(upload, download, total, expire int64) string {
		return fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", download, upload, total, expire)
	}
	var value string
	if tunnel == "-1" {
		value = header(u.OutFlow, u.InFlow, u.Flow*bytesPerGB, u.ExpTime/1000)
	} else {
		tid, err := strconv.ParseInt(tunnel, 10, 64)
		if err != nil {
			return fail("隧道不存在")
		}
		ut := getUserTunnelByID(tid)
		if ut == nil || ut.UserID != u.ID {
			return fail("隧道不存在")
		}
		value = header(ut.OutFlow, ut.InFlow, ut.Flow*bytesPerGB, ut.ExpTime/1000)
	}
	c.W.Header().Set("subscription-userinfo", value)
	return text(value)
}
