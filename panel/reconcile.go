package main

// 节点配置对账：以数据库为准，计算每个节点“应该有”的限速器 / 转发链 / 服务，
// 与节点实际配置（或修改前的期望配置）逐项比较，只下发有差异的部分。
//
// 触发时机：
//   - 节点上报配置（/flow/config：节点端启动、每次连上面板、之后每 10 分钟）——
//     换了新机器重新安装、配置丢失、离线期间面板有改动，节点一上线就自动恢复；
//   - 修改节点（IP、端口范围、监听地址、网卡）——相关节点立即重建受影响的隧道和转发。

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type nodeState struct {
	limiters map[string]map[string]any
	chains   map[string]map[string]any
	services map[string]map[string]any
	paused   map[string]bool
}

func newNodeState() *nodeState {
	return &nodeState{
		limiters: map[string]map[string]any{},
		chains:   map[string]map[string]any{},
		services: map[string]map[string]any{},
		paused:   map[string]bool{},
	}
}

// reconcileMu 同一时间只对账一个节点，避免与自身并发
var reconcileMu sync.Mutex

// desiredStates 计算指定节点（nil 表示全部节点）应有的配置
func desiredStates(nodeIDs []int64) map[int64]*nodeState {
	nodeList, err := queryList(scanNode, "SELECT "+nodeCols+" FROM node")
	must(err)
	nodes := map[int64]*Node{}
	for _, n := range nodeList {
		nodes[n.ID] = n
	}
	states := map[int64]*nodeState{}
	if nodeIDs == nil {
		for id := range nodes {
			states[id] = newNodeState()
		}
	} else {
		for _, id := range nodeIDs {
			states[id] = newNodeState()
		}
	}
	get := func(id int64) *nodeState { return states[id] } // 不关心的节点返回 nil

	tunnelList, err := queryList(scanTunnel, "SELECT "+tunnelCols+" FROM tunnel")
	must(err)
	tunnels := map[int64]*Tunnel{}
	for _, t := range tunnelList {
		tunnels[t.ID] = t
	}
	chainsByTunnel := map[int64][]*ChainTunnel{}
	for _, ct := range listChains("ORDER BY id") {
		chainsByTunnel[ct.TunnelID] = append(chainsByTunnel[ct.TunnelID], ct)
	}
	allExist := func(list []*ChainTunnel) bool {
		for _, ct := range list {
			if nodes[ct.NodeID] == nil {
				return false
			}
		}
		return len(list) > 0
	}

	// 隧道转发：入口/中间跳的转发链，中间跳/出口的监听服务
	for tid, chains := range chainsByTunnel {
		t := tunnels[tid]
		if t == nil || t.Type != 2 {
			continue
		}
		in, hops, outs := groupChains(chains)
		if !allExist(outs) {
			continue
		}
		nextOf := func(i int) []*ChainTunnel { // 第 i 跳（-1 为入口）的下一跳
			if i+1 < len(hops) {
				return hops[i+1]
			}
			return outs
		}
		for _, ct := range in {
			if s := get(ct.NodeID); s != nil && nodes[ct.NodeID] != nil && allExist(nextOf(-1)) {
				d := chainData(ct.NodeID, nextOf(-1), nodes)
				s.chains[d["name"].(string)] = d
			}
		}
		for i, hop := range hops {
			for _, ct := range hop {
				s := get(ct.NodeID)
				if s == nil || nodes[ct.NodeID] == nil {
					continue
				}
				if allExist(nextOf(i)) {
					d := chainData(ct.NodeID, nextOf(i), nodes)
					s.chains[d["name"].(string)] = d
				}
				svc := chainServiceData(ct.NodeID, ct, nodes)
				s.services[svc["name"].(string)] = svc
			}
		}
		for _, ct := range outs {
			if s := get(ct.NodeID); s != nil {
				svc := chainServiceData(ct.NodeID, ct, nodes)
				s.services[svc["name"].(string)] = svc
			}
		}
	}

	// 限速器：下发到隧道涉及的所有节点（与新建限速时一致）
	speeds, err := queryList(scanSpeed, "SELECT "+speedCols+" FROM speed_limit")
	must(err)
	for _, sp := range speeds {
		for _, ct := range chainsByTunnel[sp.TunnelID] {
			if s := get(ct.NodeID); s != nil && nodes[ct.NodeID] != nil {
				d := limiterData(sp.ID, speedToMBps(sp.Speed))
				s.limiters[d["name"].(string)] = d
			}
		}
	}

	// 转发：入口节点上的 tcp/udp 服务，已暂停的转发保持暂停
	uts, err := queryList(scanUserTunnel, "SELECT "+userTunnelCols+" FROM user_tunnel")
	must(err)
	utByKey := map[[2]int64]*UserTunnel{}
	for _, ut := range uts {
		utByKey[[2]int64{ut.UserID, ut.TunnelID}] = ut
	}
	portsByForward := map[int64]map[int64]int64{}
	for _, fp := range listForwardPorts("") {
		if portsByForward[fp.ForwardID] == nil {
			portsByForward[fp.ForwardID] = map[int64]int64{}
		}
		portsByForward[fp.ForwardID][fp.NodeID] = fp.Port
	}
	for _, f := range listForwards("") {
		t := tunnels[f.TunnelID]
		if t == nil {
			continue
		}
		ut := utByKey[[2]int64{f.UserID, f.TunnelID}]
		var limiter *int64
		if ut != nil {
			limiter = ut.SpeedID
		}
		name := serviceName(f.ID, f.UserID, ut)
		for _, ct := range chainsByTunnel[f.TunnelID] {
			if ct.ChainType != 1 {
				continue
			}
			s := get(ct.NodeID)
			node := nodes[ct.NodeID]
			port, hasPort := portsByForward[f.ID][ct.NodeID]
			if s == nil || node == nil || !hasPort {
				continue
			}
			for _, svc := range forwardServicesData(name, limiter, node, f, port, t) {
				n := svc["name"].(string)
				s.services[n] = svc
				s.paused[n] = f.Status == 0
			}
		}
	}
	return states
}

// stateFromReport 把节点上报的 gost 配置转换为 nodeState
func stateFromReport(raw []byte) (*nodeState, error) {
	var cfg struct {
		Limiters []map[string]any `json:"limiters"`
		Chains   []map[string]any `json:"chains"`
		Services []map[string]any `json:"services"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	s := newNodeState()
	for _, m := range cfg.Limiters {
		s.limiters[str(m["name"])] = m
	}
	for _, m := range cfg.Chains {
		s.chains[str(m["name"])] = m
	}
	for _, m := range cfg.Services {
		n := str(m["name"])
		s.services[n] = m
		if md, ok := m["metadata"].(map[string]any); ok && md["paused"] == true {
			s.paused[n] = true
		}
	}
	return s, nil
}

// ---------- 指纹：只比较面板下发的关键字段，忽略节点端补充的状态/默认值 ----------

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// generic 把 map 统一成 json 解码后的形式，便于期望配置和上报配置用同一套规则取值
func generic(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func sub(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

func list(m map[string]any, key string) []map[string]any {
	var out []map[string]any
	if arr, ok := m[key].([]any); ok {
		for _, v := range arr {
			if mm, ok := v.(map[string]any); ok {
				out = append(out, mm)
			}
		}
	}
	return out
}

func serviceFP(raw map[string]any) string {
	m := generic(raw)
	var targets []string
	for _, n := range list(sub(m, "forwarder"), "nodes") {
		targets = append(targets, str(n["addr"]))
	}
	return strings.Join([]string{
		str(m["addr"]), str(m["limiter"]),
		str(sub(m, "handler")["type"]), str(sub(m, "handler")["chain"]),
		str(sub(m, "listener")["type"]),
		strings.Join(targets, ","), str(sub(sub(m, "forwarder"), "selector")["strategy"]),
		str(sub(m, "metadata")["interface"]),
	}, "|")
}

func chainFP(raw map[string]any) string {
	m := generic(raw)
	var parts []string
	for _, hop := range list(m, "hops") {
		parts = append(parts, "hop:"+str(sub(hop, "selector")["strategy"])+":"+str(hop["interface"]))
		for _, n := range list(hop, "nodes") {
			parts = append(parts, str(n["addr"])+":"+str(sub(n, "dialer")["type"])+":"+str(sub(n, "connector")["type"]))
		}
	}
	return strings.Join(parts, "|")
}

func limiterFP(raw map[string]any) string {
	m := generic(raw)
	var limits []string
	if arr, ok := m["limits"].([]any); ok {
		for _, v := range arr {
			limits = append(limits, str(v))
		}
	}
	return strings.Join(limits, ",")
}

// 面板管理的配置名称；不符合的（如 web_api）不会被删除
var (
	panelServiceRe = regexp.MustCompile(`^(\d+_\d+_\d+_(tcp|udp)|\d+_tls)$`)
	panelChainRe   = regexp.MustCompile(`^chains_\d+$`)
	panelLimiterRe = regexp.MustCompile(`^\d+$`)
)

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// applyDiff 把节点从 have 调整到 want，返回失败信息（成功为空）
func applyDiff(nodeID int64, have, want *nodeState) []string {
	var errs []string
	changed := 0
	note := func(action, name string, r *GostResult) bool {
		changed++
		if r.ok() {
			return true
		}
		errs = append(errs, fmt.Sprintf("%s %s: %s", action, name, r.Msg))
		return false
	}

	// 1. 限速器（服务会引用，先建）
	for _, name := range sortedKeys(want.limiters) {
		d := want.limiters[name]
		if old, exists := have.limiters[name]; !exists {
			note("创建限速器", name, okIf(sendMsg(nodeID, d, "AddLimiters"), "exists"))
		} else if limiterFP(old) != limiterFP(d) {
			r := sendMsg(nodeID, map[string]any{"limiter": name, "data": d}, "UpdateLimiters")
			if strings.Contains(r.Msg, "not found") {
				r = okIf(sendMsg(nodeID, d, "AddLimiters"), "exists")
			}
			note("更新限速器", name, r)
		}
	}
	// 2. 转发链（服务会引用，先建）
	for _, name := range sortedKeys(want.chains) {
		d := want.chains[name]
		if old, exists := have.chains[name]; !exists {
			note("创建转发链", name, okIf(sendMsg(nodeID, d, "AddChains"), "exists"))
		} else if chainFP(old) != chainFP(d) {
			r := sendMsg(nodeID, map[string]any{"chain": name, "data": d}, "UpdateChains")
			if strings.Contains(r.Msg, "not found") {
				r = okIf(sendMsg(nodeID, d, "AddChains"), "exists")
			}
			note("更新转发链", name, r)
		}
	}
	// 3. 服务
	for _, name := range sortedKeys(want.services) {
		d := want.services[name]
		old, exists := have.services[name]
		pushed := false
		if !exists {
			if !note("创建服务", name, okIf(sendMsg(nodeID, []any{d}, "AddService"), "exists")) {
				continue
			}
			pushed = true
		} else if serviceFP(old) != serviceFP(d) {
			r := sendMsg(nodeID, []any{d}, "UpdateService")
			if strings.Contains(r.Msg, "not found") {
				r = okIf(sendMsg(nodeID, []any{d}, "AddService"), "exists")
			}
			if !note("更新服务", name, r) {
				continue
			}
			pushed = true
		}
		// 新建/更新后的服务处于运行状态；其余按上报的暂停状态比较
		running := pushed || !have.paused[name]
		if want.paused[name] && running {
			note("暂停服务", name, sendMsg(nodeID, map[string]any{"services": []string{name}}, "PauseService"))
		} else if !want.paused[name] && !running {
			note("恢复服务", name, sendMsg(nodeID, map[string]any{"services": []string{name}}, "ResumeService"))
		}
	}
	// 4. 删除多余的配置：服务 -> 转发链 -> 限速器
	var extraSvc []string
	for _, name := range sortedKeys(have.services) {
		if _, ok := want.services[name]; !ok && panelServiceRe.MatchString(name) {
			extraSvc = append(extraSvc, name)
		}
	}
	if len(extraSvc) > 0 {
		note("删除服务", strings.Join(extraSvc, ","), okIf(sendMsg(nodeID, map[string]any{"services": extraSvc}, "DeleteService"), "not found"))
	}
	for _, name := range sortedKeys(have.chains) {
		if _, ok := want.chains[name]; !ok && panelChainRe.MatchString(name) {
			note("删除转发链", name, okIf(sendMsg(nodeID, map[string]any{"chain": name}, "DeleteChains"), "not found"))
		}
	}
	for _, name := range sortedKeys(have.limiters) {
		if _, ok := want.limiters[name]; !ok && panelLimiterRe.MatchString(name) {
			note("删除限速器", name, okIf(sendMsg(nodeID, map[string]any{"limiter": name}, "DeleteLimiters"), "not found"))
		}
	}
	if changed > 0 {
		log.Printf("节点 %d 配置同步：%d 项变更，%d 项失败", nodeID, changed, len(errs))
	}
	return errs
}

// reconcileNode 按节点上报的实际配置对账
func reconcileNode(nodeID int64, actual *nodeState) {
	forwardMu.Lock()
	defer forwardMu.Unlock()
	reconcileMu.Lock()
	defer reconcileMu.Unlock()
	want := desiredStates([]int64{nodeID})[nodeID]
	if want == nil {
		return
	}
	for _, e := range applyDiff(nodeID, actual, want) {
		log.Printf("节点 %d 配置同步失败: %s", nodeID, e)
	}
}

// pushChanges 数据库变更前后的期望配置对比，把差异下发到在线节点；离线节点上线后会自动对账
func pushChanges(before, after map[int64]*nodeState) []string {
	var errs []string
	ids := make([]int64, 0, len(after))
	for id := range after {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if !nodeOnline(id) {
			continue
		}
		b := before[id]
		if b == nil {
			b = newNodeState()
		}
		for _, e := range applyDiff(id, b, after[id]) {
			errs = append(errs, fmt.Sprintf("节点%d %s", id, e))
		}
	}
	return errs
}

func nodeOnline(id int64) bool {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	return hub.nodes[id] != nil
}

// relatedNodeIDs 与该节点共用隧道的所有节点（含自身）
func relatedNodeIDs(nodeID int64) []int64 {
	set := map[int64]bool{nodeID: true}
	rows, err := db.Query(`SELECT DISTINCT b.node_id FROM chain_tunnel a JOIN chain_tunnel b ON a.tunnel_id = b.tunnel_id WHERE a.node_id = ?`, nodeID)
	must(err)
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			set[id] = true
		}
	}
	rows.Close()
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	return ids
}
