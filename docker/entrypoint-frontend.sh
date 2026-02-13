#!/bin/sh
# NOFX Frontend Entrypoint
# 启动前将 nginx.conf 中的 __NGINX_RESOLVER__ 替换为容器内部 DNS（/etc/resolv.conf 的 nameserver）
# 兼容 Docker / Podman 环境

set -e

NGINX_CONF="${NGINX_CONF:-/etc/nginx/conf.d/default.conf}"

# 从 /etc/resolv.conf 提取 nameserver（取第一个）
RESOLVER="127.0.0.11"
if [ -r /etc/resolv.conf ]; then
    _ns=$(grep -m1 '^nameserver' /etc/resolv.conf 2>/dev/null | awk '{print $2}')
    [ -n "$_ns" ] && RESOLVER="$_ns"
fi

# 替换 nginx 配置中的占位符
sed -i "s/__NGINX_RESOLVER__/$RESOLVER/g" "$NGINX_CONF"

# 执行传入的命令，若无则启动 nginx（nginx:alpine 默认 CMD）
if [ $# -gt 0 ]; then
    exec "$@"
else
    exec nginx -g 'daemon off;'
fi
