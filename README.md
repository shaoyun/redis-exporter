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

### 命令行参数

```bash
Usage of redis_exporter:
  -web.listen-address string
        监听地址和端口 (默认 ":9121")
  -config.file string
        配置文件路径 (默认 "config.yaml")
  -scrape.interval duration
        指标采集间隔 (默认 30s)
```

### 配置文件说明

配置文件使用 YAML 格式，支持以下配置项：

1. Redis 实例配置（必需）：
```yaml
redis_instances:
  - addr: "redis1:6379"      # Redis 实例地址
    password: "password1"     # Redis 密码（可选）
  - addr: "redis2:6379"      # 无密码时省略 password 字段
```

2. 采集配置（可选，均有默认值）：
```yaml
scrape_config:
  # 超时设置
  timeout: 5s              # 指标采集超时，默认 3s
  health_check_timeout: 2s # 健康检查超时，默认 2s
  retry_interval: 1s       # 重试间隔，默认 1s
  max_retries: 2          # 最大重试次数，默认 3
  
  # 采集开关（默认全部为 true）
  collect_memory: true     # 是否采集内存指标
  collect_commands: true   # 是否采集命令统计
  collect_keys: true       # 是否采集键统计
  collect_clients: true    # 是否采集客户端连接数
  
  # 其他设置
  pipeline: true          # 是否使用管道，默认 true
  max_keys_sample: 1000   # 键采样数量限制，默认 1000
```

最简配置示例：
```yaml
redis_instances:
  - addr: "redis:6379"
```

推荐配置示例：
```yaml
redis_instances:
  - addr: "redis1:6379"
    password: "password1"
  - addr: "redis2:6379"

scrape_config:
  timeout: 5s              # 采集超时设置为 5s
  health_check_timeout: 2s # 健康检查超时设置为 2s
  retry_interval: 1s       # 重试间隔设置为 1s
  max_retries: 2          # 最大重试次数设置为 2次
```

### 超时配置说明

1. **指标采集超时 (timeout)**
   - 默认值：8秒
   - 推荐值：
     - 本地环境：3-5秒
     - 远程环境：8-10秒
   - 说明：单个 Redis 实例的指标采集超时时间
   - 建议：根据网络状况和 Redis 实例的响应时间适当调整

2. **健康检查超时 (health_check_timeout)**
   - 默认值：3秒
   - 推荐值：
     - 本地环境：1-2秒
     - 远程环境：3-5秒
   - 说明：健康检查和就绪检查的超时时间
   - 建议：保持较短以快速响应健康状态变化，但要考虑网络延迟

3. **重试设置**
   - 重试间隔 (retry_interval)：
     - 默认值：2秒
     - 本地环境：1秒
     - 远程环境：2-3秒
   - 最大重试次数 (max_retries)：
     - 默认值：2次
     - 本地环境：1次
     - 远程环境：2-3次
   - 说明：采集失败时的重试策略
   - 建议：网络不稳定时适当增加重试间隔，避免频繁重试

### 超时优化建议

1. **本地开发环境**
   ```yaml
   scrape_config:
     timeout: 3s
     health_check_timeout: 1s
     retry_interval: 1s
     max_retries: 1
   ```

2. **远程生产环境（默认配置）**
   ```yaml
   scrape_config:
     timeout: 8s
     health_check_timeout: 3s
     retry_interval: 2s
     max_retries: 2
   ```

3. **网络极不稳定环境**
   ```yaml
   scrape_config:
     timeout: 10s
     health_check_timeout: 5s
     retry_interval: 3s
     max_retries: 3
   ```

## 性能优化建议

1. **采集间隔**
   - 本地环境：15-30秒
   - 远程环境：30-60秒
   - 网络不稳定：60-120秒
   - 通过 `-scrape.interval` 参数设置

2. **超时设置**
   - 本地环境：timeout=3s, health_check_timeout=1s
   - 远程环境：timeout=8s, health_check_timeout=3s（默认值）
   - 不稳定环境：适当增加超时时间和重试次数

3. **指标选择**
   - 只采集必要的指标
   - 高负载实例建议关闭 collect_commands
   - 大规模集群建议关闭 collect_keys
   - 网络不稳定时建议只采集关键指标

4. **资源占用**
   - 合理设置采集间隔，远程环境建议 30s 以上
   - 避免过多的重试次数，默认 2 次通常足够
   - 及时清理断开的连接
   - 使用 pipeline 减少网络往返

5. **网络优化**
   - 确保 exporter 和 Redis 实例在同一网络区域
   - 考虑使用内网连接
   - 避免跨地域采集
   - 必要时可以部署多个 exporter 就近采集

## 许可证

MIT License 

## Prometheus 配置

### 静态配置
```yaml
scrape_configs:
  - job_name: 'redis'
    static_configs:
      - targets: ['redis-exporter:9121']
    metrics_path: '/metrics'
    relabel_configs:
      - source_labels: [__address__]
        target_label: instance
      - source_labels: [addr]
        target_label: redis_instance
```

### Kubernetes 服务发现
```yaml
scrape_configs:
  - job_name: 'redis'
    kubernetes_sd_configs:
      - role: service
    relabel_configs:
      - source_labels: [__meta_kubernetes_service_label_app]
        regex: redis-exporter
        action: keep
      - source_labels: [__meta_kubernetes_service_name,__meta_kubernetes_namespace]
        action: replace
        target_label: instance
        regex: (.+);(.+)
        replacement: $1.$2.svc
```

完整的 Kubernetes 部署配置请参考 [k8s-deploy.yaml](k8s-deploy.yaml)。