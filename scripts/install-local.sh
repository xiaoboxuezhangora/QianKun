#!/usr/bin/env bash
# QianKun (qiankun-mcpd) 离线安装脚本（无需 Go、无需联网）。
# 适用于已 clone 仓库的场景：从仓库内 prebuilt/ 目录取对应平台的预编译二进制并安装。
#
# 用法（在仓库根目录或任意位置）：
#   bash scripts/install-local.sh
#
# 可选环境变量：
#   QIANKUN_INSTALL_DIR   安装目录（默认 /usr/local/bin 可写时用之，否则 ~/.local/bin）
set -euo pipefail

BIN="qiankun-mcpd"
# 定位仓库内 prebuilt 目录（相对脚本位置）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PREBUILT="${SCRIPT_DIR}/../prebuilt"

info() { printf '\033[0;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[0;33m警告:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[0;31m错误:\033[0m %s\n' "$*" >&2; exit 1; }

[ -d "$PREBUILT" ] || die "未找到 prebuilt 目录：$PREBUILT（确认已完整 clone 仓库）"

# 1. 识别平台
os_raw="$(uname -s)"; arch_raw="$(uname -m)"
case "$os_raw" in
  Darwin) os="darwin" ;; Linux) os="linux" ;;
  *) die "暂不支持的操作系统：$os_raw（Windows 请直接用 prebuilt 下的 .exe）" ;;
esac
case "$arch_raw" in
  arm64|aarch64) arch="arm64" ;; x86_64|amd64) arch="amd64" ;;
  *) die "暂不支持的架构：$arch_raw" ;;
esac
info "检测到平台：${os}-${arch}"

# 2. 匹配预编译产物（文件名形如 qiankun-mcpd-<version>-<os>-<arch>）
shopt -s nullglob
matches=("${PREBUILT}/${BIN}-"*"-${os}-${arch}")
[ ${#matches[@]} -gt 0 ] || die "prebuilt 中没有 ${os}-${arch} 的二进制，请联系维护者补全或改用源码安装"
src="${matches[0]}"
info "使用产物：$(basename "$src")"

# 3. 校验和（若有 checksums.txt）
if [ -f "${PREBUILT}/checksums.txt" ]; then
  name="$(basename "$src")"
  expected="$(grep " ${name}\$" "${PREBUILT}/checksums.txt" | awk '{print $1}')"
  if [ -n "$expected" ]; then
    if command -v shasum >/dev/null 2>&1; then
      actual="$(shasum -a 256 "$src" | awk '{print $1}')"
    elif command -v sha256sum >/dev/null 2>&1; then
      actual="$(sha256sum "$src" | awk '{print $1}')"
    else actual=""; warn "无 shasum/sha256sum，跳过校验"; fi
    [ -n "$actual" ] && [ "$actual" != "$expected" ] && die "校验和不匹配！期望 $expected 实得 $actual"
    [ -n "$actual" ] && info "校验通过"
  fi
fi

# 4. 选择安装目录
dir="${QIANKUN_INSTALL_DIR:-}"; sudo=""
if [ -z "$dir" ]; then
  if [ -w "/usr/local/bin" ]; then dir="/usr/local/bin"
  elif command -v sudo >/dev/null 2>&1 && [ -d "/usr/local/bin" ]; then dir="/usr/local/bin"; sudo="sudo"; warn "将使用 sudo 写入 ${dir}"
  else dir="${HOME}/.local/bin"; fi
fi
mkdir -p "$dir" 2>/dev/null || $sudo mkdir -p "$dir"

# 5. 安装
tmp="$(mktemp)"; cp "$src" "$tmp"; chmod +x "$tmp"
$sudo mv "$tmp" "${dir}/${BIN}"
[ "$os" = "darwin" ] && xattr -d com.apple.quarantine "${dir}/${BIN}" 2>/dev/null || true
info "已安装到 ${dir}/${BIN}"

case ":${PATH}:" in
  *":${dir}:"*) ;;
  *) warn "${dir} 不在 PATH 中，请加入：echo 'export PATH=\$PATH:${dir}' >> ~/.zshrc && source ~/.zshrc" ;;
esac

if "${dir}/${BIN}" --version >/dev/null 2>&1; then
  info "安装成功：$(${dir}/${BIN} --version)"
else
  warn "已安装但无法执行 --version，请检查权限或平台匹配"
fi
