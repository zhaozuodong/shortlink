# ---------- Dockerfile ----------
# 基于官方 Go 镜像构建
FROM golang:1.24-alpine AS builder
# 设置工作目录
WORKDIR /app
ENV GOPROXY=https://goproxy.cn,direct
# 拷贝源代码
COPY . .
RUN go mod tidy
# 编译可执行文件
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o shortlink main.go

# -------------------- 生产阶段 --------------------
FROM debian:bookworm-slim
# 设置工作目录
WORKDIR /app
# 复制可执行文件
COPY --from=builder /app/shortlink /app/shortlink
# 设置环境变量默认值
ENV PORT=8080
ENV API_TOKEN=mysecret
ENV SHORT_DOMAIN=http://localhost:8080
# 暴露端口
EXPOSE 8080
# 启动命令
ENTRYPOINT ["/app/shortlink"]