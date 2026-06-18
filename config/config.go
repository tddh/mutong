package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/allegro/bigcache/v3"
	"github.com/spf13/viper"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	nebula "github.com/vesoft-inc/nebula-go/v3"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	eino_openai "github.com/cloudwego/eino-ext/components/model/openai"

	"gitee.com/tddh/mutong/interfaces"
	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	"gitee.com/tddh/mutong/models"
	alert_model "gitee.com/tddh/mutong/models/alert"
	diagnosis_model "gitee.com/tddh/mutong/models/diagnosis"
	insp_model "gitee.com/tddh/mutong/models/inspection"
	oauth2model "gitee.com/tddh/mutong/models/oauth2"
	retrospective_model "gitee.com/tddh/mutong/models/retrospective"
	alert_service "gitee.com/tddh/mutong/services/alert"
	diagnosis_svc "gitee.com/tddh/mutong/services/diagnosis"
	insp_service "gitee.com/tddh/mutong/services/inspection"
	insp_rules "gitee.com/tddh/mutong/services/inspection/rules"
)

// NewConfig loads configuration from the given path (file or directory),
// initialises the logger, database, BigCache, Nebula session pool, Kafka
// clients, and Kubernetes cluster connections, and then validates required
// fields.  This is the production entry point.  For lightweight loading
// in tests or tooling, use LoadConfig instead.
func NewConfig(path string) *Config {
	c := Config{}
	c.LoadConfig(path)
	c.validate()
	return &c
}

func (c *Config) validate() {
	var missing []string
	if c.Nebula.Host == "" {
		missing = append(missing, "nebula.host")
	}
	if c.Postgres.Host == "" {
		c.Logger.Warn("PostgreSQL host not configured, user/role/audit features will be unavailable")
	}
	if c.Kafka.Broker == "" {
		c.Logger.Warn("Kafka broker not configured, resource collection via message queue disabled")
	}
	if len(missing) > 0 {
		c.Logger.Error("Missing required configuration", zap.Strings("fields", missing))
	}
}

// LocalRotLogger: lightweight fallback file writer (no rotation)
// Used when lumberjack/v2 dependency is unavailable in the build environment.
type LocalRotLogger struct {
	Filename string
	MaxSize  int
	MaxAge   int
	Compress bool
}

func (l *LocalRotLogger) Write(p []byte) (n int, err error) {
	f, err := os.OpenFile(l.Filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return f.Write(p)
}

func (l *LocalRotLogger) Sync() error { return nil }
func (c *Config) applyDefaults() {
	if c.Auth.Mode == "" {
		c.Auth.Mode = "local"
	}
	if len(c.Auth.Zitadel.Scopes) == 0 {
		c.Auth.Zitadel.Scopes = []string{"openid", "profile", "email"}
	}
}

// LoadConfig reads a single YAML config file and returns a Config with
// auth defaults applied.  This is a lightweight loader for tests and
// tooling — it does NOT initialise the logger, database, cache, Nebula
// session pool, or any other production infrastructure.  Production
// callers should use NewConfig instead.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}
	c := &Config{}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	c.applyDefaults()
	return c, nil
}

func (c *Config) LoadConfig(path string) {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		c.loadConfigDir(path)
	} else {
		c.loadConfigFile(path)
	}
	c.SetDefaultConfig()
}

// loadConfigFile loads a single yaml config file (backward compatible path).
func (c *Config) loadConfigFile(path string) {
	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")

	if err := viper.ReadInConfig(); err != nil {
		panic(fmt.Errorf("failed to load config: %w", err))
	}

	if err := viper.Unmarshal(c); err != nil {
		panic(fmt.Errorf("failed to unmarshal config: %w", err))
	}
}

// loadConfigDir loads all config.*.yaml files from a directory, merging them
// into the Config struct. Each file must have non-overlapping top-level yaml keys.
func (c *Config) loadConfigDir(dir string) {
	pattern := filepath.Join(dir, "config.*.yaml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		panic(fmt.Errorf("failed to scan config directory %s: %w", dir, err))
	}
	if len(files) == 0 {
		panic(fmt.Errorf("no config.*.yaml files found in %s", dir))
	}

	// Sort for deterministic loading order (alphabetical).
	sort.Strings(files)

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			panic(fmt.Errorf("failed to read config %s: %w", f, err))
		}
		if err := yaml.Unmarshal(data, c); err != nil {
			panic(fmt.Errorf("failed to parse config %s: %w", f, err))
		}
	}
}

func (c *Config) GetConfigClusterClient(name string) *K8sClusterClient {
	return c.Cluster.K8sClusterClient[name]
}

func (c *Config) GetConfigBigCache() *bigcache.BigCache {
	return c.BigCache
}

func (c *Config) GetConfigGormDB() *gorm.DB {
	return c.DB
}

func (c *Config) SetDefaultConfig() {
	c.initLogger()
	c.Logger.Info("SetDefaultConfig start")

	c.loadResourceProfiles()

	c.SetupBigCache()

	// Allow env vars to override sensitive config values
	if dbUser := os.Getenv("MUTONG_DB_USER"); dbUser != "" {
		c.Postgres.User = dbUser
	}
	if dbPass := os.Getenv("MUTONG_DB_PASSWORD"); dbPass != "" {
		c.Postgres.Pass = dbPass
	}
	if nebulaUser := os.Getenv("MUTONG_NEBULA_USER"); nebulaUser != "" {
		c.Nebula.User = nebulaUser
	}
	if nebulaPass := os.Getenv("MUTONG_NEBULA_PASS"); nebulaPass != "" {
		c.Nebula.Pass = nebulaPass
	}

	// External search API keys (override YAML config)
	if tavilyKey := os.Getenv("MUTONG_TAVILY_KEY"); tavilyKey != "" {
		c.ExternalSearch.Tavily.APIKey = tavilyKey
	}
	if githubToken := os.Getenv("MUTONG_GITHUB_TOKEN"); githubToken != "" {
		c.ExternalSearch.GitHub.Token = githubToken
	}

	c.SetupClusterClient()
	if c.Postgres.User == "" || c.Postgres.Pass == "" {
		c.Logger.Warn("PostgreSQL credentials not configured (set MUTONG_DB_USER / MUTONG_DB_PASSWORD env vars), " +
			"alert persistence / audit log / diagnosis archive will use in-memory fallback")
	} else if err := c.SetupDB(); err != nil {
		c.Logger.Warn("PostgreSQL initialization failed, falling back to in-memory storage", zap.Error(err))
	}
	c.SetupNubelaGraph()
	c.SetupKafkaClient()
	c.SetupTraceKafkaClient()
	c.SetupBusinessWorkloadClient()

	c.setServerDefaults()
	c.setKafkaInternalDefaults()
	c.setExecutorInternalDefaults()
	c.applyDefaults()
}

func (c *Config) initLogger() {
	level, err := zapcore.ParseLevel(c.Log.Level)
	if err != nil {
		panic(err)
	}
	// Base production config
	config := zap.NewProductionConfig()
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// Determine encoder format
	encoder := func() zapcore.Encoder {
		if strings.ToLower(strings.TrimSpace(c.Log.Format)) == "console" {
			return zapcore.NewConsoleEncoder(config.EncoderConfig)
		}
		// default to json
		return zapcore.NewJSONEncoder(config.EncoderConfig)
	}()

	// Build write targets based on Output
	var cores []zapcore.Core
	// Ensure stdout is always available as a possible target if requested
	if strings.Contains(strings.ToLower(c.Log.Output), "stdout") || c.Log.Output == "" {
		cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level))
	}
	if strings.Contains(strings.ToLower(c.Log.Output), "file") {
		// Try real lumberjack-based rotating logger if enabled via build tag
		if w, err := newRotatingLogger(c.Log.Path+"/mutong.log", c.Log.MaxSize, c.Log.MaxAge); err == nil && w != nil {
			cores = append(cores, zapcore.NewCore(encoder, w, level))
		} else {
			// Fallback to local rotation mock
			lr := LocalRotLogger{
				Filename: c.Log.Path + "/mutong.log",
				MaxSize:  c.Log.MaxSize,
				MaxAge:   c.Log.MaxAge,
				Compress: true,
			}
			cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(&lr), level))
		}
	}

	// If both stdout and file are configured, tee will merge cores
	var finalCore zapcore.Core
	if len(cores) == 0 {
		// Fallback to stdout if nothing specified
		finalCore = zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level)
	} else if len(cores) == 1 {
		finalCore = cores[0]
	} else {
		finalCore = zapcore.NewTee(cores...)
	}

	// Build logger with caller info and attach service field
	zlog := zap.New(finalCore, zap.AddCaller(), zap.AddCallerSkip(1))
	zlog = zlog.With(zap.String("service", "mutong"))
	c.Logger = zlog
}

func (c *Config) CustomCallerField(skip int) zap.Field {
	pc, _, line, ok := runtime.Caller(skip)
	callerName := "unknown"
	callerLine := "unknown"
	if ok {
		callerName = runtime.FuncForPC(pc).Name()
		callerLine = strconv.Itoa(line)
	}
	return zap.String("parentCaller", callerName+":"+callerLine)
}

func (c *Config) CustomFieldWithTraceID(traceID string) zap.Field {
	return zap.String("X-Trace-ID", traceID)
}

func (c *Config) SetupClusterClient() {
	c.Logger.Debug("SetupClusterClient start ", c.CustomCallerField(2))

	c.Cluster = &Cluster{}
	c.Cluster.K8sClusterClient = make(map[string]*K8sClusterClient)

	// 新：多集群声明式模式（优先）
	if len(c.Kubernetes.Clusters) > 0 {
		c.setupClusterClientsFromConfig()
		return
	}

	// 旧：单 kubeconfig 文件模式（向后兼容）
	if c.Kubernetes.Config != "" {
		c.setupClusterClientsFromFile(c.Kubernetes.Config)
		return
	}

	// 无 kubeconfig 配置 → 跳过
	c.Logger.Warn("no kubeconfig configured (kubernetes.config or kubernetes.clusters), " +
		"K8s cluster features will be unavailable")
}

// setupClusterClientsFromConfig 从配置文件的 clusters 列表初始化（新）
func (c *Config) setupClusterClientsFromConfig() {
	for _, entry := range c.Kubernetes.Clusters {
		c.setupClusterClientFromFile(entry.Name, entry.Config)
		// 第一个成功连接的集群作为默认 context
		if c.Cluster.Context == "" {
			if _, ok := c.Cluster.K8sClusterClient[entry.Name]; ok {
				c.Cluster.Context = entry.Name
			}
		}
	}
}

// setupClusterClientsFromFile 从单个 kubeconfig 文件初始化（旧）
func (c *Config) setupClusterClientsFromFile(configPath string) {
	config, err := os.ReadFile(configPath)
	if err != nil {
		c.Logger.Error("read kube config failed", zap.String("path", configPath), zap.Error(err))
		os.Exit(1)
	}

	clientConfig, err := clientcmd.NewClientConfigFromBytes(config)
	if err != nil {
		c.Logger.Error("create root cluster struct failed", zap.Error(err))
		os.Exit(1)
	}

	RawConfig, err := clientConfig.RawConfig()
	if err != nil {
		c.Logger.Error("error loading kube config, please ensure kubectl get namespaces works", zap.Error(err))
		os.Exit(1)
	}

	RawConfig.CurrentContext = RawConfig.Contexts[RawConfig.CurrentContext].Cluster
	c.Cluster.Context = RawConfig.CurrentContext

	for name, cluster := range RawConfig.Clusters {
		var ctxName, userName string
		for cn, ctx := range RawConfig.Contexts {
			if ctx.Cluster == name {
				ctxName = cn
				userName = ctx.AuthInfo
				break
			}
		}

		Config := api.Config{
			Kind:        "Config",
			APIVersion:  "v1",
			Preferences: api.Preferences{},
			Clusters:    make(map[string]*api.Cluster),
			AuthInfos:   make(map[string]*api.AuthInfo),
			Contexts:    make(map[string]*api.Context),
		}
		Config.Clusters[name] = cluster
		if userName != "" && RawConfig.AuthInfos[userName] != nil {
			Config.AuthInfos[userName] = RawConfig.AuthInfos[userName]
		}
		if ctxName != "" && RawConfig.Contexts[ctxName] != nil {
			Config.Contexts[ctxName] = RawConfig.Contexts[ctxName]
		}
		Config.CurrentContext = ctxName

		ConfigBytes, e := clientcmd.Write(Config)
		if e != nil {
			c.Logger.Error("write kube config failed", zap.Error(e))
			continue
		}
		c.createClusterClient(name, ConfigBytes, &RawConfig)
	}
}

// setupClusterClientFromFile 从单个集群文件初始化（新：每个文件一个集群）
func (c *Config) setupClusterClientFromFile(name, configPath string) {
	config, err := os.ReadFile(configPath)
	if err != nil {
		c.Logger.Error("read kube config failed", zap.String("name", name), zap.String("path", configPath), zap.Error(err))
		return
	}

	clientConfig, err := clientcmd.NewClientConfigFromBytes(config)
	if err != nil {
		c.Logger.Error("create ClientConfig failed", zap.String("name", name), zap.Error(err))
		return
	}

	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		c.Logger.Error("parse kube config failed", zap.String("name", name), zap.Error(err))
		return
	}

	c.createClusterClient(name, config, &rawConfig)
}

// createClusterClient 创建单个集群的 K8s 客户端和 Informer
func (c *Config) createClusterClient(name string, configBytes []byte, rawConfig *api.Config) {
	clientConfig, err := clientcmd.NewClientConfigFromBytes(configBytes)
	if err != nil {
		c.Logger.Error("create ClientConfig failed", zap.String("name", name), zap.Error(err))
		return
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		c.Logger.Error("load RestConfig failed", zap.String("name", name), zap.Error(err))
		return
	}
	restConfig.Burst = 200
	restConfig.QPS = 100
	restConfig.Timeout = 30 * time.Second

	kubeClientSet, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		c.Logger.Error("create KubeClientSet failed", zap.String("name", name), zap.Error(err))
		return
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		c.Logger.Error("create discovery client failed", zap.String("name", name), zap.Error(err))
		return
	}

	resyncInterval := time.Duration(c.Cache.ResyncInterval) * time.Minute
	if resyncInterval <= 0 {
		resyncInterval = 30 * time.Minute
	}

	factory := informers.NewSharedInformerFactoryWithOptions(kubeClientSet, resyncInterval, informers.WithNamespace(corev1.NamespaceAll))
	client := K8sClusterClient{
		Name:                name,
		RootRawConfig:       rawConfig,
		RootRestConfig:      restConfig,
		RootKubeClientSet:   kubeClientSet,
		RootDiscoveryClient: discoveryClient,
		RootClientConfig:    &clientConfig,
		Factory:             &factory,
	}

	// 验证连接
	_, gErr := client.RootKubeClientSet.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{})
	if gErr != nil {
		c.Logger.Error("cluster connectivity check failed", zap.String("name", name), zap.Error(gErr))
	} else {
		c.Logger.Info("cluster connected", zap.String("name", name))
	}

	c.Cluster.K8sClusterClient[name] = &client
}

func (c *Config) SetupDB() error {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai", c.Postgres.Host, c.Postgres.User, c.Postgres.Pass, c.Postgres.Database, c.Postgres.Port)
	d, err := gorm.Open(
		postgres.Open(dsn), &gorm.Config{
			DisableForeignKeyConstraintWhenMigrating: true,
			Logger:                                   NewGormZapLogger(c.Logger, logger.Info, 1*time.Second),
			AllowGlobalUpdate:                        false,
		},
	)
	if err != nil {
		c.Logger.Error("open postgres failed", c.CustomCallerField(2), zap.Error(err))
		return fmt.Errorf("failed to open postgres: %w", err)
	}

	sqlDB, e := d.DB()
	if e != nil {
		c.Logger.Error("sql.db failed", c.CustomCallerField(2), zap.Error(e))
		return fmt.Errorf("failed to get sql.DB: %w", e)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Minute * 30)
	c.DB = d

	// AutoMigrate for diagnosis tables
	if err := c.migrateDiagnosisTables(); err != nil {
		c.Logger.Warn("diagnosis tables migration failed", c.CustomCallerField(2), zap.Error(err))
	}

	// AutoMigrate for auth tables
	if err := c.migrateAuthTables(); err != nil {
		c.Logger.Warn("auth tables migration failed", c.CustomCallerField(2), zap.Error(err))
	}

	return nil
}

func (c *Config) migrateDiagnosisTables() error {
	if c.DB == nil {
		return fmt.Errorf("DB not initialized")
	}
	return c.DB.AutoMigrate(
		&diagnosis_model.ChatSessionModel{},
		&diagnosis_model.ChatMessageModel{},
		&diagnosis_model.DiagnosisContextModel{},
		&diagnosis_model.DiagnosisResultModel{},
		&diagnosis_model.StatsModel{},
		&diagnosis_model.FaultReportVector{},
		&alert_model.StatsModel{},
		&retrospective_model.PostmortemModel{},
		&retrospective_model.DecisionRecord{},
		&retrospective_model.IncidentKnowledge{},
		&retrospective_model.AutoRetrospectiveTask{},
		&insp_model.InspectionReportModel{},
		&insp_model.InspectionRuleModel{},
	)
}

func (c *Config) migrateAuthTables() error {
	if c.DB == nil {
		return fmt.Errorf("DB not initialized")
	}
	return c.DB.AutoMigrate(
		&models.User{},
		&models.APIToken{},
		&models.ServiceAccount{},
		&models.Role{},
		&oauth2model.OAuth2Client{},
		&oauth2model.AuthorizationCode{},
		&oauth2model.Grant{},
		&oauth2model.DeviceCode{},
	)
}

func (c *Config) SetupNubelaGraph() /*Nebula client*/ {
	config, err1 := nebula.NewSessionPoolConf(
		c.Nebula.User,
		c.Nebula.Pass,
		[]nebula.HostAddress{
			{
				Host: c.Nebula.Host,
				Port: c.Nebula.Port,
			},
		},
		c.Nebula.Space,
		// 增加超时时间，解决复杂查询时的I/O超时问题
		nebula.WithTimeOut(30000*time.Millisecond), // 30秒超时
		// 延长空闲时间，减少频繁建连（30s → 60s）
		nebula.WithIdleTime(60000*time.Millisecond), // 60秒空闲
		// 增加最大连接数，提高并发处理能力（50 → 150）
		nebula.WithMaxSize(150),
		// 保持最小连接数（5 → 15）
		nebula.WithMinSize(15),
	)

	if err1 != nil {
		c.Logger.Error("create session pool failed,", c.CustomCallerField(2), zap.Error(err1))
		os.Exit(1)
	}
	c.Logger.Debug("config: ", c.CustomCallerField(2), zap.Any("config", *config))

	sessionPool, err := nebula.NewSessionPool(*config, nebula.DefaultLogger{})
	if err != nil {
		c.Logger.Error("create session pool failed,", c.CustomCallerField(2), zap.Error(err))
		os.Exit(1)
	}

	c.Nebula.SessionPool = sessionPool
}

func (c *Config) SetupKafkaClient() {
	brokers := strings.Split(c.Kafka.Broker, ",")
	if len(brokers) == 0 {
		c.Logger.Error("Invalid Kafka broker configuration", c.CustomCallerField(2))
		os.Exit(1)
	}

	maxRecordBytes := c.Kafka.MaxRecordBytes
	if maxRecordBytes == 0 {
		maxRecordBytes = 52428800 // 50MB, must align with broker message.max.bytes
	}

	maxBrokerWriteBytes := c.Kafka.MaxBrokerWriteBytes
	if maxBrokerWriteBytes == 0 {
		maxBrokerWriteBytes = 104857600 // 100MB
	}

	fetchMaxBytes := c.Kafka.FetchMaxBytes
	if fetchMaxBytes == 0 {
		fetchMaxBytes = 52428800 // 50MB, must align with broker message.max.bytes
	}

	fetchMaxPartBytes := c.Kafka.FetchMaxPartBytes
	if fetchMaxPartBytes == 0 {
		fetchMaxPartBytes = 52428800 // 50MB
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.AllowAutoTopicCreation(),
		kgo.RecordDeliveryTimeout(60 * time.Second),
		kgo.AllowAutoTopicCreation(),
		kgo.ProducerBatchCompression(kgo.GzipCompression()),
		kgo.ProducerBatchMaxBytes(maxRecordBytes),
		kgo.BrokerMaxWriteBytes(maxBrokerWriteBytes),
		kgo.FetchMaxBytes(fetchMaxBytes),
		kgo.FetchMaxPartitionBytes(fetchMaxPartBytes),
		kgo.FetchMaxWait(5 * time.Second),
		kgo.AutoCommitMarks(),
		kgo.AutoCommitInterval(10 * time.Second),
		kgo.AutoCommitCallback(func(cl *kgo.Client, req *kmsg.OffsetCommitRequest, resp *kmsg.OffsetCommitResponse, err error) {
			if err != nil {
				c.Logger.Error("[KAFKA] Offset commit error", zap.Error(err))
				return
			}
			if req == nil || resp == nil {
				return
			}
			for _, topic := range req.Topics {
				for _, partition := range topic.Partitions {
					c.Logger.Debug(
						"[KAFKA] Offset committed",
						zap.String("group", req.Group),
						zap.String("commit_topic", topic.Topic),
						zap.Int32("partition", partition.Partition),
						zap.Int64("offset", partition.Offset),
					)
				}
			}
		}),
		kgo.ConsumerGroup(c.Kafka.Group),
		kgo.ConsumeTopics(c.Kafka.Topic),
		kgo.ConsumePreferringLagFn(kgo.PreferLagAt(1)),
		kgo.OnPartitionsAssigned(func(ctx context.Context, cl *kgo.Client, assigned map[string][]int32) {
			c.Logger.Debug("[KAFKA] Partitions assigned",
				zap.String("group", c.Kafka.Group),
				zap.Any("assignments", assigned))
		}),
		kgo.OnPartitionsRevoked(func(ctx context.Context, cl *kgo.Client, revoked map[string][]int32) {
			c.Logger.Debug("[KAFKA] Partitions revoked",
				zap.String("group", c.Kafka.Group),
				zap.Any("assignments", revoked))
		}),
		kgo.OnPartitionsLost(func(ctx context.Context, cl *kgo.Client, lost map[string][]int32) {
			c.Logger.Warn("[KAFKA] Partitions lost",
				zap.String("group", c.Kafka.Group),
				zap.Any("assignments", lost))
		}),
	}

	var err error
	c.Kafka.Client, err = kgo.NewClient(opts...)
	if err != nil {
		c.Logger.Error("create kafka client failed,", c.CustomCallerField(2), zap.Error(err))
		os.Exit(1)

	}

	c.Logger.Info("create kafka client success,", c.CustomCallerField(2),
		zap.String("topic", c.Kafka.Topic),
		zap.String("group", c.Kafka.Group),
		zap.String("broker", c.Kafka.Broker),
		zap.Int32("maxRecordBytes", maxRecordBytes),
		zap.Int32("maxBrokerWriteBytes", maxBrokerWriteBytes),
		zap.Int32("fetchMaxBytes", fetchMaxBytes),
		zap.Int32("fetchMaxPartBytes", fetchMaxPartBytes))
}

func (c *Config) SetupTraceKafkaClient() {
	if c.Kafka.TraceTopic == "" || c.Kafka.TraceGroup == "" {
		c.Logger.Info("Trace Kafka not configured, skipping",
			zap.String("traceTopic", c.Kafka.TraceTopic),
			zap.String("traceGroup", c.Kafka.TraceGroup))
		return
	}

	brokers := strings.Split(c.Kafka.Broker, ",")
	if len(brokers) == 0 {
		c.Logger.Error("Invalid Kafka broker configuration", c.CustomCallerField(2))
		os.Exit(1)
	}

	maxRecordBytes := c.Kafka.MaxRecordBytes
	if maxRecordBytes == 0 {
		maxRecordBytes = 52428800
	}

	maxBrokerWriteBytes := c.Kafka.MaxBrokerWriteBytes
	if maxBrokerWriteBytes == 0 {
		maxBrokerWriteBytes = 104857600
	}

	fetchMaxBytes := c.Kafka.FetchMaxBytes
	if fetchMaxBytes == 0 {
		fetchMaxBytes = 52428800
	}

	fetchMaxPartBytes := c.Kafka.FetchMaxPartBytes
	if fetchMaxPartBytes == 0 {
		fetchMaxPartBytes = 52428800
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.AllowAutoTopicCreation(),
		kgo.RecordDeliveryTimeout(60 * time.Second),
		kgo.ProducerBatchCompression(kgo.GzipCompression()),
		kgo.ProducerBatchMaxBytes(maxRecordBytes),
		kgo.BrokerMaxWriteBytes(maxBrokerWriteBytes),
		kgo.FetchMaxBytes(fetchMaxBytes),
		kgo.FetchMaxPartitionBytes(fetchMaxPartBytes),
		kgo.FetchMaxWait(5 * time.Second),
		kgo.AutoCommitMarks(),
		kgo.AutoCommitInterval(10 * time.Second),
		kgo.AutoCommitCallback(func(cl *kgo.Client, req *kmsg.OffsetCommitRequest, resp *kmsg.OffsetCommitResponse, err error) {
			if err != nil {
				c.Logger.Error("[TRACE_KAFKA] Offset commit error", zap.Error(err))
				return
			}
			if req == nil || resp == nil {
				return
			}
			for _, topic := range req.Topics {
				for _, partition := range topic.Partitions {
					c.Logger.Debug(
						"[TRACE_KAFKA] Offset committed",
						zap.String("group", req.Group),
						zap.String("commit_topic", topic.Topic),
						zap.Int32("partition", partition.Partition),
						zap.Int64("offset", partition.Offset),
					)
				}
			}
		}),
		kgo.ConsumerGroup(c.Kafka.TraceGroup),
		kgo.ConsumeTopics(c.Kafka.TraceTopic),
		kgo.ConsumePreferringLagFn(kgo.PreferLagAt(1)),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
		kgo.OnPartitionsAssigned(func(ctx context.Context, cl *kgo.Client, assigned map[string][]int32) {
			c.Logger.Info("[TRACE_KAFKA] Partitions assigned",
				zap.String("group", c.Kafka.TraceGroup),
				zap.Any("assignments", assigned))
		}),
		kgo.OnPartitionsRevoked(func(ctx context.Context, cl *kgo.Client, revoked map[string][]int32) {
			c.Logger.Info("[TRACE_KAFKA] Partitions revoked",
				zap.String("group", c.Kafka.TraceGroup),
				zap.Any("assignments", revoked))
		}),
		kgo.OnPartitionsLost(func(ctx context.Context, cl *kgo.Client, lost map[string][]int32) {
			c.Logger.Warn("[TRACE_KAFKA] Partitions lost",
				zap.String("group", c.Kafka.TraceGroup),
				zap.Any("assignments", lost))
		}),
	}

	var err error
	c.Kafka.TraceClient, err = kgo.NewClient(opts...)
	if err != nil {
		c.Logger.Error("create trace kafka client failed,", c.CustomCallerField(2), zap.Error(err))
		os.Exit(1)
	}

	c.Logger.Info("create trace kafka client success,", c.CustomCallerField(2),
		zap.String("topic", c.Kafka.TraceTopic),
		zap.String("group", c.Kafka.TraceGroup),
		zap.String("broker", c.Kafka.Broker))
}

func (c *Config) SetupBusinessWorkloadClient() {
	if c.Kafka.BusinessWorkloadTopic == "" || c.Kafka.BusinessWorkloadGroup == "" {
		c.Logger.Info("Business Workload Kafka not configured, skipping",
			zap.String("topic", c.Kafka.BusinessWorkloadTopic),
			zap.String("group", c.Kafka.BusinessWorkloadGroup))
		return
	}

	brokers := strings.Split(c.Kafka.Broker, ",")
	if len(brokers) == 0 {
		c.Logger.Error("Invalid Kafka broker configuration", c.CustomCallerField(2))
		os.Exit(1)
	}

	maxRecordBytes := c.Kafka.MaxRecordBytes
	if maxRecordBytes == 0 {
		maxRecordBytes = 52428800
	}

	maxBrokerWriteBytes := c.Kafka.MaxBrokerWriteBytes
	if maxBrokerWriteBytes == 0 {
		maxBrokerWriteBytes = 104857600
	}

	fetchMaxBytes := c.Kafka.FetchMaxBytes
	if fetchMaxBytes == 0 {
		fetchMaxBytes = 52428800
	}

	fetchMaxPartBytes := c.Kafka.FetchMaxPartBytes
	if fetchMaxPartBytes == 0 {
		fetchMaxPartBytes = 52428800
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.AllowAutoTopicCreation(),
		kgo.RecordDeliveryTimeout(60 * time.Second),
		kgo.ProducerBatchCompression(kgo.GzipCompression()),
		kgo.ProducerBatchMaxBytes(maxRecordBytes),
		kgo.BrokerMaxWriteBytes(maxBrokerWriteBytes),
		kgo.FetchMaxBytes(fetchMaxBytes),
		kgo.FetchMaxPartitionBytes(fetchMaxPartBytes),
		kgo.AutoCommitMarks(),
		kgo.AutoCommitInterval(5 * time.Second),
		kgo.AutoCommitCallback(func(cl *kgo.Client, req *kmsg.OffsetCommitRequest, resp *kmsg.OffsetCommitResponse, err error) {
			if err != nil {
				c.Logger.Error("[BLS_KAFKA] Auto-commit failed", zap.Error(err))
			}
		}),
		kgo.ConsumerGroup(c.Kafka.BusinessWorkloadGroup),
		kgo.ConsumeTopics(c.Kafka.BusinessWorkloadTopic),
		kgo.ConsumePreferringLagFn(kgo.PreferLagAt(1)),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
		kgo.OnPartitionsAssigned(func(ctx context.Context, cl *kgo.Client, assigned map[string][]int32) {
			c.Logger.Info("[BLS_KAFKA] Partitions assigned",
				zap.String("group", c.Kafka.BusinessWorkloadGroup),
				zap.Any("assignments", assigned))
		}),
		kgo.OnPartitionsRevoked(func(ctx context.Context, cl *kgo.Client, revoked map[string][]int32) {
			c.Logger.Info("[BLS_KAFKA] Partitions revoked, committing offsets",
				zap.String("group", c.Kafka.BusinessWorkloadGroup),
				zap.Any("assignments", revoked))
			if err := cl.CommitMarkedOffsets(ctx); err != nil {
				c.Logger.Error("[BLS_KAFKA] Commit on revoke failed", zap.Error(err))
			}
		}),
		kgo.OnPartitionsLost(func(ctx context.Context, cl *kgo.Client, lost map[string][]int32) {
			c.Logger.Warn("[BLS_KAFKA] Partitions lost, committing offsets",
				zap.String("group", c.Kafka.BusinessWorkloadGroup),
				zap.Any("assignments", lost))
			if err := cl.CommitMarkedOffsets(ctx); err != nil {
				c.Logger.Error("[BLS_KAFKA] Commit on lost failed", zap.Error(err))
			}
		}),
	}

	var err error
	c.Kafka.BusinessWorkloadClient, err = kgo.NewClient(opts...)
	if err != nil {
		c.Logger.Error("create business workload kafka client failed,", c.CustomCallerField(2), zap.Error(err))
		os.Exit(1)
	}

	c.Logger.Info("create business workload kafka client success,", c.CustomCallerField(2),
		zap.String("topic", c.Kafka.BusinessWorkloadTopic),
		zap.String("group", c.Kafka.BusinessWorkloadGroup),
		zap.String("broker", c.Kafka.Broker))
}

func (c *Config) SetupBigCache() {
	// Use configuration values from config.yaml, with sensible defaults
	// if not specified. This allows runtime configuration of cache behavior
	// without code changes.
	shards := c.Cache.Shards
	if shards <= 0 {
		shards = 64 // Default: reasonable for most workloads
	}

	lifeWindow := time.Duration(c.Cache.LifeTime) * time.Minute
	if lifeWindow <= 0 {
		lifeWindow = 40 * time.Minute
	}

	cleanWindow := time.Duration(c.Cache.CleanWindow) * time.Minute
	// CleanWindow can be 0 (disabled), which is valid

	hardMaxCacheSize := c.Cache.HardMaxCacheSize
	// HardMaxCacheSize can be 0 (unlimited), which is valid

	cache, err := bigcache.New(context.Background(), bigcache.Config{
		Shards:               shards,
		LifeWindow:           lifeWindow,
		CleanWindow:          cleanWindow,
		MaxEntriesInWindow:   0,
		MaxEntrySize:         0,
		StatsEnabled:         true,
		Verbose:              false,
		Hasher:               nil,
		HardMaxCacheSize:     hardMaxCacheSize,
		OnRemove:             nil,
		OnRemoveWithMetadata: nil,
		OnRemoveWithReason:   nil,
		Logger:               nil,
	})
	if err != nil {
		panic(err)
	}
	c.Logger.Info("create bigcache success,", c.CustomCallerField(2))
	for _, s := range c.ExcludeLabels {
		parts := strings.SplitN(s, ":", 2)
		key := strings.TrimSpace(parts[0])
		value := ""
		if len(parts) > 1 {
			value = strings.TrimSpace(parts[1])
		}
		c.Logger.Debug("exclude label: ", c.CustomCallerField(2), zap.String("key", key), zap.String("value", value))
		_ = cache.Set("ExcludeLabels-"+key, []byte(value))
	}
	c.BigCache = cache
}

// Getter methods for dependency injection
// 这些方法将Config从"服务依赖的对象"转变为"依赖提供者"
// 好处：
// 1. 服务只依赖具体接口，不知道Config的存在
// 2. 提高了可测试性（可以Mock单个依赖）
// 3. 提高了可替换性（可以替换Nebula、Kafka等组件）
// 4. 遵循依赖倒置原则（服务依赖抽象接口，而非具体实现）

// GetLogger 获取日志适配器
// 用途：为服务层提供Logger接口实例
// 返回：ZapLoggerAdapter实例，封装了zap.Logger
func (c *Config) GetLogger() *ZapLoggerAdapter {
	return NewZapLoggerAdapter(c.Logger)
}

// GetGraphDB 获取图数据库适配器
// 用途：为服务层提供GraphDB接口实例
// 返回：NebulaGraphAdapter实例，封装了Nebula SessionPool
func (c *Config) GetGraphDB() *NebulaGraphAdapter {
	return NewNebulaGraphAdapter(c.Nebula.SessionPool)
}

// GetMessageQueue 获取消息队列适配器
// 用途：为服务层提供MessageQueue接口实例
// 返回：KafkaAdapter实例，封装了Kafka客户端
func (c *Config) GetMessageQueue() *KafkaAdapter {
	return NewKafkaAdapter(c.Kafka.Client, c.Logger)
}

// GetTraceMessageQueue 获取 Trace 消息队列适配器
func (c *Config) GetTraceMessageQueue() *KafkaAdapter {
	if c.Kafka.TraceClient == nil {
		return nil
	}
	return NewKafkaAdapter(c.Kafka.TraceClient, c.Logger)
}

// GetBusinessWorkloadMessageQueue 获取业务工作负载消息队列适配器
func (c *Config) GetBusinessWorkloadMessageQueue() *KafkaAdapter {
	if c.Kafka.BusinessWorkloadClient == nil {
		return nil
	}
	return NewKafkaAdapter(c.Kafka.BusinessWorkloadClient, c.Logger)
}

// GetCache 获取缓存适配器
// 用途：为服务层提供Cache接口实例
// 返回：BigCacheAdapter实例，封装了BigCache
func (c *Config) GetCache() *BigCacheAdapter {
	return NewBigCacheAdapter(c.BigCache)
}

// GetKubernetesClient 获取Kubernetes客户端
// 用途：为服务层提供K8s客户端实例
// 参数：name - 集群名称
// 返回：K8sClusterClient实例，包含K8s集群连接信息
func (c *Config) GetKubernetesClient(name string) *K8sClusterClient {
	return c.Cluster.K8sClusterClient[name]
}

func (c *Config) GetLLMProvider() interfaces.LLMProvider {
	cfg := c.Diagnosis
	if !cfg.Enabled || cfg.LLM.Provider == "" || cfg.LLM.Provider == "none" {
		return nil
	}
	apiKey := cfg.LLM.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("MUTONG_LLM_API_KEY")
	}
	if apiKey == "" {
		c.Logger.Warn("LLM API key not configured (set MUTONG_LLM_API_KEY env var), diagnosis will use rule-based path only")
		return nil
	}
	timeout := cfg.LLM.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	if cfg.LLM.MaxTokens <= 0 {
		cfg.LLM.MaxTokens = 16384
	}

	baseURL := cfg.LLM.BaseURL
	if baseURL == "" && cfg.LLM.Endpoint != "" {
		baseURL = cfg.LLM.Endpoint
	}

	maxTokens := cfg.LLM.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 16384
	}
	chatModel, err := eino_openai.NewChatModel(context.Background(), &eino_openai.ChatModelConfig{
		APIKey:    apiKey,
		BaseURL:   baseURL,
		Model:     cfg.LLM.Model,
		MaxTokens: &maxTokens,
	})
	if err != nil {
		c.Logger.Warn("Failed to create Eino LLM provider, falling back to HTTP", zap.Error(err))
	} else {
		trackingModel := diagnosis_svc.NewTokenTrackingChatModel(chatModel, c.Logger)
		return diagnosis_svc.NewEinoLLMProvider(trackingModel, time.Duration(timeout)*time.Second, cfg.LLM.Provider, cfg.LLM.Model, baseURL+cfg.LLM.Endpoint)
	}

	embeddingAPIKey := cfg.LLM.Embedding.APIKey
	if embeddingAPIKey == "" {
		embeddingAPIKey = os.Getenv("MUTONG_EMBEDDING_API_KEY")
	}
	if embeddingAPIKey == "" {
		embeddingAPIKey = apiKey
	}

	if baseURL != "" {
		return diagnosis_svc.NewHTTPLLMProviderWithTokens(cfg.LLM.Provider, cfg.LLM.Model, apiKey, cfg.LLM.Endpoint, baseURL, timeout, cfg.LLM.MaxTokens, cfg.LLM.ContextWindow).
			WithEmbeddingConfig(cfg.LLM.Embedding.Model, embeddingAPIKey, cfg.LLM.Embedding.BaseURL)
	}
	return diagnosis_svc.NewHTTPLLMProviderWithTokens(cfg.LLM.Provider, cfg.LLM.Model, apiKey, cfg.LLM.Endpoint, "", timeout, cfg.LLM.MaxTokens, cfg.LLM.ContextWindow).
		WithEmbeddingConfig(cfg.LLM.Embedding.Model, embeddingAPIKey, cfg.LLM.Embedding.BaseURL)
}

// Close 关闭所有资源连接
// 用途：在应用关闭时调用，确保所有连接正确释放
func (c *Config) Close() error {
	var errs []error

	if c.Nebula.SessionPool != nil {
		c.Nebula.SessionPool.Close()
	}

	if c.Kafka.Client != nil {
		c.Kafka.Client.Close()
	}

	if c.Kafka.TraceClient != nil {
		c.Kafka.TraceClient.Close()
	}

	if c.BigCache != nil {
		if err := c.BigCache.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if c.Logger != nil {
		_ = c.Logger.Sync()
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing resources: %v", errs)
	}
	return nil
}

// InitAlertService 初始化告警服务
func (c *Config) InitAlertService(bizCtxProvider interfaces.BusinessContextProvider) (alert_interfaces.AlertProcessor, error) {
	enricher := alert_service.NewAlertEnricher(c.GetLogger(), c.GetGraphDB())

	if bizCtxProvider != nil {
		if enricherImpl, ok := enricher.(*alert_service.AlertEnricher); ok {
			enricherImpl.WithBusinessContextProvider(bizCtxProvider)
		}
	}

	suppressor, err := alert_service.NewAlertSuppressor(
		c.GetLogger(),
		c.GetGraphDB(),
		nil,
		alert_service.SuppressionConfig{
			TimeWindowSeconds:     c.Alert.Suppressor.TimeWindowSeconds,
			MaxDepth:              c.Alert.Suppressor.MaxDepth,
			SeverityExceptions:    c.Alert.Suppressor.SeverityExceptions,
			CriticalityExceptions: c.Alert.Suppressor.CriticalityExceptions,
			MaxAlertAgeMinutes:    c.Alert.Suppressor.MaxAlertAgeMinutes,
			CleanupIntervalSec:    c.Alert.Suppressor.CleanupIntervalSec,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create alert suppressor: %w", err)
	}

	router := alert_service.NewAlertRouter(c.GetLogger(), alert_service.RoutingConfig{
		DefaultReceiver:        c.Alert.Routing.DefaultReceiver,
		DefaultChannel:         c.Alert.Routing.DefaultChannel,
		DefaultSeverity:        c.Alert.Routing.DefaultSeverity,
		SeverityMapping:        c.Alert.Routing.SeverityMapping,
		TeamRouting:            c.Alert.Routing.TeamRouting,
		BusinessContextRouting: c.Alert.Routing.BusinessContextRouting,
		ServiceRouting:         c.Alert.Routing.ServiceRouting,
		SeverityChannelRules:   c.Alert.Routing.SeverityChannelRules,
	})

	notifier := alert_service.NewAlertNotifier(c.GetLogger(), alert_service.NotifierConfig{
		SlackWebhookURL: c.Alert.Notifier.SlackWebhookURL,
		PagerDutyAPIKey: c.Alert.Notifier.PagerDutyAPIKey,
		DingTalkWebhook: c.Alert.Notifier.DingTalkWebhook,
		EmailConfig:     convertEmailConfig(c.Alert.Notifier.EmailConfig),
		ChannelEnabled:  c.Alert.Notifier.ChannelEnabled,
	})

	storage, err := c.initAlertStorage()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize alert storage: %w", err)
	}

	alertService := alert_service.NewAlertService(
		c.GetLogger(),
		c.GetGraphDB(),
		enricher,
		suppressor,
		router,
		notifier,
		storage,
		nil,
	)

	cleanupInterval := time.Duration(c.Alert.Cleanup.CleanupInterval) * time.Hour
	retentionDuration := time.Duration(c.Alert.Cleanup.RetentionDuration) * time.Hour
	if cleanupInterval <= 0 {
		cleanupInterval = 1 * time.Hour
	}
	if retentionDuration <= 0 {
		retentionDuration = 24 * time.Hour
	}
	alertService.WithCleanupConfig(cleanupInterval, retentionDuration)
	alertService.WithDB(c.DB)
	alertService.StartCleanupScheduler()

	// 注入外部告警源配置
	alertSources := make(map[string]*alert_model.AlertSource)
	for i := range c.AlertSources.Sources {
		alertSources[c.AlertSources.Sources[i].Name] = &c.AlertSources.Sources[i]
	}
	alertService.SetAlertSources(alertSources)
	alertService.SetBizCtxProvider(bizCtxProvider)

	c.Logger.Info("External alert sources loaded",
		zap.Int("count", len(alertSources)))

	return alertService, nil
}

func (c *Config) initAlertStorage() (alert_interfaces.AlertStorage, error) {
	storageType := c.Alert.Storage.Type
	if storageType == "" {
		storageType = "memory"
	}

	switch storageType {
	case "postgres":
		if c.DB == nil {
			if err := c.SetupDB(); err != nil {
				return nil, fmt.Errorf("failed to initialize postgres for alert storage: %w", err)
			}
		}
		return alert_service.NewPostgresAlertStorage(c.GetLogger(), c.DB)
	case "memory":
		return alert_service.NewAlertStorage(c.GetLogger()), nil
	default:
		return nil, fmt.Errorf("unsupported alert storage type: %s", storageType)
	}
}

// convertEmailConfig 转换邮件配置
func convertEmailConfig(cfg *AlertEmailConfig) *alert_service.EmailNotifierConfig {
	if cfg == nil {
		return nil
	}
	return &alert_service.EmailNotifierConfig{
		SMTPHost:     cfg.SMTPHost,
		SMTPPort:     cfg.SMTPPort,
		SMTPUser:     cfg.SMTPUser,
		SMTPPassword: cfg.SMTPPassword,
		FromAddress:  cfg.FromAddress,
	}
}

// InitInspectionService 初始化巡检服务
func (c *Config) InitInspectionService(logger interfaces.Logger, graphDB interfaces.GraphDB) (interfaces.InspectionProcessor, error) {
	if !c.Inspection.Enabled {
		return nil, nil
	}

	engine := insp_service.NewInspectionEngine(logger, graphDB)

	// ① 从 YAML 文件加载声明式规则（先注册，优先级最高）
	if concreteEngine, ok := engine.(*insp_service.InspectionEngine); ok {
		c.loadInspectionRules(concreteEngine, graphDB)
	}

	// ② Go 硬编码内置规则（YAML 中已存在的同名规则会被跳过）
	engine.RegisterRule(insp_rules.NewSinglePointFailureRule(logger))
	engine.RegisterRule(insp_rules.NewCMDBDataSiloRule(logger))
	engine.RegisterRule(insp_rules.NewResourceQuotaRule(logger))
	engine.RegisterRule(insp_rules.NewMonitoringBlindspotRule(logger))
	engine.RegisterRule(insp_rules.NewCertExpiryRule(logger))
	engine.RegisterRule(insp_rules.NewImageAuditRule(logger))

	// ③ 从 DB 加载用户自定义规则（最低优先级）
	if c.DB != nil {
		store := insp_service.NewRuleStore(c.DB)
		if concreteEngine, ok := engine.(*insp_service.InspectionEngine); ok {
			_ = concreteEngine.LoadRulesFromDB(store)
		}
	}

	reporter := insp_service.NewInspectionReporter(logger, c.DB)

	processor := insp_service.NewInspectionService(
		logger,
		engine,
		reporter,
		c.Inspection.CronSpec,
	)

	return processor, nil
}

// loadInspectionRules 扫描巡检规则文件（YAML 声明式），加载并注册到 InspectionEngine。
// 规则来源优先级：YAML 文件 > Go 硬编码 > DB 规则（同名覆盖取决于 RegisterRule 先到先得语义）
func (c *Config) loadInspectionRules(engine *insp_service.InspectionEngine, graphDB interfaces.GraphDB) {
	var files []string

	// 1. 通过 Inspection.RuleFiles 显式指定的文件/glob
	for _, pattern := range c.Inspection.RuleFiles {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			c.Logger.Warn("invalid ruleFiles glob pattern, skipping", zap.String("pattern", pattern), zap.Error(err))
			continue
		}
		files = append(files, matches...)
	}

	// 2. 默认扫描 configs/rules/inspection/ 目录
	defaultDir := "configs/rules/inspection"
	scanDefault := len(c.Inspection.RuleFiles) == 0
	if !scanDefault {
		for _, p := range c.Inspection.RuleFiles {
			if p == defaultDir {
				scanDefault = true
				break
			}
		}
	}
	if scanDefault {
		if entries, err := os.ReadDir(defaultDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
					continue
				}
				files = append(files, filepath.Join(defaultDir, entry.Name()))
			}
		}
	}

	if len(files) == 0 {
		return
	}

	// 去重 + 排序
	seen := make(map[string]bool)
	var unique []string
	for _, f := range files {
		if !seen[f] {
			seen[f] = true
			unique = append(unique, f)
		}
	}
	sort.Strings(unique)

	for _, f := range unique {
		data, err := os.ReadFile(f)
		if err != nil {
			c.Logger.Warn("failed to read inspection rule file", zap.String("file", f), zap.Error(err))
			continue
		}

		var rule insp_service.Rule
		if err := yaml.Unmarshal(data, &rule); err != nil {
			c.Logger.Warn("failed to parse inspection rule file", zap.String("file", f), zap.Error(err))
			continue
		}

		if rule.Name == "" {
			c.Logger.Warn("inspection rule file missing 'name' field, skipping", zap.String("file", f))
			continue
		}

		adapter := insp_service.NewYAMLRuleAdapter(rule, c.GetLogger(), graphDB)
		engine.RegisterRule(adapter)
		c.Logger.Debug("loaded inspection rule from YAML", zap.String("rule", rule.Name), zap.String("file", f))
	}
}

func (c *Config) setServerDefaults() {
	if c.Server.Address == "" {
		c.Server.Address = "0.0.0.0"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 8888
	}
	if c.Server.ReadTimeoutSec == 0 {
		c.Server.ReadTimeoutSec = 30
	}
	if c.Server.WriteTimeoutSec == 0 {
		c.Server.WriteTimeoutSec = 600
	}
	if c.Server.IdleTimeoutSec == 0 {
		c.Server.IdleTimeoutSec = 120
	}
	if c.Server.ShutdownTimeoutSec == 0 {
		c.Server.ShutdownTimeoutSec = 10
	}
	if c.Server.RateLimitPerSec == 0 {
		c.Server.RateLimitPerSec = 100
	}
}

func (c *Config) setKafkaInternalDefaults() {
	if c.Kafka.WorkerPoolSize == 0 {
		c.Kafka.WorkerPoolSize = 10
	}
	if c.Kafka.TaskChanBuffer == 0 {
		c.Kafka.TaskChanBuffer = 1000
	}
	if c.Kafka.MaxBackoffMs == 0 {
		c.Kafka.MaxBackoffMs = 30000
	}
	if c.Kafka.PublishTimeoutSec == 0 {
		c.Kafka.PublishTimeoutSec = 60
	}
	if c.Kafka.KafkaSemaphore == 0 {
		c.Kafka.KafkaSemaphore = 50
	}
	if c.Kafka.BizPublishChanBuffer == 0 {
		c.Kafka.BizPublishChanBuffer = 500
	}
}

func (c *Config) setExecutorInternalDefaults() {
	exec := &c.Executor
	if exec.DefaultNamespace == "" {
		exec.DefaultNamespace = "default"
	}
	if exec.MaxRestartCount == 0 {
		exec.MaxRestartCount = 5
	}
	if exec.CoolDownMinutes == 0 {
		exec.CoolDownMinutes = 5
	}
	if exec.GracePeriodSec == 0 {
		exec.GracePeriodSec = 30
	}
	if exec.DeleteGracePeriodSec == 0 {
		exec.DeleteGracePeriodSec = 5
	}
	if exec.MaxReplicas == 0 {
		exec.MaxReplicas = 100
	}
	if exec.DefaultHPA.MinReplicas == 0 {
		exec.DefaultHPA.MinReplicas = 1
	}
	if exec.DefaultHPA.MaxReplicas == 0 {
		exec.DefaultHPA.MaxReplicas = 5
	}
	if exec.DefaultHPA.TargetCPU == 0 {
		exec.DefaultHPA.TargetCPU = 50
	}
}

// loadResourceProfiles 扫描 configs/profiles/ 目录，加载所有 *.yml 资源画像文件
// 并与 ResourceProfiles 中直接定义的画像合并，直接定义的优先
func (c *Config) loadResourceProfiles() {
	var files []string

	// 1. 通过 ResourceProfileFiles 显式指定的文件
	for _, f := range c.ResourceProfileFiles {
		if _, err := os.Stat(f); err == nil {
			files = append(files, f)
		} else {
			c.Logger.Warn("resourceProfile file not found, skipping", zap.String("file", f))
		}
	}

	// 2. 自动扫描 configs/profiles/ 目录
	profileDir := "configs/profiles"
	if entries, err := os.ReadDir(profileDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
				continue
			}
			path := filepath.Join(profileDir, entry.Name())
			// 避免重复加载（显式指定的文件优先级高）
			alreadyListed := false
			for _, f := range c.ResourceProfileFiles {
				if f == path {
					alreadyListed = true
					break
				}
			}
			if !alreadyListed {
				files = append(files, path)
			}
		}
		sort.Strings(files)
	}

	if len(files) == 0 {
		return
	}

	// 初始化 ResourceProfiles map（如果尚未初始化）
	if c.ResourceProfiles == nil {
		c.ResourceProfiles = make(map[string]ResourceProfile)
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			c.Logger.Warn("failed to read resource profile file", zap.String("file", f), zap.Error(err))
			continue
		}

		var pf ResourceProfileFile
		if err := yaml.Unmarshal(data, &pf); err != nil {
			c.Logger.Warn("failed to parse resource profile file", zap.String("file", f), zap.Error(err))
			continue
		}

		if pf.Kind == "" {
			c.Logger.Warn("resource profile file missing 'kind' field, skipping", zap.String("file", f))
			continue
		}

		// 直接定义的 ResourceProfiles 优先（不覆盖已有 key）
		if _, exists := c.ResourceProfiles[pf.Kind]; !exists {
			c.ResourceProfiles[pf.Kind] = ResourceProfile{
				Metrics:  pf.Metrics,
				Topology: pf.Topology,
				Evidence: pf.Evidence,
			}
			c.Logger.Debug("loaded resource profile", zap.String("kind", pf.Kind), zap.String("file", f))
		}
	}
}
