# 多阶段构建：第一阶段构建应用
FROM golang:1.25-alpine AS builder

# 设置工作目录
WORKDIR /data

# 安装必要的构建工具
RUN apk add --no-cache git gcc musl-dev

# 复制go模块文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 设置构建环境变量
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    LD_FLAGS="-w -s"

# 生成文件（如果项目需要）
RUN go generate ./...

# 构建应用
RUN go build -o /data/zbp_linux_amd64 -trimpath -ldflags "$LD_FLAGS" .

# 第二阶段：运行环境
FROM alpine:latest

# 设置工作目录
WORKDIR /data

# 安装必要的运行时依赖
# RUN apk add --no-cache ca-certificates tzdata curl

# 从构建阶段复制应用
COPY --from=builder /data/zbp_linux_amd64 /app/bin

# 设置执行权限
RUN chmod +x /app/bin

# 暴露端口（根据应用实际需要调整）
# EXPOSE 8080

# 以root用户运行应用
CMD ["/app/bin"]
