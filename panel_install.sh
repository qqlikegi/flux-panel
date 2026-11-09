#!/bin/bash
# =====================================================================
#  ForwardZ 面板管理脚本（二进制部署，无需 Docker）
#
#  面板是一个单独的二进制文件（Go 编写，前端页面已内置），无需 Java / Docker / Nginx，
#  由 systemd 管理，网页、接口、节点通信共用一个端口。
#
#  用法：
#    ./panel_install.sh              交互式菜单
#    ./panel_install.sh install      安装
#    ./panel_install.sh update       更新到最新版本
#    ./panel_install.sh uninstall    卸载
#    ./panel_install.sh status       查看状态
#
#    ./panel_install.sh reset-admin  重置管理员账号（重置后打开面板重新设置用户名和密码）
#
#  离线安装：PANEL_BINARY=/path/to/forwardz-panel-amd64 ./panel_install.sh install
#  非交互指定端口：PANEL_PORT=6365 ./panel_install.sh install
# =====================================================================

REPO="qqlikegi/forwardz"
INSTALL_DIR="/opt/forwardz-panel"
SERVICE_NAME="forwardz-panel"
BIN_NAME="forwardz-panel"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
ENV_FILE="${INSTALL_DIR}/forwardz-panel.env"
DB_FILE="${INSTALL_DIR}/data/forwardz.db"

RELEASE_URL="https://github.com/${REPO}/releases/latest/download"
DEFAULT_PORT=6365

GH_PROXY=""
COUNTRY=$(curl -s --max-time 5 https://ipinfo.io/country 2>/dev/null)
if [ "$COUNTRY" = "CN" ]; then
  GH_PROXY="https://ghfast.top/"
fi

# ---------------------------------------------------------------------
# 通用函数
# ---------------------------------------------------------------------
require_root() {
  if [[ $EUID -ne 0 ]]; then
    echo "❌ 请使用 root 用户运行此脚本"
    exit 1
  fi
}

check_deps() {
  local missing=()
  for cmd in curl tar systemctl; do
    command -v "$cmd" >/dev/null 2>&1 || missing+=("$cmd")
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    echo "❌ 缺少必要命令: ${missing[*]}，请先安装后重试"
    exit 1
  fi
}

is_installed() {
  [[ -x "${INSTALL_DIR}/${BIN_NAME}" && -f "$SERVICE_FILE" ]]
}

download() {
  local url="$1" out="$2"
  echo "⬇️ 下载: $url"
  if ! curl -fL --retry 3 --connect-timeout 15 -o "$out" "$url"; then
    echo "❌ 下载失败: $url"
    return 1
  fi
  [[ -s "$out" ]]
}

generate_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 16
  else
    tr -dc 'a-zA-Z0-9' </dev/urandom | head -c 32
  fi
}

current_port() {
  local p
  p=$(grep -E '^SERVER_PORT=' "$ENV_FILE" 2>/dev/null | cut -d= -f2)
  echo "${p:-$DEFAULT_PORT}"
}

current_db() {
  local p
  p=$(grep -E '^DB_PATH=' "$ENV_FILE" 2>/dev/null | cut -d= -f2-)
  echo "${p:-$DB_FILE}"
}

port_in_use() {
  local port="$1"
  if command -v ss >/dev/null 2>&1; then
    ss -ltnH 2>/dev/null | awk '{print $4}' | grep -Eq "[:.]${port}$"
  elif command -v netstat >/dev/null 2>&1; then
    netstat -ltn 2>/dev/null | awk '{print $4}' | grep -Eq "[:.]${port}$"
  else
    return 1
  fi
}

public_ip() {
  local ip
  ip=$(curl -s4 --max-time 5 https://api.ipify.org 2>/dev/null)
  echo "${ip:-服务器IP}"
}

wait_healthy() {
  local port="$1"
  echo "⏳ 等待面板启动..."
  for _ in $(seq 1 60); do
    if [[ "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${port}/flow/test")" == "200" ]]; then
      return 0
    fi
    sleep 2
  done
  return 1
}

# ---------------------------------------------------------------------
# 面板程序（单个二进制文件）
# ---------------------------------------------------------------------
panel_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) echo "" ;;
  esac
}

# 下载面板二进制到临时文件，成功后输出文件路径
fetch_panel_binary() {
  local arch tmp
  arch=$(panel_arch)
  if [[ -z "$arch" ]]; then
    echo "❌ 不支持的架构: $(uname -m)（仅支持 amd64 / arm64）" >&2
    return 1
  fi
  tmp=$(mktemp)
  if [[ -n "$PANEL_BINARY" ]]; then
    # 使用本地二进制（离线安装 / CI 测试）
    cp -f "$PANEL_BINARY" "$tmp" || { rm -f "$tmp"; return 1; }
  else
    download "${GH_PROXY}${RELEASE_URL}/forwardz-panel-${arch}" "$tmp" >&2 || { rm -f "$tmp"; return 1; }
  fi
  chmod +x "$tmp"
  if ! "$tmp" -v >/dev/null 2>&1; then
    echo "❌ 面板程序无法运行（文件损坏或架构不匹配）" >&2
    rm -f "$tmp"
    return 1
  fi
  echo "$tmp"
}

panel_version() {
  "${INSTALL_DIR}/${BIN_NAME}" -v 2>/dev/null | awk '{print $2}'
}

write_env() {
  local port="$1" secret="$2"
  cat > "$ENV_FILE" <<EOF
# ForwardZ 面板运行配置（修改后执行 systemctl restart ${SERVICE_NAME} 生效）
SERVER_PORT=${port}
JWT_SECRET=${secret}
DB_PATH=${DB_FILE}
LOG_DIR=${INSTALL_DIR}/logs
TZ=Asia/Shanghai
EOF
  chmod 600 "$ENV_FILE"
}

write_service() {
  cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=ForwardZ Panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=${ENV_FILE}
ExecStart=${INSTALL_DIR}/${BIN_NAME}
Restart=on-failure
RestartSec=5
TimeoutStopSec=30
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
}

# ---------------------------------------------------------------------
# zf 快捷管理菜单（与节点端共用 /usr/local/bin/zf）
# ---------------------------------------------------------------------
ZF_BIN="/usr/local/bin/zf"
SCRIPT_DIR="$(cd "$(dirname "$0")" 2>/dev/null && pwd)"

install_zf() {
  local tmp
  tmp=$(mktemp)
  if [[ -n "$ZF_SCRIPT" && -f "$ZF_SCRIPT" ]]; then
    cp -f "$ZF_SCRIPT" "$tmp"
  elif [[ -f "$SCRIPT_DIR/zf.sh" ]]; then
    cp -f "$SCRIPT_DIR/zf.sh" "$tmp"
  else
    curl -fsL --retry 2 --connect-timeout 15 -o "$tmp" "${GH_PROXY}${RELEASE_URL}/zf.sh" 2>/dev/null
  fi
  if [[ -s "$tmp" ]] && grep -q "FORWARDZ_ZF_MENU" "$tmp"; then
    install -m 755 "$tmp" "$ZF_BIN" && echo "⌨️ 快捷菜单已就绪：在 SSH 里输入 zf 即可管理"
  else
    echo "⚠️ 快捷菜单 zf 下载失败（不影响面板运行）"
  fi
  rm -f "$tmp"
}

# 本机没有面板也没有节点端时删除 zf
cleanup_zf() {
  local f
  for f in /etc/systemd/system/forwardz_agent*.service; do
    [[ -f "$f" ]] && return 0
  done
  rm -f "$ZF_BIN"
}

# ---------------------------------------------------------------------
# 安装 / 更新 / 卸载 / 状态
# ---------------------------------------------------------------------
install_panel() {
  require_root
  check_deps

  if is_installed; then
    echo "⚠️ 面板已安装在 ${INSTALL_DIR}，如需升级请选择“更新面板”"
    return 1
  fi

  local port="$PANEL_PORT"
  if [[ -z "$port" ]]; then
    read -p "面板端口（默认 ${DEFAULT_PORT}）: " port
  fi
  port=${port:-$DEFAULT_PORT}
  if ! [[ "$port" =~ ^[0-9]+$ ]] || (( port < 1 || port > 65535 )); then
    echo "❌ 端口无效: $port"
    return 1
  fi
  if port_in_use "$port"; then
    echo "❌ 端口 $port 已被占用，请换一个端口"
    return 1
  fi

  echo "🚀 开始安装面板到 ${INSTALL_DIR} ..."
  mkdir -p "${INSTALL_DIR}/data" "${INSTALL_DIR}/logs"

  local bin
  bin=$(fetch_panel_binary) || return 1
  mv -f "$bin" "${INSTALL_DIR}/${BIN_NAME}"

  write_env "$port" "$(generate_secret)"
  write_service
  systemctl enable "$SERVICE_NAME" >/dev/null 2>&1
  systemctl restart "$SERVICE_NAME"

  if wait_healthy "$port"; then
    local ip
    ip=$(public_ip)
    echo ""
    echo "🎉 面板安装完成"
    echo "🌐 访问地址: http://${ip}:${port}"
    echo "👤 首次打开面板时会要求设置管理员用户名和密码"
    echo "📌 添加节点前，请在“系统配置”里把面板地址设为 ${ip}:${port}"
    echo "📁 安装目录: ${INSTALL_DIR}    配置文件: ${ENV_FILE}"
    echo "📜 查看日志: journalctl -u ${SERVICE_NAME} -f"
    install_zf
  else
    echo "❌ 面板启动超时，请查看日志: journalctl -u ${SERVICE_NAME} -n 100"
    return 1
  fi
}

update_panel() {
  require_root
  check_deps

  if ! is_installed; then
    echo "❌ 面板未安装，请先选择安装"
    return 1
  fi

  echo "🔄 开始更新面板..."
  local bin
  bin=$(fetch_panel_binary) || return 1

  echo "🛑 停止面板服务..."
  systemctl stop "$SERVICE_NAME"

  # 备份旧版本和数据库，更新失败时回滚
  cp -f "${INSTALL_DIR}/${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}.bak"
  local db
  db=$(current_db)
  [[ -f "$db" ]] && cp -f "$db" "${db}.bak"

  mv -f "$bin" "${INSTALL_DIR}/${BIN_NAME}"
  write_service
  systemctl start "$SERVICE_NAME"

  local port
  port=$(current_port)
  if wait_healthy "$port"; then
    echo "✅ 更新完成，当前版本: $(panel_version)"
    install_zf
  else
    echo "❌ 新版本启动失败，正在回滚到旧版本..."
    systemctl stop "$SERVICE_NAME"
    mv -f "${INSTALL_DIR}/${BIN_NAME}.bak" "${INSTALL_DIR}/${BIN_NAME}"
    systemctl start "$SERVICE_NAME"
    echo "↩️ 已回滚，请查看日志: journalctl -u ${SERVICE_NAME} -n 100"
    return 1
  fi
}

reset_admin() {
  require_root
  if ! is_installed; then
    echo "❌ 面板未安装"
    return 1
  fi
  read -p "确认重置管理员账号吗？重置后打开面板重新设置管理员用户名和密码 (y/N): " confirm
  if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
    echo "❌ 已取消"
    return 0
  fi
  (set -a; . "$ENV_FILE"; set +a; "${INSTALL_DIR}/${BIN_NAME}" -reset-admin)
}

uninstall_panel() {
  require_root

  if [[ ! -d "$INSTALL_DIR" && ! -f "$SERVICE_FILE" ]]; then
    echo "ℹ️ 面板未安装"
    return 0
  fi

  read -p "确认卸载面板吗？(y/N): " confirm
  if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
    echo "❌ 取消卸载"
    return 0
  fi

  systemctl stop "$SERVICE_NAME" 2>/dev/null
  systemctl disable "$SERVICE_NAME" 2>/dev/null
  rm -f "$SERVICE_FILE"
  systemctl daemon-reload

  read -p "是否同时删除数据库（用户、节点、转发等数据，删除后无法恢复）？(y/N): " deldata
  if [[ "$deldata" == "y" || "$deldata" == "Y" ]]; then
    rm -rf "$INSTALL_DIR"
    echo "🧹 已删除 ${INSTALL_DIR}（含数据库）"
  else
    find "$INSTALL_DIR" -mindepth 1 -maxdepth 1 ! -name data -exec rm -rf {} +
    echo "🧹 已删除程序文件，数据库保留在 ${INSTALL_DIR}/data"
  fi
  cleanup_zf
  echo "✅ 卸载完成"
}

show_status() {
  if ! is_installed; then
    echo "ℹ️ 面板未安装"
    return 0
  fi
  echo "📦 版本: $(panel_version)"
  echo "🔧 服务: $(systemctl is-active "$SERVICE_NAME")"
  echo "🔌 端口: $(current_port)"
  echo "📁 目录: ${INSTALL_DIR}"
  echo "📜 日志: journalctl -u ${SERVICE_NAME} -f"
}

show_menu() {
  echo "==============================================="
  echo "            ForwardZ 面板管理脚本"
  echo "==============================================="
  echo "1. 安装面板"
  echo "2. 更新面板"
  echo "3. 卸载面板"
  echo "4. 查看状态"
  echo "5. 重置管理员密码"
  echo "6. 退出"
  echo "==============================================="
}

main() {
  case "$1" in
    install) install_panel; exit $? ;;
    update) update_panel; exit $? ;;
    uninstall) uninstall_panel; exit $? ;;
    status) show_status; exit $? ;;
    reset-admin) reset_admin; exit $? ;;
    "") ;;
    *) echo "用法: $0 [install|update|uninstall|status|reset-admin]"; exit 1 ;;
  esac

  while true; do
    show_menu
    read -p "请输入选项 (1-6): " choice
    case $choice in
      1) install_panel; exit $? ;;
      2) update_panel; exit $? ;;
      3) uninstall_panel; exit $? ;;
      4) show_status; echo "" ;;
      5) reset_admin; exit $? ;;
      6) echo "👋 退出脚本"; exit 0 ;;
      *) echo "❌ 无效选项，请输入 1-6"; echo "" ;;
    esac
  done
}

main "$@"
