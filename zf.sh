#!/bin/bash
# FLUX_ZF_MENU
# =====================================================================
#  zf —— flux-panel 面板 / flux_agent 节点端 快捷管理菜单
#
#  由 panel_install.sh 和 install.sh 安装到 /usr/local/bin/zf，SSH 里输入 zf 打开。
#  自动找出本机的面板（flux-panel.service）和全部节点端实例（flux_agent*.service），
#  有多个时先列出来让你选择要管理哪一个。
# =====================================================================

REPO="qqlikegi/flux-panel"
RELEASE_URL="https://github.com/${REPO}/releases/latest/download"
SYSTEMD_DIR="${FLUX_SYSTEMD_DIR:-/etc/systemd/system}"
AGENT_ROOT="${FLUX_AGENT_ROOT:-/etc}"   # 节点端实例目录的上级目录（仅测试时覆盖）
ZF_BIN="/usr/local/bin/zf"

PANEL_DIR="${FLUX_PANEL_DIR:-/opt/flux-panel}"
PANEL_SVC="flux-panel"
PANEL_ENV="${PANEL_DIR}/flux-panel.env"
AGENT_PREFIX="flux_agent"

if [[ $EUID -ne 0 ]]; then
  echo "❌ 请使用 root 用户运行 zf"
  exit 1
fi

# ---------------------------------------------------------------------
# 通用
# ---------------------------------------------------------------------
pause() { echo ""; read -r -p "按回车键继续..." _; }
now() { date +%Y%m%d-%H%M%S; }

arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) echo "" ;;
  esac
}

GH_PROXY_DONE=0
GH_PROXY=""
gh_proxy() {
  if [[ $GH_PROXY_DONE -eq 0 ]]; then
    GH_PROXY_DONE=1
    if [[ "$(curl -s --max-time 5 https://ipinfo.io/country 2>/dev/null)" == "CN" ]]; then
      GH_PROXY="https://ghfast.top/"
    fi
  fi
  echo "$GH_PROXY"
}

download() {
  local url="$1" out="$2"
  echo "⬇️ 下载: $url"
  curl -fL --retry 3 --connect-timeout 15 -o "$out" "$url" && [[ -s "$out" ]]
}

# 更新 zf 自身（失败不影响其他操作）
update_zf() {
  local tmp
  tmp=$(mktemp)
  if curl -fsL --retry 2 --connect-timeout 15 -o "$tmp" "$(gh_proxy)${RELEASE_URL}/zf.sh" 2>/dev/null \
    && grep -q "FLUX_ZF_MENU" "$tmp" && bash -n "$tmp" 2>/dev/null; then
    install -m 755 "$tmp" "$ZF_BIN" && echo "⌨️ zf 菜单已同步到最新版"
  fi
  rm -f "$tmp"
}

# 询问保存目录，结果在 $TARGET_DIR。
# 输入 /root 或 /root/ 都表示保存到 /root 目录；目录不存在时询问是否新建。
ask_dir() {
  local p
  TARGET_DIR=""
  read -r -p "请输入保存目录（例如 /root，直接回车取消）: " p
  p="${p#"${p%%[![:space:]]*}"}"
  p="${p%"${p##*[![:space:]]}"}"
  if [[ -z "$p" ]]; then
    echo "❌ 已取消"
    return 1
  fi
  [[ "$p" == "~" ]] && p="$HOME"
  [[ "$p" == "~/"* ]] && p="$HOME/${p#\~/}"
  [[ "$p" != /* ]] && p="$PWD/$p"
  while [[ ${#p} -gt 1 && "$p" == */ ]]; do p="${p%/}"; done

  if [[ -e "$p" && ! -d "$p" ]]; then
    echo "❌ $p 已存在，但它是文件而不是文件夹"
    return 1
  fi
  if [[ ! -d "$p" ]]; then
    echo "⚠️ 目标文件夹 $p 不存在，需要新建文件夹，是否继续？"
    echo "  1. 继续"
    echo "  2. 取消"
    local c
    read -r -p "请输入选项 (1-2): " c
    if [[ "$c" != "1" ]]; then
      echo "❌ 已取消"
      return 1
    fi
    if ! mkdir -p "$p"; then
      echo "❌ 新建文件夹失败: $p"
      return 1
    fi
    echo "📁 已新建文件夹: $p"
  fi
  TARGET_DIR="$p"
}

confirm() {
  local c
  read -r -p "$1 (y/N): " c
  [[ "$c" == "y" || "$c" == "Y" ]]
}

svc_state() { systemctl is-active "$1" 2>/dev/null || true; }

svc_show() {
  systemctl status "$1" --no-pager -l 2>/dev/null | head -n 12
  echo ""
  echo "📜 实时日志: journalctl -u $1 -f"
}

# 本机没有任何 flux 服务时删除 zf 自身
cleanup_zf() {
  if [[ -z "$(list_agents)" && ! -f "$SYSTEMD_DIR/${PANEL_SVC}.service" ]]; then
    rm -f "$ZF_BIN"
    echo "🧹 本机已没有 flux 服务，zf 菜单已一并删除"
    exit 0
  fi
}

# ---------------------------------------------------------------------
# 发现本机服务
# ---------------------------------------------------------------------
panel_installed() {
  [[ -f "$SYSTEMD_DIR/${PANEL_SVC}.service" || -x "${PANEL_DIR}/flux-panel" ]]
}

list_agents() {
  local f name
  {
    for f in "$SYSTEMD_DIR"/${AGENT_PREFIX}*.service; do
      [[ -f "$f" ]] || continue
      name=$(basename "$f" .service)
      [[ "$name" =~ ^${AGENT_PREFIX}[A-Za-z0-9_-]*$ ]] && echo "$name"
    done
    for f in "$AGENT_ROOT"/${AGENT_PREFIX}*/config.json; do
      [[ -f "$f" ]] || continue
      name=$(basename "$(dirname "$f")")
      [[ "$name" =~ ^${AGENT_PREFIX}[A-Za-z0-9_-]*$ ]] && echo "$name"
    done
  } | sort -u
}

agent_conf() {
  sed -n "s/.*\"$2\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$AGENT_ROOT/$1/config.json" 2>/dev/null | head -n1
}
agent_addr() { agent_conf "$1" addr; }
agent_fp() { local f; f=$(agent_conf "$1" fingerprint); echo "${f:--}"; }

# ---------------------------------------------------------------------
# 面板
# ---------------------------------------------------------------------
panel_env_get() {
  grep -E "^$1=" "$PANEL_ENV" 2>/dev/null | tail -n1 | cut -d= -f2-
}

panel_db() {
  local p
  p=$(panel_env_get DB_PATH)
  echo "${p:-${PANEL_DIR}/data/gost.db}"
}

panel_bin_supports() {
  "${PANEL_DIR}/flux-panel" -h 2>&1 | grep -q -- "-$1"
}

panel_fp() {
  local f=""
  panel_bin_supports fingerprint && f=$(panel_cli -fingerprint 2>/dev/null)
  echo "${f:--}"
}

panel_cli() {
  (set -a; [[ -f "$PANEL_ENV" ]] && . "$PANEL_ENV"; set +a; "${PANEL_DIR}/flux-panel" "$@")
}

panel_update() {
  local tmp
  tmp=$(mktemp)
  if ! download "$(gh_proxy)${RELEASE_URL}/panel_install.sh" "$tmp"; then
    echo "❌ 下载更新脚本失败，请检查网络"
    rm -f "$tmp"
    return 1
  fi
  bash "$tmp" update
  rm -f "$tmp"
  update_zf
}

panel_backup() {
  ask_dir || return 1
  local db file
  db=$(panel_db)
  file="${TARGET_DIR}/flux-panel-backup-$(now).db"
  if [[ ! -f "$db" ]]; then
    echo "❌ 找不到数据库文件: $db"
    return 1
  fi
  if panel_bin_supports backup; then
    panel_cli -backup "$file" || return 1
  elif command -v sqlite3 >/dev/null 2>&1; then
    sqlite3 "$db" ".backup '$file'" || return 1
  else
    echo "ℹ️ 当前面板版本较旧，需要短暂停止面板来复制数据库"
    systemctl stop "$PANEL_SVC"
    cp -f "$db" "$file"
    local rc=$?
    systemctl start "$PANEL_SVC"
    [[ $rc -eq 0 ]] || return 1
  fi
  echo "✅ 数据库已备份到: $file ($(du -h "$file" | cut -f1))"
  echo "💡 恢复方法: systemctl stop ${PANEL_SVC} && cp \"$file\" \"$db\" && rm -f \"$db-wal\" \"$db-shm\" && systemctl start ${PANEL_SVC}"
}

panel_export() {
  ask_dir || return 1
  local db file
  db=$(panel_db)
  file="${TARGET_DIR}/flux-panel-export-$(now).sql"
  if [[ ! -f "$db" ]]; then
    echo "❌ 找不到数据库文件: $db"
    return 1
  fi
  if panel_bin_supports export; then
    panel_cli -export "$file" || return 1
  elif command -v sqlite3 >/dev/null 2>&1; then
    sqlite3 "$db" .dump > "$file" || { rm -f "$file"; return 1; }
  else
    echo "❌ 当前面板版本不支持导出，请先选择“更新到最新版”"
    return 1
  fi
  echo "✅ 数据库已导出为 SQL: $file ($(du -h "$file" | cut -f1))"
  echo "💡 导入方法: sqlite3 新数据库.db < \"$file\""
}

panel_uninstall() {
  confirm "确认卸载面板吗？" || { echo "❌ 已取消"; return 1; }
  systemctl stop "$PANEL_SVC" 2>/dev/null
  systemctl disable "$PANEL_SVC" 2>/dev/null
  rm -f "$SYSTEMD_DIR/${PANEL_SVC}.service"
  systemctl daemon-reload
  if confirm "是否同时删除数据库（用户、节点、转发等数据，删除后无法恢复）？"; then
    rm -rf "$PANEL_DIR"
    echo "🧹 已删除 ${PANEL_DIR}（含数据库）"
  else
    find "$PANEL_DIR" -mindepth 1 -maxdepth 1 ! -name data -exec rm -rf {} + 2>/dev/null
    echo "🧹 已删除程序文件，数据库保留在 ${PANEL_DIR}/data"
  fi
  echo "✅ 面板已卸载"
  cleanup_zf
  return 0
}

panel_menu() {
  while true; do
    clear 2>/dev/null
    echo "==============================================="
    echo "        flux-panel 面板 快捷管理（zf）"
    echo "==============================================="
    echo "  版本: $("${PANEL_DIR}/flux-panel" -v 2>/dev/null | awk '{print $2}')    状态: $(svc_state "$PANEL_SVC")    端口: $(panel_env_get SERVER_PORT)"
    echo "  面板指纹: $(panel_fp)（系统配置里可查看和修改）"
    echo "-----------------------------------------------"
    echo "  1. 更新到最新版（GitHub）"
    echo "  2. 启动"
    echo "  3. 停止运行"
    echo "  4. 重启"
    echo "  5. 查看状态"
    echo "  6. 备份数据库"
    echo "  7. 导出数据库（SQL）"
    echo "  8. 卸载"
    echo "  0. $BACK_LABEL"
    echo "==============================================="
    local c
    read -r -p "请输入选项: " c
    case "$c" in
      1) panel_update; pause ;;
      2) systemctl start "$PANEL_SVC" && echo "✅ 已启动"; pause ;;
      3) systemctl stop "$PANEL_SVC" && echo "🛑 已停止运行"; pause ;;
      4) systemctl restart "$PANEL_SVC" && echo "✅ 已重启"; pause ;;
      5) svc_show "$PANEL_SVC"; pause ;;
      6) panel_backup; pause ;;
      7) panel_export; pause ;;
      8) panel_uninstall && { pause; return 0; }; pause ;;
      0) return 0 ;;
      *) echo "❌ 无效选项"; sleep 1 ;;
    esac
  done
}

# ---------------------------------------------------------------------
# 节点端实例
# ---------------------------------------------------------------------
agent_update_one() {
  local name="$1" bin="$2" dir="$AGENT_ROOT/$1"
  echo "🔄 更新实例 ${name} ..."
  cp -f "$dir/flux_agent" "$dir/flux_agent.bak" 2>/dev/null
  systemctl stop "$name" 2>/dev/null
  cp -f "$bin" "$dir/flux_agent" && chmod +x "$dir/flux_agent"
  systemctl start "$name"
  sleep 2
  if systemctl is-active --quiet "$name"; then
    rm -f "$dir/flux_agent.bak"
    echo "✅ ${name} 已更新: $("$dir/flux_agent" -V 2>/dev/null)"
  else
    echo "❌ ${name} 新版本启动失败，回滚到旧版本"
    systemctl stop "$name" 2>/dev/null
    mv -f "$dir/flux_agent.bak" "$dir/flux_agent"
    systemctl start "$name"
  fi
}

agent_update() {
  local name="$1" a tmp targets=("$1") others=()
  a=$(arch)
  [[ -z "$a" ]] && { echo "❌ 不支持的架构: $(uname -m)"; return 1; }
  mapfile -t others < <(list_agents | grep -vx "$name")
  if [[ ${#others[@]} -gt 0 ]]; then
    echo "本机还有 ${#others[@]} 个节点端实例: ${others[*]}"
    echo "  1. 只更新 ${name}"
    echo "  2. 全部一起更新"
    local c
    read -r -p "请输入选项 (1-2，默认 1): " c
    [[ "$c" == "2" ]] && targets+=("${others[@]}")
  fi
  tmp=$(mktemp)
  if ! download "$(gh_proxy)${RELEASE_URL}/gost-${a}" "$tmp"; then
    echo "❌ 下载失败，请检查网络"
    rm -f "$tmp"
    return 1
  fi
  chmod +x "$tmp"
  if ! "$tmp" -V >/dev/null 2>&1; then
    echo "❌ 下载的程序无法运行（文件损坏或架构不匹配）"
    rm -f "$tmp"
    return 1
  fi
  for n in "${targets[@]}"; do
    agent_update_one "$n" "$tmp"
  done
  rm -f "$tmp"
  update_zf
}

# 节点端没有数据库，“数据库”就是连接配置 config.json 和面板下发的转发配置 gost.json
agent_backup() {
  local name="$1" dir="$AGENT_ROOT/$1" file
  ask_dir || return 1
  file="${TARGET_DIR}/${name}-backup-$(now).tar.gz"
  if ! tar -czf "$file" -C "$AGENT_ROOT" "$name/config.json" "$name/gost.json" 2>/dev/null; then
    rm -f "$file"
    echo "❌ 备份失败，请确认 ${dir} 下存在 config.json 和 gost.json"
    return 1
  fi
  chmod 600 "$file"
  echo "✅ 节点配置已备份到: $file"
  echo "💡 恢复方法: systemctl stop ${name} && tar -xzf \"$file\" -C $AGENT_ROOT && systemctl start ${name}"
}

agent_export() {
  local name="$1" dir="$AGENT_ROOT/$1" file
  ask_dir || return 1
  file="${TARGET_DIR}/${name}-gost-$(now).json"
  if ! cp "$dir/gost.json" "$file" 2>/dev/null; then
    echo "❌ 导出失败: 找不到 ${dir}/gost.json"
    return 1
  fi
  chmod 600 "$file"
  echo "✅ 转发配置已导出到: $file"
}

agent_uninstall() {
  local name="$1"
  confirm "确认卸载节点端实例 ${name}（面板 $(agent_addr "$name")）吗？该实例的所有文件将被删除" || { echo "❌ 已取消"; return 1; }
  systemctl stop "$name" 2>/dev/null
  systemctl disable "$name" 2>/dev/null
  rm -f "$SYSTEMD_DIR/${name}.service"
  [[ -n "$name" && -d "$AGENT_ROOT/$name" ]] && rm -rf "$AGENT_ROOT/$name"
  systemctl daemon-reload
  echo "✅ 实例 ${name} 已卸载"
  cleanup_zf
  return 0
}

agent_menu() {
  local name="$1"
  while true; do
    clear 2>/dev/null
    echo "==============================================="
    echo "      节点端 ${name} 快捷管理（zf）"
    echo "==============================================="
    echo "  面板: $(agent_addr "$name")    指纹: $(agent_fp "$name")    状态: $(svc_state "$name")"
    echo "  目录: $AGENT_ROOT/${name}    版本: $("$AGENT_ROOT/${name}/flux_agent" -V 2>/dev/null | head -n1)"
    echo "-----------------------------------------------"
    echo "  1. 更新到最新版（GitHub）"
    echo "  2. 启动"
    echo "  3. 停止运行"
    echo "  4. 重启"
    echo "  5. 查看状态"
    echo "  6. 备份数据库（节点配置）"
    echo "  7. 导出数据库（转发配置 gost.json）"
    echo "  8. 卸载"
    echo "  0. $BACK_LABEL"
    echo "==============================================="
    local c
    read -r -p "请输入选项: " c
    case "$c" in
      1) agent_update "$name"; pause ;;
      2) systemctl start "$name" && echo "✅ 已启动"; pause ;;
      3) systemctl stop "$name" && echo "🛑 已停止运行"; pause ;;
      4) systemctl restart "$name" && echo "✅ 已重启"; pause ;;
      5) svc_show "$name"; pause ;;
      6) agent_backup "$name"; pause ;;
      7) agent_export "$name"; pause ;;
      8) agent_uninstall "$name" && { pause; return 0; }; pause ;;
      0) return 0 ;;
      *) echo "❌ 无效选项"; sleep 1 ;;
    esac
  done
}

# ---------------------------------------------------------------------
# 入口：只有一个服务时直接进入；多个时先列出来选择
# ---------------------------------------------------------------------
main() {
  while true; do
    local targets=() labels=() agents=() n
    if panel_installed; then
      targets+=("panel")
      labels+=("$(printf '%-22s 状态: %-10s 指纹: %-9s 端口: %s' "面板 flux-panel" "$(svc_state "$PANEL_SVC")" "$(panel_fp)" "$(panel_env_get SERVER_PORT)")")
    fi
    mapfile -t agents < <(list_agents)
    for n in "${agents[@]}"; do
      targets+=("$n")
      labels+=("$(printf '%-22s 状态: %-10s 指纹: %-9s 面板: %s' "节点 $n" "$(svc_state "$n")" "$(agent_fp "$n")" "$(agent_addr "$n")")")
    done

    if [[ ${#targets[@]} -eq 0 ]]; then
      echo "ℹ️ 本机没有检测到 flux-panel 面板或 flux_agent 节点端"
      exit 0
    fi

    local pick
    if [[ ${#targets[@]} -eq 1 ]]; then
      pick="${targets[0]}"
      BACK_LABEL="退出"
    else
      BACK_LABEL="返回上一级"
      clear 2>/dev/null
      echo "==============================================="
      echo "   zf 快捷管理：本机检测到 ${#targets[@]} 个 flux 服务"
      echo "==============================================="
      local i
      for i in "${!labels[@]}"; do
        echo "  $((i + 1)). ${labels[$i]}"
      done
      echo "  0. 退出"
      echo "==============================================="
      local c
      read -r -p "请选择要管理的服务: " c
      [[ "$c" == "0" ]] && exit 0
      if ! [[ "$c" =~ ^[0-9]+$ ]] || (( c < 1 || c > ${#targets[@]} )); then
        echo "❌ 无效选项"
        sleep 1
        continue
      fi
      pick="${targets[$((c - 1))]}"
    fi

    if [[ "$pick" == "panel" ]]; then
      panel_menu
    else
      agent_menu "$pick"
    fi
    [[ ${#targets[@]} -eq 1 ]] && exit 0
  done
}

main "$@"
