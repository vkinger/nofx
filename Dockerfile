# ═══════════════════════════════════════════════════════════════
# NOFX 前后端一体镜像 Dockerfile
# 从源码构建：前端 (Node/Vite) + 后端 (Go + TA-Lib)，运行时 Nginx + 后端进程
#
# 运行需挂载后端配置与数据目录，推荐使用：
#   docker compose -f docker-compose.one.yml up -d --build
# 首次请先准备 .env：cp .env.example .env 并按需修改。
# 若直接 docker run，请加：--env-file .env -v $(pwd)/data:/app/data
# ═══════════════════════════════════════════════════════════════

ARG NODE_VERSION=20-alpine
ARG GO_VERSION=1.25-alpine
ARG ALPINE_VERSION=latest
ARG TA_LIB_VERSION=0.4.0

# ──────────────────────────────────────────────────────────────
# 1) TA-Lib 构建阶段
# ──────────────────────────────────────────────────────────────
FROM alpine:${ALPINE_VERSION} AS ta-lib-builder
ARG TA_LIB_VERSION
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk update && apk add --no-cache \
    wget tar make gcc g++ musl-dev autoconf automake
RUN wget https://downloads.sourceforge.net/ta-lib/ta-lib-${TA_LIB_VERSION}-src.tar.gz && \
    tar -xzf ta-lib-${TA_LIB_VERSION}-src.tar.gz && \
    cd ta-lib && \
    if [ "$(uname -m)" = "aarch64" ]; then \
        CONFIG_GUESS=$(find /usr/share -name config.guess | head -1) && \
        CONFIG_SUB=$(find /usr/share -name config.sub | head -1) && \
        cp "$CONFIG_GUESS" config.guess && cp "$CONFIG_SUB" config.sub && chmod +x config.guess config.sub; \
    fi && \
    ./configure --prefix=/usr/local && make && make install && \
    cd .. && rm -rf ta-lib ta-lib-${TA_LIB_VERSION}-src.tar.gz

# ──────────────────────────────────────────────────────────────
# 2) 后端构建阶段 (Go + TA-Lib)
# ──────────────────────────────────────────────────────────────
FROM golang:${GO_VERSION} AS backend-builder
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk update && apk add --no-cache git make gcc g++ musl-dev

COPY --from=ta-lib-builder /usr/local /usr/local

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux \
    CGO_CFLAGS="-D_LARGEFILE64_SOURCE" \
    go build -trimpath -ldflags="-s -w" -o nofx .

# ──────────────────────────────────────────────────────────────
# 3) 前端构建阶段 (Node/Vite)
# ──────────────────────────────────────────────────────────────
FROM node:${NODE_VERSION} AS frontend-builder
WORKDIR /build

COPY web/package*.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

# ──────────────────────────────────────────────────────────────
# 4) 最终运行阶段：Alpine + Nginx + 后端二进制 + 前端静态
# ──────────────────────────────────────────────────────────────
FROM alpine:${ALPINE_VERSION}

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk update && apk add --no-cache \
    ca-certificates tzdata nginx openssl

# TA-Lib 库（后端依赖）
COPY --from=ta-lib-builder /usr/local/lib/libta_lib* /usr/local/lib/
RUN ldconfig /usr/local/lib 2>/dev/null || true

# 后端二进制
WORKDIR /app
COPY --from=backend-builder /app/nofx /app/nofx
COPY .env /app/.env
RUN mkdir -p /app/data

# 前端静态资源
COPY --from=frontend-builder /build/dist /usr/share/nginx/html

# 构建时生成自签名 SSL 证书并打包进镜像
RUN mkdir -p /app/ssl && \
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout /app/ssl/key.pem -out /app/ssl/cert.pem \
        -subj "/CN=localhost/O=NOFX/C=US"

# 启动脚本：生成 nginx 配置（使用镜像内证书）、启动后端(8081)、启动 nginx(PORT)
COPY railway/start.sh /app/start.sh
RUN chmod +x /app/start.sh

ENV DB_PATH=/app/data/data.db
ENV PORT=80

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider --no-check-certificate "https://localhost:${PORT:-80}/health" || exit 1

CMD ["/app/start.sh"]
