#!/bin/sh
# Generate secrets locally; never replace an existing environment file.
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
env_file=${1:-.env}
if [ -e "$env_file" ] || [ -L "$env_file" ]; then
  printf '%s\n' "环境文件已存在，未修改：${env_file}" "请确认管理密码已设置，再运行 docker compose up -d --build。"
  exit 0
fi
umask 077
admin_password=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
if [ "${#admin_password}" -ne 48 ]; then
  printf '%s\n' '无法生成随机管理密码，未创建环境文件。' >&2
  exit 1
fi
(
  set -C
  sed "s/^PORTPILOT_ADMIN_PASSWORD=$/PORTPILOT_ADMIN_PASSWORD=$admin_password/" .env.example > "$env_file"
)
printf '%s\n' "已生成私有环境文件：${env_file}（权限 600）" \
  '管理密码位于其中的 PORTPILOT_ADMIN_PASSWORD；代理密码在首次启动时自动生成。' \
  '下一步：docker compose up -d --build' \
  '登录面板后，在运行配置中设置入口 IP，在访问认证中查看代理账号密码。'
