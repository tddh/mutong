#!/bin/sh
# 首次启动时从 .example 模板生成配置（如果实际配置不存在）
for f in /app/configs/*.yaml.example; do
  target="${f%.example}"
  if [ ! -f "$target" ]; then
    cp "$f" "$target"
  fi
done

exec "$@"
