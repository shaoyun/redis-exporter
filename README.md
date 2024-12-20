# Redis Exporter

一个用于收集 Redis 指标的 Prometheus exporter。支持监控多个 Redis 实例，提供丰富的监控指标。

## 功能特点

- 支持监控多个 Redis 实例
- 支持有密码和无密码的 Redis 实例
- 通过 YAML 配置文件管理 Redis 连接信息
- 支持空配置和配置文件不存在的情况
- 提供丰富的监控指标
- 提供服务健康检查接口
- 支持 Kubernetes 探针
- 低资源占用

## 快速开始

### 使用 Docker

1. 使用默认配置运行（不监控任何 Redis 实例）：
```bash
docker run -p 9121:9121 redis-exporter
```

2. 使用自定义配置运行：
```bash
# 创建配置文件 config.yaml
redis_instances:
  # 带密码的 Redis 实例
  - addr: "redis1:6379"
    password: "password1"
  
  # 无密码的 Redis 实例
  - addr: "redis2:6379"

# 运行容器
docker run -p 9121:9121 -v $(pwd)/config.yaml:/etc/redis-exporter/config.yaml redis-exporter
```

3. 使用环境变量指定配置文件路径：
```bash
docker run -p 9121:9121 \
  -v $(pwd)/my-config.yaml:/config/redis.yaml \
  -e CONFIG_FILE=/config/redis.yaml \
  redis-exporter
```

### 配置说明

配置文件使用 YAML 格式，支持以下几种情况：

1. 不提供配置文件：使用内置的空配置
2. 空配置文件：
```yaml
redis_instances: []
```

3. 标准配置：
```yaml
redis_instances:
  # 带密码的实例
  - addr: "redis1:6379"      # Redis 实例地址
    password: "password1"     # Redis 密码（可选）
  
  # 无密码的实例
  - addr: "redis2:6379"      # 无密码时省略 password 字段
```

## API 接口

### 指标接口
- `/metrics`: Prometheus 指标接口，包含所有 Redis 实例的监控指标

### 健康检查接口
- `/health`: 健康检查接口
  - 返回 200: 表示 exporter 服务正常运行
  - 用于监控 exporter 服务本身的状态

### Kubernetes 探针
- `/livez`: 存活探针
  - 返回 200: 表示 exporter 服务正常运行
  - 用于检查 exporter 服务进程状态
- `/readyz`: 就绪探针
  - 返回 200: 所有 Redis 实例可连接
  - 返回 503: 存在不可连接的 Redis 实例
  - 用于检查 exporter 是否可以正常采集指标

## Kubernetes 部署示例

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis-exporter
spec:
  selector:
    matchLabels:
      app: redis-exporter
  template:
    metadata:
      labels:
        app: redis-exporter
    spec:
      containers:
      - name: redis-exporter
        image: redis-exporter:latest
        ports:
        - containerPort: 9121
        livenessProbe:
          httpGet:
            path: /livez
            port: 9121
          initialDelaySeconds: 5
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /readyz
            port: 9121
          initialDelaySeconds: 5
          periodSeconds: 10
        volumeMounts:
        - name: config
          mountPath: /etc/redis-exporter/config.yaml
          subPath: config.yaml
      volumes:
      - name: config
        configMap:
          name: redis-exporter-config
```

## 可用指标

### 基础指标
- `redis_up`: Redis 实例的可用性 (0 表示不可用，1 表示可用)

### 连接指标
- `redis_connected_clients`: 当前连接的客户端数量

### 内存指标
- `redis_memory_used_bytes`: Redis 已使用的内存字节数
- `redis_memory_max_bytes`: Redis 最大可用内存字节数
- `redis_memory_fragmentation_ratio`: 内存碎片率

### 键值指标
- `redis_keys_total`: 每个数据库的键总数
- `redis_expired_keys_total`: 过期的键总数
- `redis_evicted_keys_total`: 由于内存限制被驱逐的键总数

### 性能指标
- `redis_commands_processed_total`: 处理的命令总数
- `redis_keyspace_hits_total`: 键查找命中次数
- `redis_keyspace_misses_total`: 键查找未命中次数

### 持久化指标
- `redis_last_save_time_seconds`: 最后一次 RDB 保存的时间戳
- `redis_last_save_changes_total`: 自上次 RDB 保存以来的更改数

## Prometheus 配置示例

```yaml
scrape_configs:
  - job_name: 'redis'
    static_configs:
      - targets: ['localhost:9121']
```

## Grafana 面板示例

### 内存使用率
```
redis_memory_used_bytes / redis_memory_max_bytes * 100
```

### 键命中率
```
rate(redis_keyspace_hits_total[5m]) / (rate(redis_keyspace_hits_total[5m]) + rate(redis_keyspace_misses_total[5m])) * 100
```

### 秒命令数
```
rate(redis_commands_processed_total[1m])
```

## 构建

```bash
# 构建二进制
go build -o redis-exporter

# 构建 Docker 镜像
docker build -t redis-exporter .

# 构建多架构镜像
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t shaoyun/redis-exporter:1.0 \
  -t shaoyun/redis-exporter:latest \
  --push .
```

## 许可证

MIT License 