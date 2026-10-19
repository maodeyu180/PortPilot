#!/bin/sh
# Prepare private deployment settings; never replace an existing file.
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
env_file=${1:-.env}
if [ -e "$env_file" ] || [ -L "$env_file" ]; then
  printf '%s\n' "环境文件已存在，未修改：${env_file}" "管理密码留空时，首次访问面板即可设置。"
  exit 0
fi
umask 077
(
  set -C
  cat .env.example > "$env_file"
)
printf '%s\n' "已生成私有环境文件：${env_file}（权限 600）" \
  '管理密码默认留空，首次打开面板时设置；代理密码会独立随机生成。' \
  '下一步：docker compose up -d --build' \
  '启动后打开面板，按网页提示设置并确认管理密码。'
