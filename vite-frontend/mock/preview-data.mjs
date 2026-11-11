/** 预览用假数据：vite 开发服务器和 preview-server.mjs 共用这一份 */
const GB = 1024 ** 3;
const now = Date.now();
const DAY = 86400_000;

const payload = {
  sub: "1",
  role_id: 0,
  user: "admin",
  exp: Math.floor((now + 3650 * DAY) / 1000),
  iat: Math.floor(now / 1000),
};
const token = [
  Buffer.from(JSON.stringify({ alg: "HS256", typ: "JWT" })).toString("base64"),
  Buffer.from(JSON.stringify(payload)).toString("base64"),
  "preview",
].join(".");

const users = [
  { id: 1, user: "r27637", status: 1, flow: 100, flowResetTime: 1, num: 3, expTime: now + 26 * DAY, inFlow: 6.2 * GB, outFlow: 6.1 * GB },
  { id: 2, user: "X27637", status: 1, flow: 99999, flowResetTime: 1, num: 99999, expTime: now + 300 * DAY, inFlow: 41.8 * GB, outFlow: 38.2 * GB },
  { id: 3, user: "Y27637", status: 1, flow: 50, flowResetTime: 1, num: 2, expTime: now + 5 * DAY, inFlow: 3.1 * GB, outFlow: 2.7 * GB },
  { id: 4, user: "test-a", status: 0, flow: 20, flowResetTime: 1, num: 1, expTime: now - 3 * DAY, inFlow: 0.4 * GB, outFlow: 0.3 * GB },
  { id: 5, user: "jason", status: 1, flow: 200, flowResetTime: 1, num: 5, expTime: now + 88 * DAY, inFlow: 61.5 * GB, outFlow: 57.9 * GB },
  { id: 6, user: "mz-backup", status: 1, flow: 10, flowResetTime: 1, num: 1, expTime: now + 12 * DAY, inFlow: 0.9 * GB, outFlow: 0.8 * GB },
];

const nodes = [
  { id: 1, name: "深圳 BGP-01", ip: "1.2.3.4", serverIp: "1.2.3.4", port: "22000", version: "1.4.2", http: 1, tls: 1, socks: 0, status: 1, connectionStatus: "online", tcpListenAddr: "0.0.0.0:22000", udpListenAddr: "0.0.0.0:22000",
    systemInfo: { cpuUsage: 12.4, memoryUsage: 38.1, uploadTraffic: 41.2 * GB, downloadTraffic: 63.8 * GB, uploadSpeed: 1.8 * GB, downloadSpeed: 2.6 * GB, uptime: 41 * DAY } },
  { id: 2, name: "香港 CN2", ip: "5.6.7.8", serverIp: "5.6.7.8", port: "22000", version: "1.4.2", http: 0, tls: 1, socks: 0, status: 1, connectionStatus: "online", tcpListenAddr: "0.0.0.0:22000", udpListenAddr: "0.0.0.0:22000",
    systemInfo: { cpuUsage: 7.9, memoryUsage: 22.6, uploadTraffic: 12.4 * GB, downloadTraffic: 19.1 * GB, uploadSpeed: 0.6 * GB, downloadSpeed: 1.1 * GB, uptime: 12 * DAY } },
  { id: 3, name: "东京 IIJ", ip: "9.9.9.9", serverIp: "9.9.9.9", port: "22000", version: "1.3.9", http: 0, tls: 0, socks: 1, status: 0, connectionStatus: "offline",
    systemInfo: null },
];

const tunnels = [
  { id: 1, name: "深圳 → 香港", type: 1, status: 1, flow: 2, trafficRatio: 1, inIp: "0.0.0.0",
    inNodeId: [{ nodeId: 1, chainType: 1 }], outNodeId: [{ nodeId: 2, chainType: 3 }], chainNodes: [], entryNodeName: "深圳 BGP-01", exitNodeName: "香港 CN2" },
  { id: 2, name: "东京 中转链", type: 2, status: 1, flow: 1, trafficRatio: 1.5, inIp: "0.0.0.0",
    inNodeId: [{ nodeId: 1, chainType: 1 }], outNodeId: [{ nodeId: 3, chainType: 3 }], chainNodes: [[{ nodeId: 2, protocol: "mtls", strategy: "round", chainType: 2, inx: 0 }]], entryNodeName: "深圳 BGP-01", exitNodeName: "东京 IIJ" },
  { id: 3, name: "香港 → 东京 备用", type: 1, status: 0, flow: 2, trafficRatio: 1, inIp: "0.0.0.0",
    inNodeId: [{ nodeId: 2, chainType: 1 }], outNodeId: [{ nodeId: 3, chainType: 3 }], chainNodes: [], entryNodeName: "香港 CN2", exitNodeName: "东京 IIJ" },
];

const forwards = [
  { id: 1, name: "网站 443", tunnelId: 1, tunnelName: "深圳 → 香港", inIp: "0.0.0.0", inPort: 443, remoteAddr: "10.0.0.8:443", strategy: "fifo", status: 1, inFlow: 18.2 * GB, outFlow: 16.9 * GB, serviceRunning: true, createdTime: "2026-10-28 09:12:00", userName: "r27637", userId: 1, inx: 0 },
  { id: 2, name: "游戏 25565", tunnelId: 1, tunnelName: "深圳 → 香港", inIp: "0.0.0.0", inPort: 25565, remoteAddr: "10.0.0.9:25565", strategy: "round", status: 1, inFlow: 4.1 * GB, outFlow: 3.8 * GB, serviceRunning: true, createdTime: "2026-10-29 14:03:00", userName: "r27637", userId: 1, inx: 1 },
  { id: 3, name: "远程桌面", tunnelId: 2, tunnelName: "东京 中转链", inIp: "0.0.0.0", inPort: 3389, remoteAddr: "10.0.1.5:3389", strategy: "fifo", status: 0, inFlow: 0.6 * GB, outFlow: 0.5 * GB, serviceRunning: false, createdTime: "2026-11-02 21:40:00", userName: "r27637", userId: 1, inx: 0 },
  { id: 4, name: "NAS 同步", tunnelId: 2, tunnelName: "东京 中转链", inIp: "0.0.0.0", inPort: 6690, remoteAddr: "10.0.1.6:6690", strategy: "fifo", status: 1, inFlow: 22.4 * GB, outFlow: 21.1 * GB, serviceRunning: true, createdTime: "2026-11-05 08:15:00", userName: "r27637", userId: 1, inx: 1 },
];

const limits = [
  { id: 1, name: "标准 100M", tunnelId: 1, tunnelName: "深圳 → 香港", uploadSpeed: 100, downloadSpeed: 100 },
  { id: 2, name: "大流量 500M", tunnelId: 2, tunnelName: "东京 中转链", uploadSpeed: 500, downloadSpeed: 500 },
];

const overview = {
  nodeTotal: nodes.length,
  nodeOnline: nodes.filter((n) => n.status === 1).length,
  totalUsedFlow: 268.4 * GB,
  todayFlow: 9.6 * GB,
  monthFlow: 186.2 * GB,
  userCount: users.length,
  users: users.map((u, i) => ({
    id: u.id, user: u.user, status: u.status, flow: u.flow, num: u.num,
    usedFlow: (u.inFlow + u.outFlow) * 0.42,
    todayFlow: [0.8, 4.2, 0.1, 0, 3.4, 0.05][i % 6] * GB,
    monthFlow: [12.4, 80.2, 3.4, 0.7, 76.9, 1.2][i % 6] * GB,
    expTime: u.expTime,
    forwardCount: [2, 3, 1, 1, 2, 1][i % 6],
  })),
  statisticsFlows: Array.from({ length: 24 }, (_, h) => ({
    time: `${String(h).padStart(2, "0")}:00`,
    flow: Math.round((0.12 + 0.38 * Math.abs(Math.sin((h - 5) / 4)) + 0.06 * Math.cos(h)) * GB),
  })),
};

const configs = [
  { id: 1, key: "app_name", value: "ForwardZ" },
  { id: 2, key: "site_url", value: "https://panel.example.com" },
];


const routes = {
  "/user/login": { token, role_id: 0, name: "admin" },
  "/user/package": {},
  "/user/list": users,
  "/dashboard/overview": overview,
  "/node/list": nodes,
  "/tunnel/list": tunnels,
  "/forward/list": forwards,
  "/speed-limit/list": limits,
  "/config/list": configs,
  "/config/get": { name: "app_name", value: "ForwardZ" },
  "/backup/list": [],
  "/tunnel/user/list": [],
  "/fingerprint/get": { fingerprint: "preview-fp", secret: "preview-secret" },
  "/node/check-status": {},
};

export { token, users, nodes, tunnels, forwards, limits, overview, configs, routes };
