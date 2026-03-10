FROM golang:1.25 AS builder

WORKDIR /src

RUN apt-get update && \
    apt-get install -y --no-install-recommends git && \
    rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN GOPROXY=https://goproxy.cn,direct go mod download

COPY . .

# Match the repository workflow: regenerate derived files before building.
RUN go generate ./... && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-w -s" -o /out/zerobot-plugin .

FROM alpine:3.22

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && \
    apk add --no-cache ca-certificates tzdata && \
    adduser -D -h /app -s /bin/sh appuser

ENV TZ=Asia/Shanghai

WORKDIR /app

COPY --from=builder /out/zerobot-plugin /app/zerobot-plugin
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN chmod +x /app/zerobot-plugin /usr/local/bin/docker-entrypoint.sh && \
    mkdir -p /app/data && \
    chown -R appuser:appuser /app

USER appuser

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD []
