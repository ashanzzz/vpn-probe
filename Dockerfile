# 阶段 0: 构建 React 前端静态资产
FROM node:20-alpine AS web-builder
WORKDIR /web
ARG NPM_REGISTRY
RUN if [ -n "$NPM_REGISTRY" ]; then npm config set registry "$NPM_REGISTRY"; fi
COPY web/package*.json ./
RUN npm install
COPY web/ ./
RUN npm run build

# 阶段 1: 提取官方 sing-box 静态二进制
FROM ghcr.io/sagernet/sing-box:latest AS singbox-bin

# 阶段 2: 构建 Go 二进制
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 注入前端构建产物至 web/dist
COPY --from=web-builder /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/vpn-probe ./cmd/vpn-probe

# 阶段 3: 最小生产运行容器 (非 root 用户运行)
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata curl && \
    addgroup -S app && adduser -S app -G app

WORKDIR /app

# 注入官方 sing-box 二进制与服务程序
COPY --from=singbox-bin /usr/local/bin/sing-box /usr/local/bin/sing-box
COPY --from=builder /app/vpn-probe /app/vpn-probe

# 预置数据目录并赋权非 root 用户
RUN mkdir -p /app/data && chown -R app:app /app

# 验证 sing-box 环境
RUN sing-box version

ENV TZ=Asia/Shanghai
USER app:app
EXPOSE 8080
VOLUME ["/app/data"]

ENTRYPOINT ["/app/vpn-probe"]
CMD ["server", "--config", "/app/config.yaml"]
