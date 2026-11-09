#!/bin/bash
# =====================================================================
#  flux_agent 节点端安装脚本
#
#  一台机器可以安装多个节点端实例，分别对接不同的面板（多面板共存）：
#    实例名 = systemd 服务名，统一以 flux_agent 开头，例如 flux_agent、flux_agent_b
#    每个实例的目录为 /etc/<实例名>，服务为 <实例名>.service，互不影响
#
#  用法：
#    ./install.sh -a 面板地址 -s 密钥            安装（检测到已有实例时会询问覆盖还是共存）
#    ./install.sh -a 面板地址 -s 密钥 -n 名称    直接安装到指定实例（不存在则新建，存在则覆盖）
#    ./install.sh                                交互式菜单（安装 / 更新 / 卸载）
#
#  安装完成后，在 SSH 里输入 zf 即可打开快捷管理菜单。
#  离线安装：AGENT_BINARY=/path/to/gost-amd64 ./install.sh -a ... -s ...
# =====================================================================

REPO="qqlikegi/flux-panel"
SVC_PREFIX="flux_agent"
SYSTEMD_DIR="${FLUX_SYSTEMD_DIR:-/etc/systemd/system}"
AGENT_ROOT="${FLUX_AGENT_ROOT:-/etc}"   # 节点端实例目录的上级目录（仅测试时覆盖）
ZF_BIN="/usr/local/bin/zf"
SCRIPT_DIR="$(cd "$(dirname "$0")" 2>/dev/null && pwd)"

# 获取系统架构
get_architecture() {
  case "$(uname -m)" in
    x86_64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) echo "amd64" ;;  # 默认使用 amd64
  esac
}

RELEASE_URL="https://github.com/${REPO}/releases/latest/download"
GH_PROXY=""
COUNTRY=$(curl -s --max-time 5 https://ipinfo.io/country 2>/dev/null)
if [ "$COUNTRY" = "CN" ]; then
  GH_PROXY="https://ghfast.top/"
fi
DOWNLOAD_URL="${GH_PROXY}${RELEASE_URL}/gost-$(get_architecture)"

# ---------------------------------------------------------------------
# 交互输入：通过管道执行脚本时也从终端读取
# ---------------------------------------------------------------------
HAS_TTY=0
if [[ -t 0 ]]; then
  HAS_TTY=1
elif { : </dev/tty; } 2>/dev/null; then
  HAS_TTY=2
fi

# ask "提示" → 结果在 $REPLY；没有终端时返回 1
ask() {
  REPLY=""
  case $HAS_TTY in
    1) read -r -p "$1" REPLY ;;
    2) read -r -p "$1" REPLY </dev/tty ;;
    *) return 1 ;;
  esac
}

# ---------------------------------------------------------------------
# 实例管理
# ---------------------------------------------------------------------
valid_instance_name() {
  [[ "$1" =~ ^${SVC_PREFIX}[A-Za-z0-9_-]*$ && ${#1} -le 48 ]]
}

# 列出本机全部节点端实例（服务文件或带 config.json 的安装目录）
list_instances() {
  local f name
  {
    for f in "$SYSTEMD_DIR"/${SVC_PREFIX}*.service; do
      [[ -f "$f" ]] || continue
      name=$(basename "$f" .service)
      valid_instance_name "$name" && echo "$name"
    done
    for f in "$AGENT_ROOT"/${SVC_PREFIX}*/config.json; do
      [[ -f "$f" ]] || continue
      name=$(basename "$(dirname "$f")")
      valid_instance_name "$name" && echo "$name"
    done
  } | sort -u
}

instance_exists() {
  [[ -f "$SYSTEMD_DIR/$1.service" || -d "$AGENT_ROOT/$1" ]]
}

# 读取实例 config.json 中的字段
instance_conf() {
  local file="$AGENT_ROOT/$1/config.json"
  [[ -f "$file" ]] || return 0
  sed -n "s/.*\"$2\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$file" | head -n1
}

instance_state() {
  systemctl is-active "$1" 2>/dev/null || true
}

print_instances() {
  local i=1 name
  for name in "$@"; do
    printf "  %d. %-20s 面板: %-24s 状态: %s\n" "$i" "$name" "$(instance_conf "$name" addr)" "$(instance_state "$name")"
    i=$((i + 1))
  done
}

# 把用户输入的名称规范成实例名：不以 flux_agent 开头时自动加前缀 flux_agent_
normalize_name() {
  local n="${1//[[:space:]]/}"
  [[ -z "$n" ]] && return 1
  [[ "$n" == ${SVC_PREFIX}* ]] || n="${SVC_PREFIX}_${n}"
  valid_instance_name "$n" || return 1
  echo "$n"
}

next_free_name() {
  local i=2
  while instance_exists "${SVC_PREFIX}${i}"; do i=$((i + 1)); done
  echo "${SVC_PREFIX}${i}"
}

# 选择一个已有实例，结果在 $PICKED；只有一个时直接返回它
pick_instance() {
  local prompt="$1"; shift
  local list=("$@")
  PICKED=""
  if [[ ${#list[@]} -eq 1 ]]; then
    PICKED="${list[0]}"
    return 0
  fi
  print_instances "${list[@]}"
  ask "$prompt" || return 1
  if [[ "$REPLY" =~ ^[0-9]+$ ]] && (( REPLY >= 1 && REPLY <= ${#list[@]} )); then
    PICKED="${list[$((REPLY - 1))]}"
    return 0
  fi
  echo "❌ 无效选项"
  return 1
}

# 询问新实例名称（共存安装）
ask_new_instance() {
  local def n
  def=$(next_free_name)
  while true; do
    if ! ask "请输入新实例名称（直接回车使用 ${def}）: "; then
      INSTANCE="$def"
      return 0
    fi
    if [[ -z "$REPLY" ]]; then
      n="$def"
    elif ! n=$(normalize_name "$REPLY"); then
      echo "❌ 名称只能包含字母、数字、下划线和短横线"
      continue
    fi
    if instance_exists "$n"; then
      echo "❌ 实例 $n 已存在，请换一个名称"
      continue
    fi
    INSTANCE="$n"
    echo "📛 新实例: 服务名 ${n}.service，目录 $AGENT_ROOT/${n}"
    return 0
  done
}

# 决定本次安装到哪个实例，结果在 $INSTANCE
choose_install_instance() {
  if [[ -n "$INSTANCE" ]]; then
    local n
    if ! n=$(normalize_name "$INSTANCE"); then
      echo "❌ 实例名称无效: $INSTANCE（只能包含字母、数字、下划线和短横线）"
      exit 1
    fi
    INSTANCE="$n"
    return 0
  fi

  local existing=() name
  mapfile -t existing < <(list_instances)
  if [[ ${#existing[@]} -eq 0 ]]; then
    INSTANCE="$SVC_PREFIX"
    return 0
  fi

  # 同一个面板的同一个节点（换机重装、重复执行安装命令）：直接覆盖原实例
  for name in "${existing[@]}"; do
    if [[ "$(instance_conf "$name" addr)" == "$SERVER_ADDR" && "$(instance_conf "$name" secret)" == "$SECRET" ]]; then
      echo "🔍 本机已安装过这个节点（实例 ${name}），将覆盖重装该实例"
      INSTANCE="$name"
      return 0
    fi
  done

  echo ""
  echo "⚠️ 检测到本机已经安装了 ${#existing[@]} 个节点端实例："
  print_instances "${existing[@]}"
  echo ""
  echo "本次安装的节点（面板 ${SERVER_ADDR}）要如何处理？"
  echo "  1. 共存安装（新建一个实例，原有实例继续运行，适合一台机器对接多个面板）"
  echo "  2. 覆盖已有实例（原实例的连接配置会被替换）"
  echo "  3. 取消安装"
  if ! ask "请输入选项 (1-3，默认 1): "; then
    INSTANCE=$(next_free_name)
    echo "ℹ️ 当前没有可交互的终端，自动共存安装为实例 ${INSTANCE}"
    return 0
  fi
  case "${REPLY:-1}" in
    1)
      ask_new_instance
      echo "⚠️ 提醒：多个面板在同一台机器上分配的端口范围不能重叠，否则端口冲突会导致转发失败"
      ;;
    2)
      if [[ ${#existing[@]} -gt 1 ]]; then
        echo "请选择要覆盖的实例："
      fi
      pick_instance "请输入序号: " "${existing[@]}" || exit 1
      INSTANCE="$PICKED"
      ask "确认覆盖实例 ${INSTANCE}（当前对接面板 $(instance_conf "$INSTANCE" addr)）？(y/N): " || true
      if [[ "$REPLY" != "y" && "$REPLY" != "Y" ]]; then
        echo "❌ 已取消"
        exit 0
      fi
      ;;
    *)
      echo "❌ 已取消安装"
      exit 0
      ;;
  esac
}

# ---------------------------------------------------------------------
# zf 快捷管理菜单
# ---------------------------------------------------------------------
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
  if [[ -s "$tmp" ]] && grep -q "FLUX_ZF_MENU" "$tmp"; then
    install -m 755 "$tmp" "$ZF_BIN" && echo "⌨️ 已安装快捷菜单：在 SSH 里输入 zf 即可管理"
  else
    echo "⚠️ 快捷菜单 zf 下载失败（不影响节点运行）"
  fi
  rm -f "$tmp"
}

# 本机没有任何 flux 服务时删除 zf
cleanup_zf() {
  if [[ -z "$(list_instances)" && ! -f "$SYSTEMD_DIR/flux-panel.service" ]]; then
    rm -f "$ZF_BIN"
  fi
}

# ---------------------------------------------------------------------
# 其他
# ---------------------------------------------------------------------
show_menu() {
  echo "==============================================="
  echo "            flux_agent 节点端管理脚本"
  echo "==============================================="
  echo "请选择操作："
  echo "1. 安装"
  echo "2. 更新"
  echo "3. 卸载"
  echo "4. 退出"
  echo "==============================================="
}

# 删除脚本自身
delete_self() {
  echo ""
  echo "🗑️ 操作已完成，正在清理脚本文件..."
  SCRIPT_PATH="$(readlink -f "$0" 2>/dev/null || realpath "$0" 2>/dev/null || echo "$0")"
  sleep 1
  rm -f "$SCRIPT_PATH" && echo "✅ 脚本文件已删除" || echo "❌ 删除脚本文件失败"
}

# 检查并安装 tcpkill
check_and_install_tcpkill() {
  if command -v tcpkill &> /dev/null; then
    return 0
  fi
  local OS_TYPE SUDO_CMD DISTRO
  OS_TYPE=$(uname -s)
  if [[ $EUID -ne 0 ]]; then
    SUDO_CMD="sudo"
  else
    SUDO_CMD=""
  fi
  if [[ "$OS_TYPE" == "Darwin" ]]; then
    if command -v brew &> /dev/null; then
      brew install dsniff &> /dev/null
    fi
    return 0
  fi
  if [ -f /etc/os-release ]; then
    DISTRO=$(. /etc/os-release && echo "$ID")
  elif [ -f /etc/redhat-release ]; then
    DISTRO="rhel"
  elif [ -f /etc/debian_version ]; then
    DISTRO="debian"
  else
    return 0
  fi
  case $DISTRO in
    ubuntu|debian)
      $SUDO_CMD apt update &> /dev/null
      $SUDO_CMD apt install -y dsniff &> /dev/null
      ;;
    centos|rhel|fedora)
      if command -v dnf &> /dev/null; then
        $SUDO_CMD dnf install -y dsniff &> /dev/null
      elif command -v yum &> /dev/null; then
        $SUDO_CMD yum install -y dsniff &> /dev/null
      fi
      ;;
    alpine) $SUDO_CMD apk add --no-cache dsniff &> /dev/null ;;
    arch|manjaro) $SUDO_CMD pacman -S --noconfirm dsniff &> /dev/null ;;
    opensuse*|sles) $SUDO_CMD zypper install -y dsniff &> /dev/null ;;
    gentoo) $SUDO_CMD emerge --ask=n net-analyzer/dsniff &> /dev/null ;;
    void) $SUDO_CMD xbps-install -Sy dsniff &> /dev/null ;;
  esac
  return 0
}

# 获取用户输入的配置参数
get_config_params() {
  if [[ -z "$SERVER_ADDR" || -z "$SECRET" ]]; then
    echo "请输入配置参数："
    if [[ -z "$SERVER_ADDR" ]]; then
      ask "服务器地址: " && SERVER_ADDR="$REPLY"
    fi
    if [[ -z "$SECRET" ]]; then
      ask "密钥: " && SECRET="$REPLY"
    fi
    if [[ -z "$SERVER_ADDR" || -z "$SECRET" ]]; then
      echo "❌ 参数不完整，操作取消。"
      exit 1
    fi
  fi
}

# 解析命令行参数
while getopts "a:s:n:" opt; do
  case $opt in
    a) SERVER_ADDR="$OPTARG" ;;
    s) SECRET="$OPTARG" ;;
    n) INSTANCE="$OPTARG" ;;
    *) echo "❌ 无效参数"; exit 1 ;;
  esac
done

# 下载节点端二进制到临时文件，成功后输出路径
fetch_agent_binary() {
  local tmp
  tmp=$(mktemp)
  if [[ -n "$AGENT_BINARY" ]]; then
    # 使用本地二进制（离线安装 / CI 测试）
    echo "📦 使用本地文件: $AGENT_BINARY" >&2
    cp -f "$AGENT_BINARY" "$tmp"
  else
    echo "⬇️ 下载 flux_agent: $DOWNLOAD_URL" >&2
    curl -fL --retry 3 --connect-timeout 15 "$DOWNLOAD_URL" -o "$tmp" >&2
  fi
  if [[ ! -s "$tmp" ]]; then
    echo "❌ 下载失败，请检查网络或下载链接。" >&2
    rm -f "$tmp"
    return 1
  fi
  chmod +x "$tmp"
  echo "$tmp"
}

# ---------------------------------------------------------------------
# 安装
# ---------------------------------------------------------------------
install_flux_agent() {
  get_config_params
  choose_install_instance

  local dir="$AGENT_ROOT/${INSTANCE}"
  local svc="${INSTANCE}"
  local service_file="$SYSTEMD_DIR/${svc}.service"
  echo "🚀 开始安装节点端（实例 ${INSTANCE}）..."

  check_and_install_tcpkill

  local bin
  bin=$(fetch_agent_binary) || exit 1
  echo "✅ 下载完成"

  # 停止已有服务
  if [[ -f "$service_file" ]]; then
    echo "🔍 检测到已存在的 ${svc} 服务，先停止"
    systemctl stop "$svc" 2>/dev/null && echo "🛑 停止服务"
  fi

  # 覆盖对接另一个面板的实例时，旧面板下发的转发配置不能保留
  local old_addr
  old_addr=$(instance_conf "$INSTANCE" addr)
  mkdir -p "$dir"
  if [[ -n "$old_addr" && "$old_addr" != "$SERVER_ADDR" && -f "$dir/gost.json" ]]; then
    echo "🧹 原实例对接的是面板 ${old_addr}，清空旧的转发配置"
    mv -f "$dir/gost.json" "$dir/gost.json.old"
  fi

  mv -f "$bin" "$dir/flux_agent"
  chmod +x "$dir/flux_agent"
  echo "🔎 flux_agent 版本：$("$dir/flux_agent" -V 2>/dev/null)"

  # 写入 config.json（安装时总是创建新的）
  echo "📄 创建新配置: config.json"
  cat > "$dir/config.json" <<EOF
{
  "addr": "$SERVER_ADDR",
  "secret": "$SECRET"
}
EOF

  if [[ -f "$dir/gost.json" ]]; then
    echo "⏭️ 跳过配置文件: gost.json (已存在)"
  else
    echo "📄 创建新配置: gost.json"
    echo "{}" > "$dir/gost.json"
  fi
  chmod 600 "$dir"/*.json

  # 创建 systemd 服务
  cat > "$service_file" <<EOF
[Unit]
Description=Flux_agent Proxy Service (${INSTANCE}, panel ${SERVER_ADDR})
After=network.target

[Service]
WorkingDirectory=$dir
ExecStart=$dir/flux_agent
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable "$svc" >/dev/null 2>&1
  systemctl start "$svc"

  install_zf

  echo "🔄 检查服务状态..."
  if systemctl is-active --quiet "$svc"; then
    echo "✅ 安装完成，${svc} 服务已启动并设置为开机启动。"
    echo "📁 配置目录: $dir"
    echo "🔧 服务名称: ${svc}    状态: $(systemctl is-active "$svc")"
    local count
    count=$(list_instances | wc -l)
    if (( count > 1 )); then
      echo "📚 本机共有 ${count} 个节点端实例，输入 zf 可选择管理"
    fi
  else
    echo "❌ ${svc} 服务启动失败，请执行以下命令查看日志："
    echo "journalctl -u ${svc} -f"
  fi
}

# ---------------------------------------------------------------------
# 更新
# ---------------------------------------------------------------------
update_one() {
  local name="$1" bin="$2" dir="$AGENT_ROOT/$1"
  echo "🔄 更新实例 ${name} ..."
  cp -f "$dir/flux_agent" "$dir/flux_agent.bak" 2>/dev/null
  systemctl stop "$name" 2>/dev/null
  cp -f "$bin" "$dir/flux_agent"
  chmod +x "$dir/flux_agent"
  systemctl start "$name"
  sleep 2
  if systemctl is-active --quiet "$name"; then
    rm -f "$dir/flux_agent.bak"
    echo "✅ ${name} 已更新：$("$dir/flux_agent" -V 2>/dev/null)"
  else
    echo "❌ ${name} 新版本启动失败，回滚到旧版本"
    systemctl stop "$name" 2>/dev/null
    mv -f "$dir/flux_agent.bak" "$dir/flux_agent"
    systemctl start "$name"
  fi
}

update_flux_agent() {
  local existing=() targets=() name
  mapfile -t existing < <(list_instances)
  if [[ ${#existing[@]} -eq 0 ]]; then
    echo "❌ flux_agent 未安装，请先选择安装。"
    return 1
  fi
  if [[ ${#existing[@]} -eq 1 ]]; then
    targets=("${existing[@]}")
  else
    echo "本机有 ${#existing[@]} 个节点端实例："
    print_instances "${existing[@]}"
    echo "  0. 全部更新"
    ask "请选择要更新的实例 (默认 0): " || REPLY=0
    if [[ -z "$REPLY" || "$REPLY" == "0" ]]; then
      targets=("${existing[@]}")
    elif [[ "$REPLY" =~ ^[0-9]+$ ]] && (( REPLY >= 1 && REPLY <= ${#existing[@]} )); then
      targets=("${existing[$((REPLY - 1))]}")
    else
      echo "❌ 无效选项"
      return 1
    fi
  fi

  check_and_install_tcpkill
  local bin
  bin=$(fetch_agent_binary) || return 1
  for name in "${targets[@]}"; do
    update_one "$name" "$bin"
  done
  rm -f "$bin"
  install_zf
}

# ---------------------------------------------------------------------
# 卸载
# ---------------------------------------------------------------------
uninstall_one() {
  local name="$1"
  systemctl stop "$name" 2>/dev/null
  systemctl disable "$name" 2>/dev/null
  rm -f "$SYSTEMD_DIR/${name}.service"
  [[ -n "$name" && -d "$AGENT_ROOT/$name" ]] && rm -rf "$AGENT_ROOT/$name"
  echo "🧹 已卸载实例 ${name}"
}

uninstall_flux_agent() {
  local existing=()
  mapfile -t existing < <(list_instances)
  if [[ ${#existing[@]} -eq 0 ]]; then
    echo "ℹ️ flux_agent 未安装"
    return 0
  fi
  if [[ ${#existing[@]} -gt 1 ]]; then
    echo "本机有 ${#existing[@]} 个节点端实例，请选择要卸载的："
  fi
  pick_instance "请输入序号: " "${existing[@]}" || return 1
  local name="$PICKED"
  ask "确认卸载实例 ${name}（面板 $(instance_conf "$name" addr)）吗？此操作将删除该实例的所有文件 (y/N): " || true
  if [[ "$REPLY" != "y" && "$REPLY" != "Y" ]]; then
    echo "❌ 取消卸载"
    return 0
  fi
  uninstall_one "$name"
  systemctl daemon-reload
  cleanup_zf
  echo "✅ 卸载完成"
}

# ---------------------------------------------------------------------
# 主逻辑
# ---------------------------------------------------------------------
main() {
  # 提供了命令行参数，直接执行安装
  if [[ -n "$SERVER_ADDR" && -n "$SECRET" ]]; then
    install_flux_agent
    delete_self
    exit 0
  fi

  while true; do
    show_menu
    ask "请输入选项 (1-4): " || { echo "❌ 没有可交互的终端，请使用 -a 和 -s 参数安装"; exit 1; }
    case $REPLY in
      1) install_flux_agent; delete_self; exit 0 ;;
      2) update_flux_agent; delete_self; exit 0 ;;
      3) uninstall_flux_agent; delete_self; exit 0 ;;
      4) echo "👋 退出脚本"; delete_self; exit 0 ;;
      *) echo "❌ 无效选项，请输入 1-4"; echo "" ;;
    esac
  done
}

main
