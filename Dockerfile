FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o redis-exporter

FROM alpine:3.18

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/redis-exporter /usr/local/bin/
COPY --from=builder /app/config.default.yaml /etc/redis-exporter/config.yaml

# 创建启动脚本
RUN echo '#!/bin/sh' > /usr/local/bin/docker-entrypoint.sh && \
    echo 'exec /usr/local/bin/redis-exporter --config.file=${CONFIG_FILE:-/etc/redis-exporter/config.yaml}' >> /usr/local/bin/docker-entrypoint.sh && \
    chmod +x /usr/local/bin/docker-entrypoint.sh

EXPOSE 9121

# 设置默认配置文件路径
ENV CONFIG_FILE=/etc/redis-exporter/config.yaml

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]