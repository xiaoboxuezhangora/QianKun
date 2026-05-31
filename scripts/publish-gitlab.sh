#!/usr/bin/env bash
# 把 dist/ 下的预编译二进制 + checksums.txt 上传到内网 GitLab Generic Package Registry。
# 需要一个有 api / write_package_registry 权限的 token。
#
# 用法：
#   make build 之后先交叉编译产物到 dist/（见 README），再：
#   GITLAB_TOKEN=<token> VERSION=0.4.0-w4 bash scripts/publish-gitlab.sh
#
# 环境变量：
#   GITLAB_TOKEN          必填，PRIVATE-TOKEN（api / write_package_registry）
#   VERSION               包版本（默认 0.4.0-w4）
#   QIANKUN_GITLAB_HOST   GitLab 地址（默认 http://oscoe.neusoft.com:8000）
#   QIANKUN_GITLAB_PROJ   URL 编码项目路径（默认 b.w_neu%2Fqiankun）
#   DIST                  产物目录（默认 dist）
set -euo pipefail

PKG="qiankun-mcpd"
VERSION="${VERSION:-0.4.0-w4}"
HOST="${QIANKUN_GITLAB_HOST:-http://oscoe.neusoft.com:8000}"
PROJ="${QIANKUN_GITLAB_PROJ:-b.w_neu%2Fqiankun}"
DIST="${DIST:-dist}"

info() { printf '\033[0;32m==>\033[0m %s\n' "$*"; }
die()  { printf '\033[0;31m错误:\033[0m %s\n' "$*" >&2; exit 1; }

[ -n "${GITLAB_TOKEN:-}" ] || die "请设置 GITLAB_TOKEN（需 api/write_package_registry 权限）"
[ -d "$DIST" ] || die "未找到产物目录 $DIST，请先交叉编译"

base="${HOST}/api/v4/projects/${PROJ}/packages/generic/${PKG}/${VERSION}"
info "上传到 ${base}"

shopt -s nullglob
files=("$DIST"/${PKG}-* "$DIST"/checksums.txt)
[ ${#files[@]} -gt 0 ] || die "$DIST 下没有可上传的产物"

for f in "${files[@]}"; do
  name="$(basename "$f")"
  printf '  上传 %s ... ' "$name"
  code=$(curl -sS --connect-timeout 20 --max-time 600 \
    --header "PRIVATE-TOKEN: ${GITLAB_TOKEN}" \
    --upload-file "$f" \
    -o /dev/null -w '%{http_code}' \
    "${base}/${name}")
  case "$code" in
    20*|create*) echo "OK ($code)" ;;
    *) echo "失败 ($code)"; die "上传 $name 失败，HTTP $code（检查 token 权限）" ;;
  esac
done
info "全部上传完成。安装版本：$VERSION"
