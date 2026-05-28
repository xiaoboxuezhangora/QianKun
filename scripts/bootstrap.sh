#!/usr/bin/env bash
set -eu

QIANKUN_HOME="${QIANKUN_HOME:-"$HOME/.qiankun"}"

mkdir -p \
  "$QIANKUN_HOME/bin" \
  "$QIANKUN_HOME/cache" \
  "$QIANKUN_HOME/db" \
  "$QIANKUN_HOME/logs"

echo "QianKun home ready: $QIANKUN_HOME"
