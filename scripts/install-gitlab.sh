#!/usr/bin/env bash
# QianKun (qiankun-mcpd) 内网 GitLab 一键安装脚本（无需 Go 环境）。
# 从内网 GitLab Generic Package Registry 下载预编译二进制并安装。
#
# 用法（需先准备一个有 read_api / read_package_registry 权限的 token）：
#   export GITLAB_TOKEN=<token>
#   curl -fsSL --header "PRIVATE-TOKEN: $GITLAB_TOKEN" \
#     "http://oscoe.neusoft.com:8000/api/v4/projects/b.w_neu%2Fqiankun/repository/files/scripts%2Finstall-gitlab.sh/raw?ref=main" \
#     | bash
#
# 可选环境变量：
#   GITLAB_TOKEN          访问 token（必填，PRIVATE-TOKEN）
#   QIANKUN_VERSION       指定版本（默认查询 package registry 最新），如 0.4.0-w4
#   QIANKUN_INSTALL_DIR   安装目录（默认 /usr/local/bin 可写时用之，否则 ~/.local/bin）
#   QIANKUN_GITLAB_HOST   GitLab 地址（默认 http://oscoe.neusoft.com:8000）
#   QIANKUN_GITLAB_PROJ   URL 编码的项目路径（默认 b.w_neu%2Fqiankun）
set -euo pipefail

BIN="qiankun-mcpd"
PKG="qiankun-mcpd"   # generic package 名
HOST="${QIANKUN_GITLAB_HOST:-http://oscoe.neusoft.com:8000}"
PROJ="${QIANKUN_GITLAB_PROJ:-b.w_neu%2Fqiankun}"

info() { printf '\033[0;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[0;33m警告:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[0;31m错误:\033[0m %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "缺少依赖命令：$1"; }
need uname; need curl

[ -n "${GITLAB_TOKEN:-}" ] || die "请先设置 GITLAB_TOKEN（需 read_package_registry/read_api 权限）"

API="${HOST}/api/v4/projects/${PROJ}"
CURL=(curl -fSL --connect-timeout 20 --max-time 600 --retry 2 --retry-delay 1 --header "PRIVATE-TOKEN: ${GITLAB_TOKEN}")
CURLS=(curl -fsSL --connect-timeout 15 --retry 2 --header "PRIVATE-TOKEN: ${GITLAB_TOKEN}")

# 1. 识别平台
os_raw="$(uname -s)"; arch_raw="$(uname -m)"
case "$os_raw" in
  Darwin) os="darwin" ;; Linux) os="linux" ;;
  *) die "暂不支持的操作系统：$os_raw（Windows 请从 GitLab Package 手动下载 .exe）" ;;
esac
case "$arch_raw" in
  arm64|aarch64) arch="arm64" ;; x86_64|amd64) arch="amd64" ;;
  *) die "暂不支持的架构：$arch_raw" ;;
esac
info "检测到平台：${os}-${arch}"

# 2. 解析版本（默认取 package registry 最新）
version="${QIANKUN_VERSION:-}"
if [ -z "$version" ]; then
  info "查询最新版本..."
  version="$("${CURLS[@]}" "${API}/packages?package_type=generic&package_name=${PKG}&order_by=created_at&sort=desc&per_page=1" 2>/dev/null \
    | grep -m1 '"version"' | sed -E 's/.*"version" *: *"([^"]+)".*/\1/')"
  [ -n "$version" ] || die "无法查询版本，请用 QIANKUN_VERSION 指定，如 0.4.0-w4"
fi
asset="${BIN}-${version}-${os}-${arch}"
[ "$os" = windows ] && asset="${asset}.exe"
base="${API}/packages/generic/${PKG}/${version}"
info "目标版本：${version}（产物 ${asset}）"

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

# 3. 下载二进制与校验和
info "下载二进制..."
"${CURL[@]}" --progress-bar "${base}/${asset}" -o "${tmp}/${asset}" \
  || die "下载失败：${base}/${asset}（检查 token 权限与版本是否存在）"

if "${CURLS[@]}" "${base}/checksums.txt" -o "${tmp}/checksums.txt" 2>/dev/null; then
  expected="$(grep " ${asset}\$" "${tmp}/checksums.txt" | awk '{print $1}')"
  if [ -n "$expected" ]; then
    if command -v shasum >/dev/null 2>&1; then
      actual="$(shasum -a 256 "${tmp}/${asset}" | awk '{print $1}')"
    elif command -v sha256sum >/dev/null 2>&1; then
      actual="$(sha256sum "${tmp}/${asset}" | awk '{print $1}')"
    else actual=""; warn "无 shasum/sha256sum，跳过校验"; fi
    [ -n "$actual" ] && [ "$actual" != "$expected" ] && die "校验和不匹配！期望 $expected 实得 $actual"
    [ -n "$actual" ] && info "校验通过"
  else warn "checksums.txt 未包含 ${asset}，跳过校验"; fi
else warn "未找到 checksums.txt，跳过校验"; fi

# 4. 选择安装目录
dir="${QIANKUN_INSTALL_DIR:-}"; sudo=""
if [ -z "$dir" ]; then
  if [ -w "/usr/local/bin" ]; then dir="/usr/local/bin"
  elif command -v sudo >/dev/null 2>&1 && [ -d "/usr/local/bin" ]; then dir="/usr/local/bin"; sudo="sudo"; warn "将使用 sudo 写入 ${dir}"
  else dir="${HOME}/.local/bin"; fi
fi
mkdir -p "$dir" 2>/dev/null || $sudo mkdir -p "$dir"

# 5. 安装
chmod +x "${tmp}/${asset}"
$sudo mv "${tmp}/${asset}" "${dir}/${BIN}"
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
