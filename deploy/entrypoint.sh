#!/bin/sh
# 首次启动时从 .example 模板生成配置（如果实际配置不存在）。
# 如果 /app/configs 不可写（例如被 root 属主的命名卷覆盖），
# 回退到可写目录，避免以非 root 用户启动时崩溃。
set -e

CONFIG_DIR="${MUTONG_CONFIG_DIR:-/app/configs}"
mkdir -p "$CONFIG_DIR" 2>/dev/null || true

if [ ! -w "$CONFIG_DIR" ] || ! ls "$CONFIG_DIR"/config.*.yaml >/dev/null 2>&1; then
  FALLBACK="${MUTONG_CONFIG_DIR_FALLBACK:-/tmp/mutong-configs}"
  mkdir -p "$FALLBACK"
  for f in /app/configs/config.*.yaml.example; do
    [ -f "$f" ] || continue
    base=$(basename "$f")
    target="$FALLBACK/${base%.example}"
    [ -f "$target" ] || cp "$f" "$target"
  done
  CONFIG_DIR="$FALLBACK"
fi

if [ "$#" -eq 0 ]; then
  set -- /app/mutong
fi

exec "$@" -c "$CONFIG_DIR"