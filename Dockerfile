# ---------- Dockerfile ----------
# 基于官方 Go 镜像构建
FROM golang:1.21-alpine AS builder

# 安装 git、bash 等依赖
RUN apk add --no-cache git bash

# 设置工作目录
WORKDIR /app

# 拷贝 go.mod 和 go.sum
COPY go.mod go.sum ./
RUN go mod download

# 拷贝源代码
COPY . .

# 编译可执行文件
RUN go build -o shortlink .

# -------------------- 生产阶段 --------------------
FROM alpine:3.18

# 设置工作目录
WORKDIR /app

# 复制可执行文件
COPY --from=builder /app/shortlink /app/shortlink

# 复制 SQLite 数据库文件（第一次为空即可）
VOLUME ["/app/data"]
ENV DATA_DIR=/app/data

# 设置环境变量默认值
ENV PORT=8080
ENV API_TOKEN=mysecret
ENV SHORT_DOMAIN=http://localhost:8080

# 暴露端口
EXPOSE 8080

# 启动命令
CMD ["/app/shortlink"]