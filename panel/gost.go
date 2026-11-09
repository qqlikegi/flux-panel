package main

import (
	"fmt"
	"strconv"
	"strings"
)

// 下发给节点端（gost）的配置，名称规则与 Java 版保持一致：
//   转发服务  {forwardId}_{userId}_{userTunnelId}_{tcp|udp}
//   隧道服务  {tunnelId}_tls      转发链  chains_{tunnelId}      限速器  {speedLimitId}

func okIf(r *GostResult, contains string) *GostResult {
	if strings.Contains(r.Msg, contains) {
		r.Msg = "OK"
	}
	return r
}

func limiterData(name int64, speed string) map[string]any {
	return map[string]any{
		"name":   strconv.FormatInt(name, 10),
		"limits": []string{"$ " + speed + "MB " + speed + "MB"},
	}
}

func gostAddLimiter(nodeID, name int64, speed string) *GostResult {
	return okIf(sendMsg(nodeID, limiterData(name, speed), "AddLimiters"), "exists")
}

func gostUpdateLimiter(nodeID, name int64, speed string) *GostResult {
	return sendMsg(nodeID, map[string]any{"limiter": strconv.FormatInt(name, 10), "data": limiterData(name, speed)}, "UpdateLimiters")
}

func gostDeleteLimiter(nodeID, name int64) *GostResult {
	return okIf(sendMsg(nodeID, map[string]any{"limiter": strconv.FormatInt(name, 10)}, "DeleteLimiters"), "not found")
}

// gostAddChains 在 nodeID 上创建转发链，指向下一跳的节点列表
func gostAddChains(nodeID int64, next []*ChainTunnel, nodes map[int64]*Node) *GostResult {
	hopNodes := []any{}
	for _, ct := range next {
		n := nodes[ct.NodeID]
		hopNodes = append(hopNodes, map[string]any{
			"name": fmt.Sprintf("node_%d", ct.NodeID), // 同一跳内节点名唯一

			"addr":      processServerAddress(fmt.Sprintf("%s:%d", n.ServerIP, ct.port())),
			"connector": map[string]any{"type": "relay"},
			"dialer":    map[string]any{"type": strOr(ct.Protocol, "")},
		})
	}
	first := next[0]
	hop := map[string]any{
		"name": fmt.Sprintf("hop_%d", first.TunnelID),
		"selector": map[string]any{
			"strategy":    strOr(first.Strategy, ""),
			"maxFails":    1,
			"failTimeout": int64(600000000000), // 600 秒（纳秒）
		},
		"nodes": hopNodes,
	}
	if self := nodes[nodeID]; self != nil && self.iface() != "" {
		hop["interface"] = self.iface()
	}
	data := map[string]any{"name": fmt.Sprintf("chains_%d", first.TunnelID), "hops": []any{hop}}
	return okIf(sendMsg(nodeID, data, "AddChains"), "exists")
}

func gostDeleteChains(nodeID int64, name string) *GostResult {
	return okIf(sendMsg(nodeID, map[string]any{"chain": name}, "DeleteChains"), "not found")
}

// gostAddChainService 中间跳 / 出口节点上的隧道监听服务
func gostAddChainService(nodeID int64, ct *ChainTunnel, nodes map[int64]*Node) *GostResult {
	n := nodes[ct.NodeID]
	svc := map[string]any{
		"name":     fmt.Sprintf("%d_tls", ct.TunnelID),
		"addr":     fmt.Sprintf("%s:%d", n.TCPListenAddr, ct.port()),
		"listener": map[string]any{"type": strOr(ct.Protocol, "")},
	}
	if ct.ChainType == 3 && nodes[nodeID] != nil && nodes[nodeID].iface() != "" {
		svc["metadata"] = map[string]any{"interface": nodes[nodeID].iface()}
	}
	h := map[string]any{"type": "relay"}
	if ct.ChainType == 2 {
		h["chain"] = fmt.Sprintf("chains_%d", ct.TunnelID)
	}
	svc["handler"] = h
	return okIf(sendMsg(nodeID, []any{svc}, "AddService"), "exists")
}

// gostAddOrUpdateService 入口节点上的转发服务（tcp + udp），method 为 AddService / UpdateService
func gostAddOrUpdateService(name string, limiter *int64, node *Node, f *Forward, port int64, tunnel *Tunnel, method string) *GostResult {
	services := []any{}
	for _, proto := range []string{"tcp", "udp"} {
		listenAddr := node.TCPListenAddr
		if proto == "udp" {
			listenAddr = node.UDPListenAddr
		}
		svc := map[string]any{
			"name": name + "_" + proto,
			"addr": fmt.Sprintf("%s:%d", listenAddr, port),
		}
		if tunnel.Type == 1 && node.iface() != "" {
			svc["metadata"] = map[string]any{"interface": node.iface()}
		}
		if limiter != nil {
			svc["limiter"] = strconv.FormatInt(*limiter, 10)
		}
		h := map[string]any{"type": proto}
		if tunnel.Type == 2 {
			h["chain"] = fmt.Sprintf("chains_%d", f.TunnelID)
		}
		svc["handler"] = h
		listener := map[string]any{"type": proto}
		if proto == "udp" {
			listener["metadata"] = map[string]any{"keepAlive": true}
		}
		svc["listener"] = listener
		svc["forwarder"] = forwarderData(f.RemoteAddr, f.Strategy)
		services = append(services, svc)
	}
	return okIf(sendMsg(node.ID, services, method), "exists")
}

func forwarderData(remoteAddr, strategy string) map[string]any {
	nodes := []any{}
	for i, addr := range strings.Split(remoteAddr, ",") {
		nodes = append(nodes, map[string]any{"name": fmt.Sprintf("node_%d", i+1), "addr": addr})
	}
	if strategy == "" {
		strategy = "fifo"
	}
	return map[string]any{
		"nodes":    nodes,
		"selector": map[string]any{"strategy": strategy, "maxFails": 1, "failTimeout": "600s"},
	}
}

func gostDeleteService(nodeID int64, services []string) *GostResult {
	return okIf(sendMsg(nodeID, map[string]any{"services": services}, "DeleteService"), "not found")
}

func forwardServiceNames(name string) []string { return []string{name + "_tcp", name + "_udp"} }

// gostPauseResume method 为 PauseService / ResumeService
func gostPauseResume(nodeID int64, name, method string) *GostResult {
	return sendMsg(nodeID, map[string]any{"services": forwardServiceNames(name)}, method)
}

func strOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

// speedToMBps Mbps -> MB/s，保留 1 位小数（四舍五入），与 Java 版输出一致（如 "12.5"、"125.0"）
func speedToMBps(speed int64) string {
	tenths := (20*speed + 8) / 16 // = round_half_up(speed / 8 * 10)
	return fmt.Sprintf("%d.%d", tenths/10, tenths%10)
}
