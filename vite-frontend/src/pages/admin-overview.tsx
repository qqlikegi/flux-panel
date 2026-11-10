import { useEffect, useMemo, useState } from "react";
import { Card, CardBody, CardHeader } from "@heroui/card";
import { Button } from "@heroui/button";
import { Select, SelectItem } from "@heroui/select";
import toast from "react-hot-toast";
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from "recharts";

import { getDashboardOverview } from "@/api";

interface OverviewUser {
  id: number;
  user: string;
  status: number;
  flow: number; // GB，99999 为无限制
  num: number;
  usedFlow: number;
  todayFlow: number;
  monthFlow: number;
  expTime: number;
  forwardCount: number;
}

interface Overview {
  nodeTotal: number;
  nodeOnline: number;
  totalUsedFlow: number;
  todayFlow: number;
  monthFlow: number;
  userCount: number;
  users: OverviewUser[];
  statisticsFlows: { time: string; flow: number }[];
}

type SortKey = "todayFlow" | "monthFlow" | "usedFlow";

const formatFlow = (v: number) => {
  if (!v || v <= 0) return "0 B";
  if (v < 1024) return v + " B";
  if (v < 1024 ** 2) return (v / 1024).toFixed(2) + " KB";
  if (v < 1024 ** 3) return (v / 1024 ** 2).toFixed(2) + " MB";
  if (v < 1024 ** 4) return (v / 1024 ** 3).toFixed(2) + " GB";
  return (v / 1024 ** 4).toFixed(2) + " TB";
};

const axisFlow = (v: number) => {
  if (v === 0) return "0";
  if (v < 1024) return `${v}B`;
  if (v < 1024 ** 2) return `${(v / 1024).toFixed(1)}K`;
  if (v < 1024 ** 3) return `${(v / 1024 ** 2).toFixed(1)}M`;
  return `${(v / 1024 ** 3).toFixed(1)}G`;
};

const expInfo = (u: OverviewUser) => {
  if (u.status === 0) return { text: "已停用", cls: "bg-default-100 text-default-600" };
  if (!u.expTime) return { text: "永久", cls: "bg-success-50 text-success-600 dark:bg-success-500/10" };
  const days = Math.ceil((u.expTime - Date.now()) / 86400000);
  if (days <= 0) return { text: "已过期", cls: "bg-danger-50 text-danger-600 dark:bg-danger-500/10" };
  if (days <= 7) return { text: `剩 ${days} 天`, cls: "bg-warning-50 text-warning-600 dark:bg-warning-500/10" };
  return { text: new Date(u.expTime).toLocaleDateString(), cls: "bg-default-100 text-default-600" };
};

function UsageBar({ used, limitGB }: { used: number; limitGB: number }) {
  if (limitGB === 99999) {
    return <p className="text-xs text-default-500">{formatFlow(used)} / 无限制</p>;
  }
  const pct = limitGB > 0 ? Math.min((used / (limitGB * 1024 ** 3)) * 100, 100) : 0;
  const color = pct >= 90 ? "bg-red-500" : pct >= 70 ? "bg-orange-500" : "bg-blue-500";
  return (
    <div className="min-w-[120px]">
      <div className="w-full h-1.5 bg-gray-200 dark:bg-gray-800 rounded-full">
        <div className={`h-1.5 rounded-full ${color}`} style={{ width: `${pct}%` }} />
      </div>
      <p className="text-xs text-default-500 mt-1 whitespace-nowrap">
        {formatFlow(used)} / {limitGB} GB
      </p>
    </div>
  );
}

function StatCard({ label, value, sub, color, icon }: { label: string; value: string; sub?: string; color: string; icon: React.ReactNode }) {
  return (
    <Card className="border border-gray-200 dark:border-default-200 shadow-md">
      <CardBody className="p-3 lg:p-4">
        <div className="flex items-center justify-between">
          <p className="text-xs lg:text-sm text-default-600 truncate">{label}</p>
          <div className={`p-1.5 lg:p-2 rounded-lg flex-shrink-0 ${color}`}>{icon}</div>
        </div>
        <p className="text-base lg:text-2xl font-bold text-foreground truncate mt-2">{value}</p>
        {sub && <p className="text-xs text-default-500 mt-1 truncate">{sub}</p>}
      </CardBody>
    </Card>
  );
}

const icon = (d: string) => (
  <svg className="w-4 h-4 lg:w-5 lg:h-5" fill="currentColor" viewBox="0 0 20 20">
    <path fillRule="evenodd" clipRule="evenodd" d={d} />
  </svg>
);

export default function AdminOverview() {
  const [data, setData] = useState<Overview | null>(null);
  const [loading, setLoading] = useState(true);
  const [sortKey, setSortKey] = useState<SortKey>("todayFlow");
  const [picked, setPicked] = useState<Set<string>>(new Set());

  const load = async (quiet = false) => {
    if (!quiet) setLoading(true);
    try {
      const res = await getDashboardOverview();
      if (res.code === 0) setData(res.data);
      else if (!quiet) toast.error(res.msg || "获取数据失败");
    } catch {
      if (!quiet) toast.error("获取数据失败");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
    localStorage.setItem("e", "/dashboard");
    const timer = setInterval(() => load(true), 60000);
    return () => clearInterval(timer);
  }, []);

  // 最近 24 小时，没有数据的小时补 0
  const chartData = useMemo(() => {
    const map = new Map<string, number>();
    (data?.statisticsFlows || []).forEach(i => map.set(i.time, i.flow || 0));
    const now = Date.now();
    const out: { time: string; flow: number }[] = [];
    for (let i = 23; i >= 0; i--) {
      const h = new Date(now - i * 3600000).getHours().toString().padStart(2, "0") + ":00";
      out.push({ time: h, flow: map.get(h) || 0 });
    }
    return out;
  }, [data]);

  // 下拉列表里的全部用户（按名称排序）
  const allUsers = useMemo(
    () => [...(data?.users || [])].sort((a, b) => a.user.localeCompare(b.user)),
    [data],
  );

  // 没勾选任何用户时显示全部
  const users = useMemo(() => {
    return [...(data?.users || [])]
      .filter(u => picked.size === 0 || picked.has(String(u.id)))
      .sort((a, b) => b[sortKey] - a[sortKey]);
  }, [data, sortKey, picked]);

  if (loading && !data) {
    return (
      <div className="px-3 lg:px-6 pt-4 flex items-center justify-center h-64">
        <div className="flex items-center gap-3">
          <div className="animate-spin h-5 w-5 border-2 border-gray-200 dark:border-gray-700 border-t-gray-600 dark:border-t-gray-300 rounded-full" />
          <span className="text-default-600">正在加载数据...</span>
        </div>
      </div>
    );
  }

  const d = data || ({ nodeTotal: 0, nodeOnline: 0, totalUsedFlow: 0, todayFlow: 0, monthFlow: 0, userCount: 0, users: [], statisticsFlows: [] } as Overview);
  const sortTabs: { key: SortKey; label: string }[] = [
    { key: "todayFlow", label: "今日" },
    { key: "monthFlow", label: "本月" },
    { key: "usedFlow", label: "已用" },
  ];

  return (
    <div className="px-3 lg:px-6 py-2 lg:py-4">
      {/* 统计卡片 */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 lg:gap-4 mb-6">
        <StatCard
          label="在线节点"
          value={`${d.nodeOnline} / ${d.nodeTotal}`}
          sub={d.nodeTotal - d.nodeOnline > 0 ? `${d.nodeTotal - d.nodeOnline} 个节点离线` : "全部在线"}
          color="bg-green-100 text-green-600 dark:bg-green-500/20 dark:text-green-400"
          icon={icon("M2 5a2 2 0 012-2h12a2 2 0 012 2v2a2 2 0 01-2 2H4a2 2 0 01-2-2V5zm14 1a1 1 0 11-2 0 1 1 0 012 0zM2 13a2 2 0 012-2h12a2 2 0 012 2v2a2 2 0 01-2 2H4a2 2 0 01-2-2v-2zm14 1a1 1 0 11-2 0 1 1 0 012 0z")}
        />
        <StatCard
          label="总使用流量"
          value={formatFlow(d.totalUsedFlow)}
          sub={`共 ${d.userCount} 个用户（本计费周期）`}
          color="bg-blue-100 text-blue-600 dark:bg-blue-500/20 dark:text-blue-400"
          icon={icon("M3 3a1 1 0 000 2v8a2 2 0 002 2h2.586l-1.293 1.293a1 1 0 101.414 1.414L10 15.414l2.293 2.293a1 1 0 001.414-1.414L12.414 15H15a2 2 0 002-2V5a1 1 0 100-2H3zm11.707 4.707a1 1 0 00-1.414-1.414L10 9.586 8.707 8.293a1 1 0 00-1.414 0l-2 2a1 1 0 101.414 1.414L8 10.414l1.293 1.293a1 1 0 001.414 0l4-4z")}
        />
        <StatCard
          label="今日流量"
          value={formatFlow(d.todayFlow)}
          color="bg-purple-100 text-purple-600 dark:bg-purple-500/20 dark:text-purple-400"
          icon={icon("M10 18a8 8 0 100-16 8 8 0 000 16zm1-12a1 1 0 10-2 0v4a1 1 0 00.293.707l2.828 2.829a1 1 0 101.415-1.415L11 9.586V6z")}
        />
        <StatCard
          label="本月流量"
          value={formatFlow(d.monthFlow)}
          color="bg-orange-100 text-orange-600 dark:bg-orange-500/20 dark:text-orange-400"
          icon={icon("M6 2a1 1 0 00-1 1v1H4a2 2 0 00-2 2v10a2 2 0 002 2h12a2 2 0 002-2V6a2 2 0 00-2-2h-1V3a1 1 0 10-2 0v1H7V3a1 1 0 00-1-1zm0 5a1 1 0 000 2h8a1 1 0 100-2H6z")}
        />
      </div>

      {/* 24 小时流量 */}
      <Card className="mb-6 border border-gray-200 dark:border-default-200 shadow-md">
        <CardHeader className="pb-3 flex items-center justify-between">
          <h2 className="text-lg lg:text-xl font-semibold text-foreground">24小时流量统计</h2>
          <Button size="sm" variant="flat" onPress={() => load()} isLoading={loading}>
            刷新
          </Button>
        </CardHeader>
        <CardBody className="pt-0">
          <div className="h-64 lg:h-80 w-full">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={chartData} accessibilityLayer={false}>
                <CartesianGrid strokeDasharray="3 3" className="opacity-30" />
                <XAxis dataKey="time" tick={{ fontSize: 12 }} tickLine={false} axisLine={{ stroke: "#e5e7eb", strokeWidth: 1 }} />
                <YAxis tick={{ fontSize: 12 }} tickLine={false} axisLine={{ stroke: "#e5e7eb", strokeWidth: 1 }} tickFormatter={axisFlow} />
                <Tooltip
                  content={({ active, payload, label }) =>
                    active && payload && payload.length ? (
                      <div className="bg-white dark:bg-default-100 border border-default-200 rounded-lg shadow-lg p-3">
                        <p className="font-medium text-foreground">{`时间: ${label}`}</p>
                        <p className="text-primary">{`流量: ${formatFlow((payload[0]?.value as number) || 0)}`}</p>
                      </div>
                    ) : null
                  }
                />
                <Line type="monotone" dataKey="flow" stroke="#8b5cf6" strokeWidth={3} dot={false} activeDot={{ r: 4, stroke: "#8b5cf6", strokeWidth: 2, fill: "#fff" }} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </CardBody>
      </Card>

      {/* 用户流量 */}
      <div className="mb-6">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 mb-4 px-1">
          <div className="flex items-center gap-2">
            <h2 className="text-xl lg:text-2xl font-bold text-foreground">用户流量</h2>
            <span className="fz-cell !rounded-full px-2.5 py-0.5 text-default-600 text-xs font-semibold">
              {picked.size > 0 ? `${users.length} / ${allUsers.length}` : users.length}
            </span>
          </div>
          <div className="flex items-center gap-2 w-full sm:w-auto">
            <Select
              aria-label="筛选用户"
              className="flex-1 sm:flex-none sm:w-56"
              placeholder="全部用户"
              renderValue={(items) => (items.length === 1 ? items[0].textValue : `已选 ${items.length} 个用户`)}
              selectedKeys={picked}
              selectionMode="multiple"
              size="sm"
              onSelectionChange={(keys) =>
                setPicked(keys === "all" ? new Set(allUsers.map(u => String(u.id))) : new Set(Array.from(keys).map(String)))
              }
            >
              {allUsers.map(u => (
                <SelectItem key={String(u.id)} textValue={u.user}>{u.user}</SelectItem>
              ))}
            </Select>
            {picked.size > 0 && (
              <Button className="flex-shrink-0" size="sm" variant="flat" onPress={() => setPicked(new Set())}>
                全部
              </Button>
            )}
            <div className="fz-segment flex-shrink-0">
              {sortTabs.map(t => (
                <button key={t.key} data-active={sortKey === t.key} type="button" onClick={() => setSortKey(t.key)}>
                  {t.label}
                </button>
              ))}
            </div>
          </div>
        </div>

        {users.length === 0 ? (
          <Card>
            <CardBody>
              <p className="text-center text-default-500 py-10">暂无用户</p>
            </CardBody>
          </Card>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4 gap-3 lg:gap-4">
            {users.map(u => {
              const e = expInfo(u);

              return (
                <Card key={u.id}>
                  <CardBody className="p-4 gap-3">
                    <div className="flex items-center gap-3">
                      <div className="w-10 h-10 rounded-full bg-primary/15 text-primary flex items-center justify-center font-bold uppercase flex-shrink-0">
                        {u.user.slice(0, 1)}
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="font-semibold text-foreground truncate">{u.user}</p>
                        <p className="text-xs text-default-500">
                          转发 {u.num === 99999 ? u.forwardCount : `${u.forwardCount} / ${u.num}`}
                        </p>
                      </div>
                      <span className={`px-2.5 py-0.5 rounded-full text-xs whitespace-nowrap ${e.cls}`}>{e.text}</span>
                    </div>
                    <div className="grid grid-cols-2 gap-2">
                      <div className="fz-cell px-3 py-2">
                        <p className="text-xs text-default-500">今日</p>
                        <p className="text-base font-bold tabular-nums text-foreground">{formatFlow(u.todayFlow)}</p>
                      </div>
                      <div className="fz-cell px-3 py-2">
                        <p className="text-xs text-default-500">本月</p>
                        <p className="text-base font-bold tabular-nums text-foreground">{formatFlow(u.monthFlow)}</p>
                      </div>
                    </div>
                    <UsageBar limitGB={u.flow} used={u.usedFlow} />
                  </CardBody>
                </Card>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
