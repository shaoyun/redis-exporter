package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/context"
	"gopkg.in/yaml.v2"
)

var (
	listenAddress  = flag.String("web.listen-address", ":9121", "Address to listen on for web interface and telemetry")
	configFile     = flag.String("config.file", "config.yaml", "Path to configuration file")
	scrapeInterval = flag.Duration("scrape.interval", 30*time.Second, "Interval between metric collection")
)

type RedisInstance struct {
	Addr     string  `yaml:"addr"`
	Password *string `yaml:"password,omitempty"`
}

type Config struct {
	RedisInstances []RedisInstance `yaml:"redis_instances"`
	ScrapeConfig   ScrapeConfig    `yaml:"scrape_config"`
}

type ScrapeConfig struct {
	Timeout            *time.Duration `yaml:"timeout,omitempty"`
	Pipeline           *bool          `yaml:"pipeline,omitempty"`
	MaxKeys            *int           `yaml:"max_keys_sample,omitempty"`
	CollectMemory      *bool          `yaml:"collect_memory,omitempty"`
	CollectCommands    *bool          `yaml:"collect_commands,omitempty"`
	CollectKeys        *bool          `yaml:"collect_keys,omitempty"`
	CollectClients     *bool          `yaml:"collect_clients,omitempty"`
	MaxRetries         *int           `yaml:"max_retries,omitempty"`
	RetryInterval      *time.Duration `yaml:"retry_interval,omitempty"`
	HealthCheckTimeout *time.Duration `yaml:"health_check_timeout,omitempty"`
}

type RedisExporter struct {
	mutex   sync.Mutex
	clients map[string]*redis.Client
	config  *Config

	// 基础指标
	up *prometheus.GaugeVec

	// 连接指标
	connectedClients *prometheus.GaugeVec

	// 内存指标
	usedMemory      *prometheus.GaugeVec
	maxMemory       *prometheus.GaugeVec
	memoryFragRatio *prometheus.GaugeVec

	// 键值指标
	totalKeys   *prometheus.GaugeVec
	expiredKeys *prometheus.GaugeVec
	evictedKeys *prometheus.GaugeVec

	// 性能指标
	commandsProcessed *prometheus.CounterVec
	keyspaceMisses    *prometheus.CounterVec
	keyspaceHits      *prometheus.CounterVec

	// 持久化指标
	lastSaveTime    *prometheus.GaugeVec
	lastSaveChanges *prometheus.GaugeVec

	lastCheck       time.Time
	lastCheckResult bool
	checkInterval   time.Duration
}

func loadConfig(filename string) (*Config, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Config file %s not found, using empty configuration", filename)
			return &Config{RedisInstances: []RedisInstance{}}, nil
		}
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil, err
	}

	// 如果配置为空，返回空配置而不是错误
	if config.RedisInstances == nil {
		config.RedisInstances = []RedisInstance{}
	}

	return &config, nil
}

func NewRedisExporter(config *Config) (*RedisExporter, *prometheus.Registry) {
	registry := prometheus.NewRegistry()

	exporter := &RedisExporter{
		clients: make(map[string]*redis.Client),
		config:  config,

		up: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "redis_up",
				Help: "Whether the Redis server is up (1) or down (0).",
			},
			[]string{"addr"},
		),

		connectedClients: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "redis_connected_clients",
				Help: "Number of client connections.",
			},
			[]string{"addr"},
		),

		usedMemory: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "redis_memory_used_bytes",
				Help: "Total number of bytes allocated by Redis.",
			},
			[]string{"addr"},
		),

		maxMemory: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "redis_memory_max_bytes",
				Help: "Maximum amount of memory Redis can use.",
			},
			[]string{"addr"},
		),

		memoryFragRatio: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "redis_memory_fragmentation_ratio",
				Help: "Ratio of memory allocated by Redis to memory requested by Redis.",
			},
			[]string{"addr"},
		),

		totalKeys: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "redis_keys_total",
				Help: "Total number of keys.",
			},
			[]string{"addr", "db"},
		),

		expiredKeys: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "redis_expired_keys_total",
				Help: "Total number of expired keys.",
			},
			[]string{"addr"},
		),

		evictedKeys: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "redis_evicted_keys_total",
				Help: "Total number of evicted keys due to maxmemory limit.",
			},
			[]string{"addr"},
		),

		commandsProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "redis_commands_processed_total",
				Help: "Total number of commands processed.",
			},
			[]string{"addr"},
		),

		keyspaceHits: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "redis_keyspace_hits_total",
				Help: "Number of successful lookups of keys in the main dictionary.",
			},
			[]string{"addr"},
		),

		keyspaceMisses: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "redis_keyspace_misses_total",
				Help: "Number of failed lookups of keys in the main dictionary.",
			},
			[]string{"addr"},
		),

		lastSaveTime: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "redis_last_save_time_seconds",
				Help: "Timestamp of last save operation in seconds since the epoch.",
			},
			[]string{"addr"},
		),

		lastSaveChanges: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "redis_last_save_changes_total",
				Help: "Number of changes since last dump.",
			},
			[]string{"addr"},
		),

		checkInterval: 5 * time.Second,
	}

	// 注册所有指标
	registry.MustRegister(
		exporter.up,
		exporter.connectedClients,
		exporter.usedMemory,
		exporter.maxMemory,
		exporter.memoryFragRatio,
		exporter.totalKeys,
		exporter.expiredKeys,
		exporter.evictedKeys,
		exporter.commandsProcessed,
		exporter.keyspaceHits,
		exporter.keyspaceMisses,
		exporter.lastSaveTime,
		exporter.lastSaveChanges,
	)

	// 初始化 Redis 客户端
	for _, instance := range config.RedisInstances {
		options := &redis.Options{
			Addr: instance.Addr,
			DB:   0,
		}

		// 只有在密码不为 nil 时才设置密码
		if instance.Password != nil {
			options.Password = *instance.Password
		}

		client := redis.NewClient(options)
		exporter.clients[instance.Addr] = client
	}

	return exporter, registry
}

func parseFloatOrZero(s string) float64 {
	if val, err := strconv.ParseFloat(s, 64); err == nil {
		return val
	}
	return 0
}

func (c *ScrapeConfig) getTimeout() time.Duration {
	if c.Timeout == nil {
		return 8 * time.Second
	}
	return *c.Timeout
}

func (c *ScrapeConfig) getPipeline() bool {
	if c.Pipeline == nil {
		return true
	}
	return *c.Pipeline
}

func (c *ScrapeConfig) getMaxKeys() int {
	if c.MaxKeys == nil {
		return 1000
	}
	return *c.MaxKeys
}

func (c *ScrapeConfig) getCollectMemory() bool {
	if c.CollectMemory == nil {
		return true
	}
	return *c.CollectMemory
}

func (c *ScrapeConfig) getCollectCommands() bool {
	if c.CollectCommands == nil {
		return true
	}
	return *c.CollectCommands
}

func (c *ScrapeConfig) getCollectKeys() bool {
	if c.CollectKeys == nil {
		return true
	}
	return *c.CollectKeys
}

func (c *ScrapeConfig) getCollectClients() bool {
	if c.CollectClients == nil {
		return true
	}
	return *c.CollectClients
}

func (c *ScrapeConfig) getMaxRetries() int {
	if c.MaxRetries == nil {
		return 2
	}
	return *c.MaxRetries
}

func (c *ScrapeConfig) getRetryInterval() time.Duration {
	if c.RetryInterval == nil {
		return 2 * time.Second
	}
	return *c.RetryInterval
}

func (c *ScrapeConfig) getHealthCheckTimeout() time.Duration {
	if c.HealthCheckTimeout == nil {
		return 3 * time.Second
	}
	return *c.HealthCheckTimeout
}

func (e *RedisExporter) collectMetrics() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// 为每个实例的采集设置独立的超时
	for addr, client := range e.clients {
		// 使用更短的上下文超时，避免单个实例阻塞太久
		ctx, cancel := context.WithTimeout(context.Background(), e.config.ScrapeConfig.getTimeout())
		retries := 0
		for retries < e.config.ScrapeConfig.getMaxRetries() {
			if err := e.collectInstanceMetrics(ctx, addr, client); err != nil {
				retries++
				time.Sleep(e.config.ScrapeConfig.getRetryInterval())
				continue
			}
			break
		}
		cancel() // 及时释放上下文
	}
}

func (e *RedisExporter) collectInstanceMetrics(ctx context.Context, addr string, client *redis.Client) error {
	pipe := client.Pipeline()
	pingCmd := pipe.Ping(ctx)
	infoCmd := pipe.Info(ctx)

	// 执行 pipeline
	_, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}

	// 检查连接状态
	_, err = pingCmd.Result()
	if err != nil {
		return err
	}
	e.up.WithLabelValues(addr).Set(1)

	// 获取 INFO 信息
	info, err := infoCmd.Result()
	if err != nil {
		return err
	}

	// 解析 INFO 命令的输出
	infoMap := parseRedisInfo(info)

	// 更新连接指标
	if e.config.ScrapeConfig.getCollectClients() {
		if v, ok := infoMap["connected_clients"]; ok {
			if val, err := strconv.ParseFloat(v, 64); err == nil {
				e.connectedClients.WithLabelValues(addr).Set(val)
			}
		}
	}

	// 更新内存指标
	if e.config.ScrapeConfig.getCollectMemory() {
		if v, ok := infoMap["used_memory"]; ok {
			e.usedMemory.WithLabelValues(addr).Set(parseFloatOrZero(v))
		}
		if v, ok := infoMap["maxmemory"]; ok {
			if val, err := strconv.ParseFloat(v, 64); err == nil {
				e.maxMemory.WithLabelValues(addr).Set(val)
			}
		}
		if v, ok := infoMap["mem_fragmentation_ratio"]; ok {
			if val, err := strconv.ParseFloat(v, 64); err == nil {
				e.memoryFragRatio.WithLabelValues(addr).Set(val)
			}
		}
	}

	// 更新键值统计
	if e.config.ScrapeConfig.getCollectKeys() {
		if v, ok := infoMap["expired_keys"]; ok {
			if val, err := strconv.ParseFloat(v, 64); err == nil {
				e.expiredKeys.WithLabelValues(addr).Set(val)
			}
		}
		if v, ok := infoMap["evicted_keys"]; ok {
			if val, err := strconv.ParseFloat(v, 64); err == nil {
				e.evictedKeys.WithLabelValues(addr).Set(val)
			}
		}
	}

	// 更新性能指标
	if e.config.ScrapeConfig.getCollectCommands() {
		if v, ok := infoMap["total_commands_processed"]; ok {
			if val, err := strconv.ParseFloat(v, 64); err == nil {
				e.commandsProcessed.WithLabelValues(addr).Add(val)
			}
		}
		if v, ok := infoMap["keyspace_hits"]; ok {
			if val, err := strconv.ParseFloat(v, 64); err == nil {
				e.keyspaceHits.WithLabelValues(addr).Add(val)
			}
		}
		if v, ok := infoMap["keyspace_misses"]; ok {
			if val, err := strconv.ParseFloat(v, 64); err == nil {
				e.keyspaceMisses.WithLabelValues(addr).Add(val)
			}
		}
	}

	// 更新持久化指标
	if v, ok := infoMap["rdb_last_save_time"]; ok {
		if val, err := strconv.ParseFloat(v, 64); err == nil {
			e.lastSaveTime.WithLabelValues(addr).Set(val)
		}
	}
	if v, ok := infoMap["rdb_changes_since_last_save"]; ok {
		if val, err := strconv.ParseFloat(v, 64); err == nil {
			e.lastSaveChanges.WithLabelValues(addr).Set(val)
		}
	}

	// 只扫描 INFO keyspace 中实际存在的数据库
	for k, v := range infoMap {
		if strings.HasPrefix(k, "db") {
			if strings.Contains(v, "keys=") {
				parts := strings.Split(v, ",")
				for _, part := range parts {
					if strings.HasPrefix(part, "keys=") {
						if val, err := strconv.ParseFloat(strings.TrimPrefix(part, "keys="), 64); err == nil {
							e.totalKeys.WithLabelValues(addr, k).Set(val)
						}
					}
				}
			}
		}
	}

	return nil
}

// 解析 Redis INFO 命令的输出
func parseRedisInfo(info string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(info, "\n")

	for _, line := range lines {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	return result
}

func (e *RedisExporter) checkRedisStatus() bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if time.Since(e.lastCheck) < e.checkInterval {
		return e.lastCheckResult
	}

	result := true
	for _, client := range e.clients {
		ctx, cancel := context.WithTimeout(context.Background(), e.config.ScrapeConfig.getHealthCheckTimeout())
		_, err := client.Ping(ctx).Result()
		cancel()

		if err != nil {
			result = false
			break
		}
	}

	e.lastCheck = time.Now()
	e.lastCheckResult = result
	return result
}

func main() {
	flag.Parse()

	// 优先使用环境变量中的配置文件路径
	if envConfig := os.Getenv("CONFIG_FILE"); envConfig != "" {
		*configFile = envConfig
	}

	config, err := loadConfig(*configFile)
	if err != nil {
		log.Printf("Error loading config file %s: %v", *configFile, err)
		log.Printf("Using empty configuration")
		config = &Config{RedisInstances: []RedisInstance{}}
	}

	exporter, registry := NewRedisExporter(config)

	// 创建一个定时器来收集指标
	go func() {
		for {
			exporter.collectMetrics()
			time.Sleep(*scrapeInterval)
		}
	}()

	// 健康检查接口 - 只检查 exporter 服务状态
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// 存活探针接口 - 只检查 exporter 服务状态
	http.HandleFunc("/livez", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// 就绪探针接口 - 检查 Redis 连接状态
	http.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if exporter.checkRedisStatus() {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Service Unavailable"))
		}
	})

	http.Handle("/metrics", promhttp.HandlerFor(
		registry,
		promhttp.HandlerOpts{
			EnableOpenMetrics: false,
		},
	))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>Redis Exporter</title></head>
			<body>
			<h1>Redis Exporter</h1>
			<p><a href="/metrics">Metrics</a></p>
			<p><a href="/health">Health Check</a></p>
			</body>
			</html>`))
	})

	// 添加 HTTP 服务器超时设置
	server := &http.Server{
		Addr:              *listenAddress,
		Handler:           nil, // 使用默认的 DefaultServeMux
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("Starting Redis exporter on %s", *listenAddress)
	log.Fatal(server.ListenAndServe())
}
