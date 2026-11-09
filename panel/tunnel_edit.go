package main

import (
	"fmt"
	"log"
	"strings"
)

// 编辑已有隧道：名称、类型、入口 / 中间跳 / 出口节点、协议、策略、流量计算、倍率、入口 IP 都可以改。
// 保存后以数据库为准重新计算相关节点（修改前后涉及的全部节点）应有的配置，只下发差异：
//   - 新加入的节点自动创建转发链 / 监听服务 / 限速器，被移除的节点自动清理；
//   - 该隧道下所有用户的转发自动迁移到新的入口节点（尽量沿用原端口）并指向新的转发链；
//   - 离线节点上线后自动对账补齐。

type tunnelEditForm struct {
	ID           Num           `json:"id"`
	Name         Str           `json:"name"`
	Type         Num           `json:"type"`
	Flow         Num           `json:"flow"`
	InIP         Str           `json:"inIp"`
	TrafficRatio Num           `json:"trafficRatio"`
	InNodeID     *[]chainForm  `json:"inNodeId"`
	ChainNodes   [][]chainForm `json:"chainNodes"`
	OutNodeID    []chainForm   `json:"outNodeId"`
}

func copyChain(ct *ChainTunnel) *ChainTunnel {
	n := *ct
	n.ID = nil
	return &n
}

func tunnelUpdate(c *Ctx) any {
	var d tunnelEditForm
	c.bind(&d)
	notNull(d.ID, "隧道ID不能为空")
	notBlank(d.Name, "隧道名称不能为空")
	notNull(d.Flow, "流量计算类型不能为空")
	validRatio(d.TrafficRatio)

	forwardMu.Lock()
	defer forwardMu.Unlock()
	reconcileMu.Lock()
	defer reconcileMu.Unlock()

	id := d.ID.Int()
	t := getTunnel(id)
	if t == nil {
		return fail("隧道不存在")
	}
	if countOf("SELECT COUNT(*) FROM tunnel WHERE name = ? AND id != ?", d.Name.V, id) > 0 {
		return fail("隧道名称重复")
	}
	oldChains := listChains("WHERE tunnel_id = ? ORDER BY id", id)

	// ---------- 新的节点结构 ----------
	tunnelType := t.Type
	var inNodes, outNodes []*ChainTunnel
	var hops [][]*ChainTunnel
	if d.InNodeID == nil {
		// 旧版前端只传名称等字段：节点结构保持不变
		in, h, out := groupChains(oldChains)
		for _, ct := range in {
			inNodes = append(inNodes, copyChain(ct))
		}
		for _, group := range h {
			var list []*ChainTunnel
			for _, ct := range group {
				list = append(list, copyChain(ct))
			}
			hops = append(hops, list)
		}
		for _, ct := range out {
			outNodes = append(outNodes, copyChain(ct))
		}
	} else {
		if d.Type.Valid {
			tunnelType = d.Type.Int()
		}
		if tunnelType != 1 && tunnelType != 2 {
			return fail("隧道类型错误")
		}
		for _, f := range *d.InNodeID {
			ct := f.toChain(1)
			ct.ChainType, ct.Port, ct.Inx = 1, nil, nil
			inNodes = append(inNodes, ct)
		}
		if tunnelType == 2 {
			for _, group := range d.ChainNodes {
				var list []*ChainTunnel
				for _, f := range group {
					ct := f.toChain(2)
					inx := int64(len(hops) + 1)
					ct.ChainType, ct.Port, ct.Inx = 2, nil, &inx
					list = append(list, ct)
				}
				if len(list) > 0 {
					hops = append(hops, list)
				}
			}
			for _, f := range d.OutNodeID {
				ct := f.toChain(3)
				ct.ChainType, ct.Port, ct.Inx = 3, nil, nil
				outNodes = append(outNodes, ct)
			}
		}
	}
	if len(inNodes) == 0 {
		return fail("入口不能为空")
	}
	if tunnelType == 2 && len(outNodes) == 0 {
		return fail("出口不能为空")
	}
	newChains := append([]*ChainTunnel{}, inNodes...)
	for _, h := range hops {
		newChains = append(newChains, h...)
	}
	newChains = append(newChains, outNodes...)

	nodes := map[int64]*Node{}
	seen := map[int64]bool{}
	for _, ct := range newChains {
		if seen[ct.NodeID] {
			return fail("节点重复：同一个节点在隧道中只能出现一次")
		}
		seen[ct.NodeID] = true
		n := getNode(ct.NodeID)
		if n == nil {
			return fail("节点不存在")
		}
		nodes[n.ID] = n
	}

	// ---------- 端口规划（全部算好再写库，不够用直接报错，不做任何修改） ----------
	oldHeld := map[int64]map[int64]bool{} // 本隧道原来在各节点上占用的转发链端口
	oldPort := map[int64]int64{}
	for _, ct := range oldChains {
		if ct.Port != nil && ct.ChainType != 1 {
			if oldHeld[ct.NodeID] == nil {
				oldHeld[ct.NodeID] = map[int64]bool{}
			}
			oldHeld[ct.NodeID][*ct.Port] = true
			oldPort[ct.NodeID] = *ct.Port
		}
	}
	usedCache := map[int64]map[int64]bool{}
	rangeCache := map[int64][]int64{}
	inRange := map[int64]map[int64]bool{}
	prepare := func(nid int64) {
		if usedCache[nid] != nil {
			return
		}
		u := usedPorts(nid, 0)
		for p := range oldHeld[nid] {
			delete(u, p)
		}
		usedCache[nid] = u
		rangeCache[nid] = parsePorts(nodes[nid].Port)
		inRange[nid] = map[int64]bool{}
		for _, p := range rangeCache[nid] {
			inRange[nid][p] = true
		}
	}
	take := func(nid, prefer int64) (int64, bool) {
		prepare(nid)
		u := usedCache[nid]
		if prefer > 0 && inRange[nid][prefer] && !u[prefer] {
			u[prefer] = true
			return prefer, true
		}
		for _, p := range rangeCache[nid] {
			if !u[p] {
				u[p] = true
				return p, true
			}
		}
		return 0, false
	}
	for _, ct := range newChains {
		if ct.ChainType == 1 {
			continue
		}
		p, ok := take(ct.NodeID, oldPort[ct.NodeID])
		if !ok {
			return fail(fmt.Sprintf("节点 %s 端口已满，无可用端口", nodes[ct.NodeID].Name))
		}
		ct.Port = &p
	}

	// 已有转发：移除的入口节点删掉端口，新加的入口节点分配端口（优先沿用原端口）
	entrySet := map[int64]bool{}
	for _, ct := range inNodes {
		entrySet[ct.NodeID] = true
	}
	type fpAdd struct{ forwardID, nodeID, port int64 }
	var adds []fpAdd
	var dels []int64
	forwards := listForwards("WHERE tunnel_id = ?", id)
	for _, f := range forwards {
		fps := listForwardPorts("WHERE forward_id = ? ORDER BY id", f.ID)
		have := map[int64]bool{}
		var prefer int64
		for _, fp := range fps {
			if entrySet[fp.NodeID] {
				have[fp.NodeID] = true
				if prefer == 0 {
					prefer = fp.Port
				}
			} else {
				dels = append(dels, fp.ID)
			}
		}
		if prefer == 0 && len(fps) > 0 {
			prefer = fps[0].Port
		}
		for _, ct := range inNodes {
			if have[ct.NodeID] {
				continue
			}
			p, ok := take(ct.NodeID, prefer)
			if !ok {
				return fail(fmt.Sprintf("入口节点 %s 的空闲端口不够分配给隧道下已有的 %d 个转发", nodes[ct.NodeID].Name, len(forwards)))
			}
			adds = append(adds, fpAdd{f.ID, ct.NodeID, p})
		}
	}

	// 入口 IP：原来是自动生成的（或留空），跟随新的入口节点；手动填写的保持手动值
	var ips []string
	for _, ct := range inNodes {
		ips = append(ips, nodes[ct.NodeID].ServerIP)
	}
	autoIP := strings.Join(ips, ",")
	oldAuto := t.inIP() == "" || t.inIP() == entryIPs(id)
	inIP := strings.TrimSpace(d.InIP.V)
	if inIP == "" || (oldAuto && inIP == t.inIP()) {
		inIP = autoIP
	}

	// ---------- 写库 ----------
	affectedSet := map[int64]bool{}
	for _, ct := range oldChains {
		affectedSet[ct.NodeID] = true
	}
	for _, ct := range newChains {
		affectedSet[ct.NodeID] = true
	}
	affected := make([]int64, 0, len(affectedSet))
	for nid := range affectedSet {
		affected = append(affected, nid)
	}
	before := desiredStates(affected)

	tx, err := db.Begin()
	must(err)
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	exec := func(q string, args ...any) {
		_, err := tx.Exec(q, args...)
		must(err)
	}
	sets := []string{"name = ?", "type = ?", "flow = ?", "in_ip = ?", "updated_time = ?"}
	args := []any{d.Name.V, tunnelType, d.Flow.Int(), inIP, nowMs()}
	if d.TrafficRatio.Valid {
		sets = append(sets, "traffic_ratio = ?")
		args = append(args, d.TrafficRatio.V)
	}
	args = append(args, id)
	exec("UPDATE tunnel SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	exec("DELETE FROM chain_tunnel WHERE tunnel_id = ?", id)
	for _, ct := range newChains {
		exec("INSERT INTO chain_tunnel (tunnel_id, chain_type, node_id, port, strategy, inx, protocol) VALUES (?, ?, ?, ?, ?, ?, ?)",
			id, ct.ChainType, ct.NodeID, ct.Port, ct.Strategy, ct.Inx, ct.Protocol)
	}
	for _, fpID := range dels {
		exec("DELETE FROM forward_port WHERE id = ?", fpID)
	}
	for _, a := range adds {
		exec("INSERT INTO forward_port (forward_id, node_id, port) VALUES (?, ?, ?)", a.forwardID, a.nodeID, a.port)
	}
	must(tx.Commit())
	committed = true
	log.Printf("隧道 %d 已修改：节点 %d 个，迁移转发端口 新增 %d / 删除 %d", id, len(newChains), len(adds), len(dels))

	// ---------- 实时下发 ----------
	if errs := pushChanges(before, desiredStates(affected)); len(errs) > 0 {
		return fail("隧道已保存，但以下配置同步失败（节点重新连接后会自动重试）：" + strings.Join(errs, "；"))
	}
	return ok()
}
