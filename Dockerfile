# 第一阶段：构建
FROM golang:1.21 as builder

# 设置环境变量
ENV GO111MODULE=on \
    GOPROXY=https://goproxy.cn,direct \
    CGO_ENABLED=0 \
    GOOS=linux

# 复制 go.mod 和 go.sum 文件并下载依赖
WORKDIR /build
COPY . .
RUN go mod download

# 复制源代码并构建
RUN go build -tags=jsoniter -ldflags="-w -s" -o ldor

# 第二阶段：运行
FROM alpine:3.18

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件和配置文件
COPY --from=builder /build/ldor /app/ldor
COPY config.json /app

# 设置时区和权限
RUN ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone \
    && chmod 755 /app/ldor

# 设置入口点
ENTRYPOINT ["/app/ldor", "-r", "-p"]