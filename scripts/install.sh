#!/usr/bin/env bash
# QianKun (qiankun-mcpd) 一键安装脚本。
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/xiaoboxuezhangora/QianKun/main/scripts/install.sh | bash
# 可选环境变量：
#   QIANKUN_VERSION       指定版本 tag（默认安装最新 release），如 v0.4.0-w4
#   QIANKUN_INSTALL_DIR   安装目录（默认 /usr/local/bin 可写时用之，否则 ~/.local/bin）
set -euo pipefail

REPO="xiaoboxuezhangora/QianKun"
BIN="qiankun-mcpd"

info()  { printf '\033[0;32m==>\033[0m %s\n' "$*"; }
warn()  { printf '\033[0;33m警告:\033[0m %s\n' "$*" >&2; }
die()   { printf '\033[0;31m错误:\033[0m %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "缺少依赖命令：$1"; }
need uname
need curl

# 1. 识别平台
os_raw="$(uname -s)"
arch_raw="$(uname -m)"
case "$os_raw" in
  Darwin) os="darwin" ;;
  Linux)  os="linux"  ;;
  *) die "暂不支持的操作系统：$os_raw（Windows 请手动从 Releases 下载 .exe）" ;;
esac
case "$arch_raw" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64)  arch="amd64" ;;
  *) die "暂不支持的架构：$arch_raw" ;;
esac
info "检测到平台：${os}-${arch}"

# 2. 解析版本
tag="${QIANKUN_VERSION:-}"
if [ -z "$tag" ]; then
  info "查询最新 release..."
  tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name" *: *"([^"]+)".*/\1/')"
  [ -n "$tag" ] || die "无法获取最新版本，请用 QIANKUN_VERSION 指定，如 v0.4.0-w4"
fi
version="${tag#v}"   # 去掉前导 v 以匹配产物文件名
asset="${BIN}-${version}-${os}-${arch}"
info "目标版本：${tag}（产物 ${asset}）"

base="https://github.com/${REPO}/releases/download/${tag}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# 3. 下载二进制与校验和
info "下载二进制..."
curl -fSL --progress-bar "${base}/${asset}" -o "${tmp}/${asset}" \
  || die "下载失败：${base}/${asset}"

if curl -fsSL "${base}/checksums.txt" -o "${tmp}/checksums.txt" 2>/dev/null; then
  info "校验 SHA-256..."
  expected="$(grep " ${asset}\$" "${tmp}/checksums.txt" | awk '{print $1}')"
  if [ -n "$expected" ]; then
    if command -v shasum >/dev/null 2>&1; then
      actual="$(shasum -a 256 "${tmp}/${asset}" | awk '{print $1}')"
    elif command -v sha256sum >/dev/null 2>&1; then
      actual="$(sha256sum "${tmp}/${asset}" | awk '{print $1}')"
    else
      actual=""; warn "无 shasum/sha256sum，跳过校验"
    fi
    if [ -n "$actual" ] && [ "$actual" != "$expected" ]; then
      die "校验和不匹配！期望 $expected 实得 $actual"
    fi
    [ -n "$actual" ] && info "校验通过"
  else
    warn "checksums.txt 未包含 ${asset}，跳过校验"
  fi
else
  warn "未找到 checksums.txt，跳过校验"
fi

# 4. 选择安装目录
dir="${QIANKUN_INSTALL_DIR:-}"
sudo=""
if [ -z "$dir" ]; then
  if [ -w "/usr/local/bin" ] || ([ -d "/usr/local/bin" ] && touch /usr/local/bin/.qk_w 2>/dev/null && rm -f /usr/local/bin/.qk_w); then
    dir="/usr/local/bin"
  elif command -v sudo >/dev/null 2>&1 && [ -d "/usr/local/bin" ]; then
    dir="/usr/local/bin"; sudo="sudo"; warn "将使用 sudo 写入 ${dir}"
  else
    dir="${HOME}/.local/bin"
  fi
fi
mkdir -p "$dir" 2>/dev/null || $sudo mkdir -p "$dir"

# 5. 安装
chmod +x "${tmp}/${asset}"
$sudo mv "${tmp}/${asset}" "${dir}/${BIN}"
# macOS 解除 Gatekeeper 隔离（二进制未签名）
[ "$os" = "darwin" ] && xattr -d com.apple.quarantine "${dir}/${BIN}" 2>/dev/null || true
info "已安装到 ${dir}/${BIN}"

# 6. PATH 提示与验证
case ":${PATH}:" in
  *":${dir}:"*) ;;
  *) warn "${dir} 不在 PATH 中，请加入后再用，例如："
     printf '      echo '\''export PATH=$PATH:%s'\'' >> ~/.zshrc && source ~/.zshrc\n' "$dir" ;;
esac

if "${dir}/${BIN}" --version >/dev/null 2>&1; then
  info "安装成功：$(${dir}/${BIN} --version)"
else
  warn "已安装但无法执行 --version，请检查权限或平台匹配"
fi
