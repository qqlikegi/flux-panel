#!/bin/bash
# CI 冒烟测试：在干净的 Ubuntu 上用安装脚本装好面板和节点端，跑一遍端到端流程。
# 用法: sudo bash .github/scripts/smoke-test.sh <构建产物目录>
set -euo pipefail

ART="$1"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
PORT=6365
API="http://127.0.0.1:${PORT}/api/v1"

step() { echo; echo "==== $* ===="; }
fail() { echo "❌ $*"; exit 1; }

command -v jq >/dev/null || apt-get install -y -q jq
command -v sqlite3 >/dev/null || apt-get install -y -q sqlite3

step "安装面板（本地二进制）"
PANEL_BINARY="$ART/flux-panel-amd64" PANEL_PORT=$PORT bash "$ROOT/panel_install.sh" install
systemctl is-active --quiet flux-panel || fail "面板服务未运行"

step "前端页面由后端托管"
curl -fs "http://127.0.0.1:${PORT}/" | grep -qi '<div id="root"' || fail "首页不是前端页面"
curl -fs "http://127.0.0.1:${PORT}/dashboard" | grep -qi '<div id="root"' || fail "前端路由刷新未回退到 index.html"
code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/assets/not-exist.js")
[[ "$code" == "404" ]] || fail "缺失的静态文件应返回 404，实际 $code"

step "首次部署设置管理员"
[[ "$(curl -fs -X POST "$API/setup/status" | jq -r .data.needSetup)" == "true" ]] || fail "全新安装应进入初始化流程"
curl -fs -X POST "$API/setup/init" -H 'Content-Type: application/json' \
  -d '{"username":"admin_user","password":"admin_user"}' | jq -e '.code == 0' >/dev/null || fail "设置管理员失败"
[[ "$(curl -fs -X POST "$API/setup/status" | jq -r .data.needSetup)" == "false" ]] || fail "初始化后状态未更新"

step "登录"
TOKEN=$(curl -fs -X POST "$API/user/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin_user","password":"admin_user"}' | jq -r '.data.token')
[[ -n "$TOKEN" && "$TOKEN" != "null" ]] || fail "登录失败"
api() {
  local body="${2:-}"
  [[ -z "$body" ]] && body='{}'
  curl -fs -X POST "$API$1" -H 'Content-Type: application/json' -H "Authorization: $TOKEN" -d "$body"
}
expect_ok() { local r; r=$(api "$1" "$2"); echo "$1 -> $r"; [[ "$(echo "$r" | jq -r .code)" == "0" ]] || fail "$1 失败"; }

step "创建节点并用安装脚本安装节点端"
expect_ok /node/create '{"name":"ci","serverIp":"127.0.0.1","port":"31000-31010"}'
SECRET=$(sqlite3 /opt/flux-panel/data/gost.db "select secret from node where id=1")
FP=$(api /fingerprint/get | jq -r .data.fingerprint)
[[ "$FP" =~ ^[A-Za-z0-9]{8}$ ]] || fail "面板指纹格式不对: $FP"
api /node/install '{"id":1}' | jq -r .data | grep -q -- "-f $FP" || fail "安装命令未带面板指纹"
cp "$ROOT/install.sh" /tmp/agent_install.sh   # 安装脚本执行完会删除自身
AGENT_BINARY="$ART/gost-amd64" ZF_SCRIPT="$ROOT/zf.sh" bash /tmp/agent_install.sh -a "127.0.0.1:${PORT}" -s "$SECRET" -f "$FP"
grep -q "\"fingerprint\": \"$FP\"" /etc/flux_agent/config.json || fail "实例配置未写入面板指纹"
systemctl is-active --quiet flux_agent || fail "节点端服务未运行"

for _ in $(seq 1 30); do
  st=$(api /node/list | jq -r '.data[0].status')
  [[ "$st" == "1" ]] && break
  sleep 2
done
[[ "$st" == "1" ]] || fail "节点未上线"
echo "✅ 节点已上线"

step "同一台机器共存安装第二个节点端实例（用另一个指纹模拟第二个面板）"
expect_ok /node/create '{"name":"ci2","serverIp":"127.0.0.1","port":"31011-31020"}'
SECRET2=$(sqlite3 /opt/flux-panel/data/gost.db "select secret from node where name='ci2'")
cp "$ROOT/install.sh" /tmp/agent_install.sh
AGENT_BINARY="$ART/gost-amd64" ZF_SCRIPT="$ROOT/zf.sh" bash /tmp/agent_install.sh -a "127.0.0.1:${PORT}" -s "$SECRET2" -f OtherP99 </dev/null
systemctl is-active --quiet flux_agent2 || fail "第二个实例 flux_agent2 未运行"
systemctl is-active --quiet flux_agent || fail "共存安装影响了原实例"
grep -q "$SECRET" /etc/flux_agent/config.json || fail "原实例配置被覆盖"
for _ in $(seq 1 30); do
  st2=$(api /node/list | jq -r '.data[] | select(.name=="ci2") | .status')
  [[ "$st2" == "1" ]] && break
  sleep 2
done
[[ "$st2" == "1" ]] || fail "第二个实例未上线"
cp "$ROOT/install.sh" /tmp/agent_install.sh
AGENT_BINARY="$ART/gost-amd64" bash /tmp/agent_install.sh -a "127.0.0.1:${PORT}" -s "$SECRET" -f "$FP" </dev/null
[[ ! -d /etc/flux_agent3 ]] || fail "指纹相同重复安装不应新建实例"
# 面板改了指纹：靠节点密钥认出原实例，覆盖并写入新指纹
FP2=$(api /fingerprint/save '{"regenerate":true}' | jq -r .data.fingerprint)
[[ "$FP2" =~ ^[A-Za-z0-9]{8}$ && "$FP2" != "$FP" ]] || fail "重新生成指纹失败"
cp "$ROOT/install.sh" /tmp/agent_install.sh
AGENT_BINARY="$ART/gost-amd64" bash /tmp/agent_install.sh -a "127.0.0.1:${PORT}" -s "$SECRET" -f "$FP2" </dev/null
[[ ! -d /etc/flux_agent3 ]] || fail "改指纹后重装不应新建实例"
grep -q "\"fingerprint\": \"$FP2\"" /etc/flux_agent/config.json || fail "改指纹后重装未写入新指纹"
[[ -x /usr/local/bin/zf ]] && bash -n /usr/local/bin/zf || fail "zf 快捷菜单未安装"
echo "✅ 多实例共存正常，zf 已安装"

step "创建隧道和转发（转发到面板自身的 /flow/test）"
expect_ok /tunnel/create '{"name":"t","inNodeId":[{"nodeId":1,"chainType":1}],"type":1,"flow":1,"trafficRatio":1}'
expect_ok /forward/create '{"name":"f","tunnelId":1,"remoteAddr":"127.0.0.1:6365","strategy":"fifo","inPort":31000}'
sleep 1
[[ "$(curl -fs --max-time 5 http://127.0.0.1:31000/flow/test)" == "test" ]] || fail "经 31000 转发访问失败"
echo "✅ 转发可用"

step "修改转发端口 31000 -> 31001"
expect_ok /forward/update '{"id":1,"userId":1,"name":"f","remoteAddr":"127.0.0.1:6365","strategy":"fifo","inPort":31001}'
sleep 1
[[ "$(curl -fs --max-time 5 http://127.0.0.1:31001/flow/test)" == "test" ]] || fail "新端口 31001 不通"
if curl -fs --max-time 3 http://127.0.0.1:31000/flow/test >/dev/null 2>&1; then fail "旧端口 31000 未释放"; fi
echo "✅ 端口切换正常"

step "重启节点端后转发仍可用（从工作目录 gost.json 恢复）"
mkdir -p /etc/gost && echo '{}' > /etc/gost/gost.json   # 模拟残留的旧配置
systemctl restart flux_agent
for _ in $(seq 1 15); do
  curl -fs --max-time 3 http://127.0.0.1:31001/flow/test >/dev/null 2>&1 && break
  sleep 2
done
[[ "$(curl -fs --max-time 5 http://127.0.0.1:31001/flow/test)" == "test" ]] || fail "重启节点端后转发丢失"
rm -rf /etc/gost
echo "✅ 节点端重启后配置恢复"

step "面板更新（同一二进制）"
PANEL_BINARY="$ART/flux-panel-amd64" bash "$ROOT/panel_install.sh" update
[[ "$(api /forward/list | jq '.data | length')" == "1" ]] || fail "更新后数据丢失"
echo "✅ 更新后数据完好"

step "命令行备份 / 导出数据库"
(set -a; . /opt/flux-panel/flux-panel.env; set +a
 /opt/flux-panel/flux-panel -backup /tmp/zf-bk/a.db && /opt/flux-panel/flux-panel -export /tmp/zf-bk/a.sql) || fail "备份/导出命令失败"
[[ "$(sqlite3 /tmp/zf-bk/a.db 'select count(*) from forward')" == "1" ]] || fail "备份文件内容不对"
sqlite3 /tmp/zf-imp.db < /tmp/zf-bk/a.sql || fail "导出的 SQL 无法导入"
[[ "$(sqlite3 /tmp/zf-imp.db 'select count(*) from node')" == "2" ]] || fail "导入后数据不对"
echo "✅ 备份 / 导出正常"

echo
echo "🎉 冒烟测试全部通过"
