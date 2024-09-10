FROM golang:1.21 as builder 

ENV GO111MODULE=on GOPROXY=https://goproxy.cn,direct
RUN go mod download
    
RUN  GO111MODULE=on CGO_ENABLED=0 GOOS=linux go build -tags=jsoniter -ldflags="-w -s" -o or


FROM alpine:3.18

WORKDIR /app
COPY --from=builder /build/cmd/revisions-cleaner /app/revisions-cleaner

RUN ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && echo "Asia/Shanghai" > /etc/timezone

RUN chmod 755 /app/or

ENTRYPOINT ["/app/or"]