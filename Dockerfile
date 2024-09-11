FROM golang:1.21 as builder 

WORKDIR /build
COPY . ./

RUN GOPROXY=https://goproxy.cn,direct go mod download
RUN GO111MODULE=on CGO_ENABLED=0 GOOS=linux go build -tags=jsoniter -ldflags="-w -s" -o ldor

FROM alpine:3.18

WORKDIR /app
COPY --from=builder /build/ldor /app/ldor
COPY config.json /app

RUN ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && echo "Asia/Shanghai" > /etc/timezone

RUN chmod 755 /app/ldor

ENTRYPOINT ["/app/ldor", "-r", "-p"]