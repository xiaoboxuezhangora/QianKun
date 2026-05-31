#!/usr/bin/env bash
# QianKun (qiankun-mcpd) 卸载脚本。
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/xiaoboxuezhangora/QianKun/main/scripts/uninstall.sh | bash
#   或本地：bash scripts/uninstall.sh [--purge]
# 选项：
#   --purge               同时删除数据目录（${QIANKUN_HOME:-~/.qiankun}，含 DB/缓存/日志）
# 环境变量：
#   QIANKUN_INSTALL_DIR   仅在该目录查找二进制（默认搜索常见目录 + PATH）
#   QIANKUN_HOME          数据目录（默认 ~/.qiankun）
set -euo pipefail

BIN="qiankun-mcpd"
purge=0
for a in "$@"; do
  case "$a" in
    --purge) purge=1 ;;
    *) printf '未知参数：%s\n' "$a" >&2; exit 2 ;;
  esac
done

info()  { printf '\033[0;32m==>\033[0m %s\n' "$*"; }
warn()  { printf '\033[0;33m警告:\033[0m %s\n' "$*" >&2; }

# 1. 收集候选二进制路径（去重）
candidates=()
[ -n "${QIANKUN_INSTALL_DIR:-}" ] && candidates+=("${QIANKUN_INSTALL_DIR}/${BIN}")
candidates+=("/usr/local/bin/${BIN}" "${HOME}/.local/bin/${BIN}")
if command -v "$BIN" >/dev/null 2>&1; then
  candidates+=("$(command -v "$BIN")")
fi

removed=0
seen=""
for p in "${candidates[@]}"; do
  case ":$seen:" in *":$p:"*) continue ;; esac
  seen="$seen:$p"
  [ -e "$p" ] || continue
  if [ -w "$(dirname "$p")" ]; then
    rm -f "$p" && { info "已删除 $p"; removed=1; }
  elif command -v sudo >/dev/null 2>&1; then
    warn "需要 sudo 删除 $p"
    sudo rm -f "$p" && { info "已删除 $p"; removed=1; }
  else
    warn "无权限删除 $p，请手动 rm"
  fi
done

[ "$removed" -eq 0 ] && warn "未找到已安装的 ${BIN} 二进制"

# 2. 可选清理数据目录
home="${QIANKUN_HOME:-$HOME/.qiankun}"
if [ "$purge" -eq 1 ]; then
  if [ -d "$home" ]; then
    rm -rf "$home" && info "已删除数据目录 $home"
  else
    warn "数据目录不存在：$home"
  fi
else
  [ -d "$home" ] && warn "保留数据目录 $home（含 DB/缓存/日志）；如需一并删除请加 --purge"
fi

info "卸载完成"
