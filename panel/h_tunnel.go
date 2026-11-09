package main

import (
	"fmt"
	"sort"
	"strings"
)

type chainForm struct {
	NodeID    Num `json:"nodeId"`
	ChainType Num `json:"chainType"`
	Port      Num `json:"port"`
	Strategy  Str `json:"strategy"`
	Inx       Num `json:"inx"`
	Protocol  Str `json:"protocol"`
}

func (f chainForm) toChain(defaultType int64) *ChainTunnel {
	ct := &ChainTunnel{NodeID: f.NodeID.Int(), ChainType: defaultType, Port: f.Port.Ptr(), Strategy: f.Strategy.Ptr(), Inx: f.Inx.Ptr(), Protocol: f.Protocol.Ptr()}
	if f.ChainType.Valid {
		ct.ChainType = f.ChainType.Int()
	}
	return ct
}

func validRatio(n Num) {
	if n.Valid && (n.V <= 0 || n.V > 100) {
		if n.V <= 0 {
			panic(validErr("流量倍率必须大于0.0"))
		}
		panic(validErr("流量倍率不能大于100.0"))
	}
}

// nodePortForChain 为转发链/出口节点分配一个未被占用的端口
func nodePortForChain(nodeID int64, reserved map[int64]map[int64]bool) int64 {
	node := getNode(nodeID)
	if node == nil {
		throw("节点不存在")
	}
	used := usedPorts(nodeID, 0)
	for p := range reserved[nodeID] {
		used[p] = true
	}
	for _, p := range parsePorts(node.Port) {
		if !used[p] {
			return p
		}
	}
	throw("节点端口已满，无可用端口")
	return 0
}

// usedPorts 节点上已被隧道和转发占用的端口（excludeForwardID 对应转发的端口不计入）
func usedPorts(nodeID, excludeForwardID int64) map[int64]bool {
	used := map[int64]bool{}
	for _, ct := range listChains("WHERE node_id = ?", nodeID) {
		if ct.Port != nil {
			used[*ct.Port] = true
		}
	}
	for _, fp := range listForwardPorts("WHERE node_id = ? AND forward_id != ?", nodeID, excludeForwardID) {
		used[fp.Port] = true
	}
	return used
}

func tunnelCreate(c *Ctx) any {
	var d struct {
		Name         Str           `json:"name"`
		InNodeID     *[]chainForm  `json:"inNodeId"`
		ChainNodes   [][]chainForm `json:"chainNodes"`
		OutNodeID    []chainForm   `json:"outNodeId"`
		InIP         Str           `json:"inIp"`
		Type         Num           `json:"type"`
		Flow         Num           `json:"flow"`
		TrafficRatio Num           `json:"trafficRatio"`
	}
	c.bind(&d)
	forwardMu.Lock()
	defer forwardMu.Unlock()
	notBlank(d.Name, "隧道名称不能为空")
	if d.InNodeID == nil {
		panic(validErr("入口节点不能为空"))
	}
	notNull(d.Type, "隧道类型不能为空")
	notNull(d.Flow, "流量计算类型不能为空")
	validRatio(d.TrafficRatio)

	if countOf("SELECT COUNT(*) FROM tunnel WHERE name = ?", d.Name.V) > 0 {
		return fail("隧道名称重复")
	}
	if len(*d.InNodeID) == 0 {
		return fail("入口不能为空")
	}
	tunnelType := d.Type.Int()
	if tunnelType == 2 && len(d.OutNodeID) == 0 {
		return fail("出口不能为空")
	}

	nodes := map[int64]*Node{}
	var nodeIDs []int64
	loadNode := func(id int64) bool {
		n := getNode(id)
		if n == nil {
			return false
		}
		nodes[n.ID] = n
		nodeIDs = append(nodeIDs, id)
		return true
	}

	var inNodes, outNodes []*ChainTunnel
	var hops [][]*ChainTunnel
	for _, f := range *d.InNodeID {
		ct := f.toChain(1)
		if !loadNode(ct.NodeID) {
			return fail("节点不存在")
		}
		inNodes = append(inNodes, ct)
	}
	reserved := map[int64]map[int64]bool{}
	assignPort := func(ct *ChainTunnel) {
		p := nodePortForChain(ct.NodeID, reserved)
		if reserved[ct.NodeID] == nil {
			reserved[ct.NodeID] = map[int64]bool{}
		}
		reserved[ct.NodeID][p] = true
		ct.Port = &p
	}
	if tunnelType == 2 {
		for i, hop := range d.ChainNodes {
			var list []*ChainTunnel
			for _, f := range hop {
				ct := f.toChain(2)
				if !loadNode(ct.NodeID) {
					return fail("节点不存在")
				}
				inx := int64(i + 1)
				ct.Inx = &inx
				assignPort(ct)
				list = append(list, ct)
			}
			hops = append(hops, list)
		}
		for _, f := range d.OutNodeID {
			ct := f.toChain(3)
			if !loadNode(ct.NodeID) {
				return fail("节点不存在")
			}
			assignPort(ct)
			outNodes = append(outNodes, ct)
		}
	}
	seen := map[int64]bool{}
	for _, id := range nodeIDs {
		if seen[id] {
			return fail("节点重复")
		}
		seen[id] = true
	}
	for _, id := range nodeIDs {
		if nodes[id].Status != 1 {
			return fail("部分节点不在线")
		}
	}

	inIP := strings.TrimSpace(d.InIP.V)
	if !d.InIP.Valid || d.InIP.V == "" {
		var ips []string
		for _, ct := range inNodes {
			ips = append(ips, nodes[ct.NodeID].ServerIP)
		}
		inIP = strings.Join(ips, ",")
	} else {
		inIP = d.InIP.V
	}
	ratio := 1.0
	if d.TrafficRatio.Valid {
		ratio = d.TrafficRatio.V
	}
	now := nowMs()
	res := mustExec(`INSERT INTO tunnel (name, traffic_ratio, type, flow, created_time, updated_time, status, in_ip) VALUES (?, ?, ?, ?, ?, ?, 1, ?)`,
		d.Name.V, ratio, tunnelType, d.Flow.Int(), now, now, inIP)
	tunnelID, _ := res.LastInsertId()

	all := append([]*ChainTunnel{}, inNodes...)
	for _, h := range hops {
		all = append(all, h...)
	}
	all = append(all, outNodes...)
	for _, ct := range all {
		ct.TunnelID = tunnelID
		r := mustExec("INSERT INTO chain_tunnel (tunnel_id, chain_type, node_id, port, strategy, inx, protocol) VALUES (?, ?, ?, ?, ?, ?, ?)",
			tunnelID, ct.ChainType, ct.NodeID, ct.Port, ct.Strategy, ct.Inx, ct.Protocol)
		id, _ := r.LastInsertId()
		ct.ID = &id
	}

	if tunnelType != 2 {
		return ok()
	}
	// 下发配置：任何一步失败都整体回滚（删除已下发的 chain/service 和数据库记录）
	var chainOK, serviceOK []int64
	rollback := func(r *AgentResult) R {
		rollbackCreateTunnel(tunnelID, chainOK, serviceOK)
		return fail(r.Msg)
	}
	// 1. 入口节点：Chain 指向第一跳，没有中间跳时直接指向出口
	for _, in := range inNodes {
		next := outNodes
		if len(hops) > 0 {
			next = hops[0]
		}
		if r := agentAddChains(in.NodeID, next, nodes); !r.ok() {
			return rollback(r)
		}
		chainOK = append(chainOK, in.NodeID)
	}
	// 2. 中间跳：Chain 指向下一跳（最后一跳指向出口）+ 监听 Service
	for i, hop := range hops {
		next := outNodes
		if i+1 < len(hops) {
			next = hops[i+1]
		}
		for _, ct := range hop {
			if r := agentAddChains(ct.NodeID, next, nodes); !r.ok() {
				return rollback(r)
			}
			chainOK = append(chainOK, ct.NodeID)
			if r := agentAddChainService(ct.NodeID, ct, nodes); !r.ok() {
				return rollback(r)
			}
			serviceOK = append(serviceOK, ct.NodeID)
		}
	}
	// 3. 出口节点：监听 Service
	for _, out := range outNodes {
		if r := agentAddChainService(out.NodeID, out, nodes); !r.ok() {
			return rollback(r)
		}
		serviceOK = append(serviceOK, out.NodeID)
	}
	return ok()
}

func rollbackCreateTunnel(tunnelID int64, chainNodes, serviceNodes []int64) {
	for _, n := range chainNodes {
		agentDeleteChains(n, fmt.Sprintf("chains_%d", tunnelID))
	}
	for _, n := range serviceNodes {
		agentDeleteService(n, []string{fmt.Sprintf("%d_tls", tunnelID)})
	}
	mustExec("DELETE FROM chain_tunnel WHERE tunnel_id = ?", tunnelID)
	mustExec("DELETE FROM tunnel WHERE id = ?", tunnelID)
}

// groupChains 按类型拆分：入口、按 inx 排序的中间跳、出口
func groupChains(chains []*ChainTunnel) (in []*ChainTunnel, hops [][]*ChainTunnel, out []*ChainTunnel) {
	in, out = []*ChainTunnel{}, []*ChainTunnel{}
	byInx := map[int64][]*ChainTunnel{}
	for _, ct := range chains {
		switch ct.ChainType {
		case 1:
			in = append(in, ct)
		case 2:
			byInx[ct.inx()] = append(byInx[ct.inx()], ct)
		case 3:
			out = append(out, ct)
		}
	}
	keys := make([]int64, 0, len(byInx))
	for k := range byInx {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	hops = [][]*ChainTunnel{}
	for _, k := range keys {
		hops = append(hops, byInx[k])
	}
	return
}

func tunnelList(c *Ctx) any {
	tunnels, err := queryList(scanTunnel, "SELECT "+tunnelCols+" FROM tunnel")
	must(err)
	byTunnel := map[int64][]*ChainTunnel{}
	for _, ct := range listChains("ORDER BY id") {
		byTunnel[ct.TunnelID] = append(byTunnel[ct.TunnelID], ct)
	}
	out := []map[string]any{}
	for _, t := range tunnels {
		in, hops, outs := groupChains(byTunnel[t.ID])
		out = append(out, map[string]any{
			"id": t.ID, "name": t.Name, "type": t.Type, "flow": t.Flow, "trafficRatio": t.TrafficRatio,
			"status": t.Status, "createdTime": t.CreatedTime, "updatedTime": t.UpdatedTime, "inIp": t.InIP,
			"inNodeId": in, "chainNodes": hops, "outNodeId": outs,
		})
	}
	return ok(out)
}

func tunnelDelete(c *Ctx) any {
	id := c.params().int("id")
	forwardMu.Lock()
	defer forwardMu.Unlock()
	if getTunnel(id) == nil {
		return fail("隧道不存在")
	}
	deleteTunnelInternal(id)
	return ok()
}

// deleteTunnelInternal 删除隧道：先删转发，再删各节点上的 chain/service 和数据库记录
func deleteTunnelInternal(id int64) {
	if getTunnel(id) == nil {
		return
	}
	for _, f := range listForwards("WHERE tunnel_id = ?", id) {
		deleteForwardInternal(f)
	}
	mustExec("DELETE FROM forward WHERE tunnel_id = ?", id)
	mustExec("DELETE FROM user_tunnel WHERE tunnel_id = ?", id)
	mustExec("DELETE FROM tunnel WHERE id = ?", id)
	chainName := fmt.Sprintf("chains_%d", id)
	svc := []string{fmt.Sprintf("%d_tls", id)}
	for _, ct := range listChains("WHERE tunnel_id = ?", id) {
		switch ct.ChainType {
		case 1:
			agentDeleteChains(ct.NodeID, chainName)
		case 2:
			agentDeleteChains(ct.NodeID, chainName)
			agentDeleteService(ct.NodeID, svc)
		default:
			agentDeleteService(ct.NodeID, svc)
		}
	}
	mustExec("DELETE FROM chain_tunnel WHERE tunnel_id = ?", id)
}

func tunnelForUser(c *Ctx) any {
	if c.isAdmin() {
		list, err := queryList(scanTunnel, "SELECT "+tunnelCols+" FROM tunnel WHERE status = 1")
		must(err)
		return ok(list)
	}
	list, err := queryList(scanTunnel, "SELECT "+tunnelCols+" FROM tunnel WHERE status = 1 AND id IN (SELECT tunnel_id FROM user_tunnel WHERE user_id = ?)", c.userID())
	must(err)
	return ok(list)
}

// ---------- 诊断 ----------

type diagResult struct {
	NodeID        int64   `json:"nodeId"`
	NodeName      string  `json:"nodeName"`
	TargetIP      string  `json:"targetIp"`
	TargetPort    int64   `json:"targetPort"`
	Description   string  `json:"description"`
	Success       bool    `json:"success"`
	Message       string  `json:"message"`
	AverageTime   float64 `json:"averageTime"`
	PacketLoss    float64 `json:"packetLoss"`
	Timestamp     int64   `json:"timestamp"`
	FromChainType *int64  `json:"fromChainType"`
	FromInx       *int64  `json:"fromInx"`
	ToChainType   *int64  `json:"toChainType"`
	ToInx         *int64  `json:"toInx"`
}

func i64(v int64) *int64 { return &v }

func tcpPing(node *Node, ip string, port int64, desc string) *diagResult {
	res := &diagResult{NodeID: node.ID, NodeName: node.Name, TargetIP: ip, TargetPort: port, Description: desc, Timestamp: nowMs()}
	r := sendMsg(node.ID, map[string]any{"ip": ip, "port": port, "count": 4, "timeout": 5000}, "TcpPing")
	if r.ok() {
		if r.Data != nil {
			success, _ := r.Data["success"].(bool)
			res.Success = success
			if success {
				res.Message = "TCP连接成功"
				res.AverageTime, _ = r.Data["averageTime"].(float64)
				res.PacketLoss, _ = r.Data["packetLoss"].(float64)
			} else {
				res.Message, _ = r.Data["errorMessage"].(string)
				res.AverageTime, res.PacketLoss = -1, 100
			}
		} else {
			res.Success, res.Message = true, "TCP连接成功"
		}
	} else {
		res.Message = r.Msg
		res.AverageTime, res.PacketLoss = -1, 100
	}
	return res
}

// diagnoseChain 诊断入口 -> 中间跳 -> 出口之间的连通性；endpoint 用于生成首尾两端的检测目标
func diagnoseChain(tunnel *Tunnel, chains []*ChainTunnel, endpoints func(n *Node, from int64) []*diagResult) []*diagResult {
	in, hops, out := groupChains(chains)
	results := []*diagResult{}
	if tunnel.Type == 1 {
		for _, ct := range in {
			if n := getNode(ct.NodeID); n != nil {
				results = append(results, endpoints(n, 1)...)
			}
		}
		return results
	}
	link := func(from *Node, fromType int64, fromInx *int64, to *ChainTunnel, toType int64, desc string) {
		toNode := getNode(to.NodeID)
		if toNode == nil {
			return
		}
		r := tcpPing(from, toNode.ServerIP, to.port(), fmt.Sprintf(desc, toNode.Name))
		r.FromChainType, r.FromInx, r.ToChainType = i64(fromType), fromInx, i64(toType)
		if toType == 2 {
			r.ToInx = to.Inx
		}
		results = append(results, r)
	}
	for _, ct := range in {
		from := getNode(ct.NodeID)
		if from == nil {
			continue
		}
		if len(hops) > 0 {
			for _, to := range hops[0] {
				link(from, 1, nil, to, 2, "入口("+from.Name+")->第1跳(%s)")
			}
		} else {
			for _, to := range out {
				link(from, 1, nil, to, 3, "入口("+from.Name+")->出口(%s)")
			}
		}
	}
	for i, hop := range hops {
		for _, cur := range hop {
			from := getNode(cur.NodeID)
			if from == nil {
				continue
			}
			if i+1 < len(hops) {
				for _, to := range hops[i+1] {
					link(from, 2, cur.Inx, to, 2, fmt.Sprintf("第%d跳(%s)->第%d跳", i+1, from.Name, i+2)+"(%s)")
				}
			} else {
				for _, to := range out {
					link(from, 2, cur.Inx, to, 3, fmt.Sprintf("第%d跳(%s)->出口", i+1, from.Name)+"(%s)")
				}
			}
		}
	}
	for _, ct := range out {
		if n := getNode(ct.NodeID); n != nil {
			results = append(results, endpoints(n, 3)...)
		}
	}
	return results
}

func tunnelTypeName(t int64) string {
	if t == 1 {
		return "端口转发"
	}
	return "隧道转发"
}

func tunnelDiagnose(c *Ctx) any {
	id := c.params().int("tunnelId")
	tunnel := getTunnel(id)
	if tunnel == nil {
		return fail("隧道不存在")
	}
	chains := listChains("WHERE tunnel_id = ? ORDER BY id", id)
	if len(chains) == 0 {
		return fail("隧道配置不完整")
	}
	results := diagnoseChain(tunnel, chains, func(n *Node, from int64) []*diagResult {
		label := "入口"
		if from == 3 {
			label = "出口"
		}
		r := tcpPing(n, "www.google.com", 443, label+"("+n.Name+")->外网")
		r.FromChainType = i64(from)
		return []*diagResult{r}
	})
	return ok(map[string]any{
		"tunnelId": id, "tunnelName": tunnel.Name, "tunnelType": tunnelTypeName(tunnel.Type),
		"results": results, "timestamp": nowMs(),
	})
}

// ---------- 用户隧道权限 ----------

type userTunnelForm struct {
	ID            Num `json:"id"`
	UserID        Num `json:"userId"`
	TunnelID      Num `json:"tunnelId"`
	Flow          Num `json:"flow"`
	Num           Num `json:"num"`
	FlowResetTime Num `json:"flowResetTime"`
	ExpTime       Num `json:"expTime"`
	SpeedID       Num `json:"speedId"`
	Status        Num `json:"status"`
}

func userTunnelAssign(c *Ctx) any {
	var d userTunnelForm
	c.bind(&d)
	notNull(d.UserID, "用户ID不能为空")
	notNull(d.TunnelID, "隧道ID不能为空")
	notNull(d.Flow, "流量限制不能为空")
	minVal(d.Flow, 0, "流量限制不能小于0")
	notNull(d.Num, "转发数量不能为空")
	minVal(d.Num, 0, "转发数量不能小于0")
	notNull(d.FlowResetTime, "流量重置时间不能为空")
	notNull(d.ExpTime, "到期时间不能为空")
	if countOf("SELECT COUNT(*) FROM user_tunnel WHERE user_id = ? AND tunnel_id = ?", d.UserID.Int(), d.TunnelID.Int()) > 0 {
		return fail("该用户已拥有此隧道权限")
	}
	mustExec(`INSERT INTO user_tunnel (user_id, tunnel_id, speed_id, num, flow, in_flow, out_flow, flow_reset_time, exp_time, status)
		VALUES (?, ?, ?, ?, ?, 0, 0, ?, ?, 1)`,
		d.UserID.Int(), d.TunnelID.Int(), d.SpeedID.Ptr(), d.Num.Int(), d.Flow.Int(), d.FlowResetTime.Int(), d.ExpTime.Int())
	return ok()
}

func userTunnelList(c *Ctx) any {
	var d struct {
		UserID Num `json:"userId"`
	}
	c.bind(&d)
	if !d.UserID.Valid {
		panic(validErr("不能为null"))
	}
	rows, err := db.Query(`SELECT ut.id, ut.user_id, ut.tunnel_id, ut.flow, ut.in_flow, ut.out_flow, ut.num, ut.flow_reset_time, ut.exp_time,
			ut.speed_id, ut.status, t.name, t.flow, sl.name, sl.speed
		FROM user_tunnel ut LEFT JOIN tunnel t ON ut.tunnel_id = t.id LEFT JOIN speed_limit sl ON ut.speed_id = sl.id
		WHERE ut.user_id = ? ORDER BY ut.id`, d.UserID.Int())
	must(err)
	defer rows.Close()
	list := []map[string]any{}
	for rows.Next() {
		var id, userID, tunnelID, flow, inFlow, outFlow, num, resetTime, expTime, status int64
		var speedID, tunnelFlow, speed *int64
		var tunnelName, speedName *string
		if err := rows.Scan(&id, &userID, &tunnelID, &flow, &inFlow, &outFlow, &num, &resetTime, &expTime, &speedID, &status, &tunnelName, &tunnelFlow, &speedName, &speed); err != nil {
			must(err)
		}
		list = append(list, map[string]any{
			"id": id, "userId": userID, "tunnelId": tunnelID, "flow": flow, "num": num, "flowResetTime": resetTime,
			"expTime": expTime, "speedId": speedID, "speedLimitName": speedName, "speed": speed, "tunnelName": tunnelName,
			"tunnelFlow": tunnelFlow, "inFlow": inFlow, "outFlow": outFlow, "status": status,
		})
	}
	return ok(list)
}

func userTunnelRemove(c *Ctx) any {
	id := c.params().int("id")
	forwardMu.Lock()
	defer forwardMu.Unlock()
	ut := getUserTunnelByID(id)
	if ut == nil {
		return fail("未找到对应的用户隧道权限记录")
	}
	for _, f := range listForwards("WHERE user_id = ? AND tunnel_id = ?", ut.UserID, ut.TunnelID) {
		deleteForwardInternal(f)
	}
	mustExec("DELETE FROM user_tunnel WHERE id = ?", id)
	return ok()
}

func userTunnelUpdate(c *Ctx) any {
	var d userTunnelForm
	c.bind(&d)
	notNull(d.ID, "用户隧道权限ID不能为空")
	notNull(d.Flow, "流量限制不能为空")
	minVal(d.Flow, 0, "流量限制不能小于0")
	notNull(d.Num, "转发数量不能为空")
	minVal(d.Num, 0, "转发数量不能小于0")
	notNull(d.FlowResetTime, "流量重置时间不能为空")
	notNull(d.ExpTime, "到期时间不能为空")
	notNull(d.Status, "状态必选")
	ut := getUserTunnelByID(d.ID.Int())
	if ut == nil {
		return fail("隧道不存在")
	}
	newSpeed := d.SpeedID.Ptr()
	speedChanged := (ut.SpeedID == nil) != (newSpeed == nil) || (ut.SpeedID != nil && newSpeed != nil && *ut.SpeedID != *newSpeed)
	mustExec("UPDATE user_tunnel SET flow = ?, num = ?, flow_reset_time = ?, exp_time = ?, status = ?, speed_id = ? WHERE id = ?",
		d.Flow.Int(), d.Num.Int(), d.FlowResetTime.Int(), d.ExpTime.Int(), d.Status.Int(), newSpeed, ut.ID)
	if !speedChanged {
		return ok()
	}
	// 把新的限速规则同步到该用户在此隧道下的所有转发（端口与暂停状态保持不变）
	var failed []string
	for _, f := range listForwards("WHERE user_id = ? AND tunnel_id = ?", ut.UserID, ut.TunnelID) {
		if msg := syncForward(f, nil); msg != "" {
			failed = append(failed, f.Name+"："+msg)
		}
	}
	if len(failed) > 0 {
		return fail("权限已保存，但以下转发的限速同步失败：" + strings.Join(failed, "；"))
	}
	return ok()
}
