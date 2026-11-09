package main

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
)

var portPartRe = regexp.MustCompile(`^([0-9]{1,5})(-([0-9]{1,5}))?$`)

func validatePortRange(port string) {
	if port == "" {
		throw("可用端口不合法")
	}
	for _, part := range strings.Split(port, ",") {
		part = strings.TrimSpace(part)
		m := portPartRe.FindStringSubmatch(part)
		if m == nil {
			throw("可用端口不合法")
		}
		start, _ := strconv.Atoi(m[1])
		if m[3] != "" {
			end, _ := strconv.Atoi(m[3])
			if end > 65535 || start > end {
				throw("可用端口不合法")
			}
		} else if start > 65535 {
			throw("可用端口不合法")
		}
	}
}

// parsePorts "1000-1010,2000" -> 去重排序后的端口列表
func parsePorts(input string) []int64 {
	set := map[int64]bool{}
	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			r := strings.SplitN(part, "-", 2)
			start, err1 := strconv.ParseInt(strings.TrimSpace(r[0]), 10, 64)
			end, err2 := strconv.ParseInt(strings.TrimSpace(r[1]), 10, 64)
			if err1 != nil || err2 != nil {
				continue
			}
			for i := start; i <= end; i++ {
				set[i] = true
			}
		} else if p, err := strconv.ParseInt(part, 10, 64); err == nil {
			set[p] = true
		}
	}
	out := make([]int64, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sortInt64(out)
	return out
}

func sortInt64(a []int64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

type nodeForm struct {
	ID            Num `json:"id"`
	Name          Str `json:"name"`
	ServerIP      Str `json:"serverIp"`
	Port          Str `json:"port"`
	InterfaceName Str `json:"interfaceName"`
	HTTP          Num `json:"http"`
	TLS           Num `json:"tls"`
	Socks         Num `json:"socks"`
	TCPListenAddr Str `json:"tcpListenAddr"`
	UDPListenAddr Str `json:"udpListenAddr"`
}

func nodeCreate(c *Ctx) any {
	var d nodeForm
	c.bind(&d)
	notBlank(d.Name, "节点名称不能为空")
	notBlank(d.ServerIP, "服务器ip不能为空")
	notBlank(d.Port, "可用端口不能为空")
	validatePortRange(d.Port.V)
	now := nowMs()
	// 监听地址使用表默认值 [::]（与 Java 版一致）
	mustExec(`INSERT INTO node (name, secret, server_ip, port, interface_name, created_time, updated_time, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
		d.Name.V, simpleUUID(), d.ServerIP.V, d.Port.V, d.InterfaceName.Ptr(), now, now)
	return ok()
}

func nodeList(c *Ctx) any {
	list, err := queryList(scanNode, "SELECT "+nodeCols+" FROM node ORDER BY status DESC")
	must(err)
	for _, n := range list {
		n.Secret = nil
	}
	return ok(list)
}

func nodeUpdate(c *Ctx) any {
	var d nodeForm
	c.bind(&d)
	notNull(d.ID, "节点ID不能为空")
	notBlank(d.Name, "节点名称不能为空")
	notBlank(d.ServerIP, "服务器ip不能为空")
	notBlank(d.Port, "可用port不能为空")
	validatePortRange(d.Port.V)

	forwardMu.Lock()
	defer forwardMu.Unlock()
	reconcileMu.Lock()
	defer reconcileMu.Unlock()

	node := getNode(d.ID.Int())
	if node == nil {
		return fail("节点不存在")
	}
	changed := func(n Num, cur int64) bool { return n.Valid && n.Int() != cur }
	if node.Status == 1 && (changed(d.HTTP, node.HTTP) || changed(d.TLS, node.TLS) || changed(d.Socks, node.Socks)) {
		r := sendMsg(node.ID, map[string]any{"http": d.HTTP.Ptr(), "tls": d.TLS.Ptr(), "socks": d.Socks.Ptr()}, "SetProtocol")
		if r.Msg != "OK" {
			return fail(r.Msg)
		}
	}

	// 修改前：相关节点的期望配置、入口 IP 自动生成的隧道
	related := relatedNodeIDs(node.ID)
	before := desiredStates(related)
	autoInIP := map[int64]bool{}
	for _, t := range tunnelsWithEntry(node.ID) {
		if t.InIP == nil || *t.InIP == "" || *t.InIP == entryIPs(t.ID) {
			autoInIP[t.ID] = true
		}
	}
	// 端口范围变化：不在新范围内的隧道端口、转发端口重新分配（不够时直接报错，不做任何修改）
	chainMoves, portMoves := planPortMoves(node.ID, d.Port.V)

	tcp, udp := "0.0.0.0", "0.0.0.0" // 未传时的默认值与 Java 版一致
	if d.TCPListenAddr.Valid {
		tcp = d.TCPListenAddr.V
	}
	if d.UDPListenAddr.Valid {
		udp = d.UDPListenAddr.V
	}
	sets := []string{"name = ?", "server_ip = ?", "port = ?", "updated_time = ?", "tcp_listen_addr = ?", "udp_listen_addr = ?"}
	args := []any{d.Name.V, d.ServerIP.V, d.Port.V, nowMs(), tcp, udp}
	for col, v := range map[string]Num{"http": d.HTTP, "tls": d.TLS, "socks": d.Socks} {
		if v.Valid {
			sets = append(sets, col+" = ?")
			args = append(args, v.Int())
		}
	}
	if d.InterfaceName.Valid {
		sets = append(sets, "interface_name = ?")
		args = append(args, d.InterfaceName.V)
	}
	args = append(args, node.ID)
	mustExec("UPDATE node SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	for id, p := range chainMoves {
		mustExec("UPDATE chain_tunnel SET port = ? WHERE id = ?", p, id)
	}
	for id, p := range portMoves {
		mustExec("UPDATE forward_port SET port = ? WHERE id = ?", p, id)
	}
	for tid := range autoInIP {
		mustExec("UPDATE tunnel SET in_ip = ? WHERE id = ?", entryIPs(tid), tid)
	}
	if len(chainMoves)+len(portMoves) > 0 {
		log.Printf("节点 %d 端口范围变更，重新分配隧道端口 %d 个、转发端口 %d 个", node.ID, len(chainMoves), len(portMoves))
	}

	// 修改后：把差异下发到相关的在线节点（离线节点上线后自动对账）
	if errs := pushChanges(before, desiredStates(related)); len(errs) > 0 {
		return fail("节点已保存，但以下配置同步失败（节点重新连接后会自动重试）：" + strings.Join(errs, "；"))
	}
	return ok()
}

// tunnelsWithEntry 以该节点为入口的隧道
func tunnelsWithEntry(nodeID int64) []*Tunnel {
	list, err := queryList(scanTunnel, "SELECT "+tunnelCols+" FROM tunnel WHERE id IN (SELECT tunnel_id FROM chain_tunnel WHERE node_id = ? AND chain_type = 1)", nodeID)
	must(err)
	return list
}

// entryIPs 隧道入口节点 IP 列表（未手动指定入口 IP 时隧道使用的默认值）
func entryIPs(tunnelID int64) string {
	var ips []string
	for _, ct := range entryChains(tunnelID) {
		if n := getNode(ct.NodeID); n != nil {
			ips = append(ips, n.ServerIP)
		}
	}
	return strings.Join(ips, ",")
}

// planPortMoves 端口范围修改后，为不在新范围内的隧道端口 / 转发端口挑选新端口
func planPortMoves(nodeID int64, newRange string) (chainMoves, portMoves map[int64]int64) {
	chainMoves, portMoves = map[int64]int64{}, map[int64]int64{}
	allowed := map[int64]bool{}
	ports := parsePorts(newRange)
	for _, p := range ports {
		allowed[p] = true
	}
	used := map[int64]bool{}
	var needChain []*ChainTunnel
	var needPort []*ForwardPort
	for _, ct := range listChains("WHERE node_id = ? ORDER BY id", nodeID) {
		if ct.Port == nil {
			continue
		}
		if allowed[*ct.Port] {
			used[*ct.Port] = true
		} else if ct.ChainType != 1 {
			needChain = append(needChain, ct)
		}
	}
	for _, fp := range listForwardPorts("WHERE node_id = ? ORDER BY id", nodeID) {
		if allowed[fp.Port] {
			used[fp.Port] = true
		} else {
			needPort = append(needPort, fp)
		}
	}
	var free []int64
	for _, p := range ports {
		if !used[p] {
			free = append(free, p)
		}
	}
	if need := len(needChain) + len(needPort); need > len(free) {
		throw(fmt.Sprintf("新端口范围不够用：需要重新分配 %d 个端口（隧道 %d 个、转发 %d 个），新范围内只有 %d 个空闲端口",
			need, len(needChain), len(needPort), len(free)))
	}
	i := 0
	for _, ct := range needChain {
		chainMoves[*ct.ID] = free[i]
		i++
	}
	for _, fp := range needPort {
		portMoves[fp.ID] = free[i]
		i++
	}
	return
}

func nodeDelete(c *Ctx) any {
	id := c.params().int("id")
	forwardMu.Lock()
	defer forwardMu.Unlock()
	if getNode(id) == nil {
		return fail("节点不存在")
	}
	rows, err := db.Query("SELECT DISTINCT tunnel_id FROM chain_tunnel WHERE node_id = ?", id)
	must(err)
	var tunnelIDs []int64
	for rows.Next() {
		var t int64
		if rows.Scan(&t) == nil {
			tunnelIDs = append(tunnelIDs, t)
		}
	}
	rows.Close()
	for _, t := range tunnelIDs {
		deleteTunnelInternal(t)
	}
	mustExec("DELETE FROM node WHERE id = ?", id)
	return ok()
}

func nodeInstall(c *Ctx) any {
	id := c.params().int("id")
	node := getNode(id)
	if node == nil {
		return fail("节点不存在")
	}
	ip, found := getConfigValue("ip")
	if !found {
		return fail("请先前往「系统配置」设置面板地址")
	}
	cmd := "curl -L https://github.com/qqlikegi/flux-panel/releases/latest/download/install.sh -o ./install.sh && chmod +x ./install.sh && " +
		"./install.sh -a " + processServerAddress(ip) + " -s " + node.secret()
	return ok(cmd)
}

// ---------- 网站配置 ----------

func configList(c *Ctx) any {
	rows, err := db.Query("SELECT name, value FROM vite_config")
	must(err)
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			m[k] = v
		}
	}
	return ok(m)
}

func configGet(c *Ctx) any {
	name := strings.TrimSpace(c.params().str("name"))
	if name == "" {
		return fail("配置名称不能为空")
	}
	var v ViteConfig
	err := db.QueryRow("SELECT id, name, value, time FROM vite_config WHERE name = ?", name).Scan(&v.ID, &v.Name, &v.Value, &v.Time)
	if err != nil {
		return fail("配置不存在")
	}
	return ok(v)
}

func upsertConfig(name, value string) {
	mustExec(`INSERT INTO vite_config (name, value, time) VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET value = excluded.value, time = excluded.time`, name, value, nowMs())
}

func configUpdate(c *Ctx) any {
	p := c.params()
	if len(p) == 0 {
		return fail("配置数据不能为空")
	}
	for k := range p {
		if strings.TrimSpace(k) == "" {
			continue
		}
		v := ""
		if p[k] != nil {
			v = p.str(k)
		}
		upsertConfig(k, v)
	}
	return ok()
}

func configUpdateSingle(c *Ctx) any {
	p := c.params()
	name, value := strings.TrimSpace(p.str("name")), p.str("value")
	if name == "" {
		return fail("配置名称不能为空")
	}
	if strings.TrimSpace(value) == "" {
		return fail("配置值不能为空")
	}
	upsertConfig(name, value)
	return ok()
}
