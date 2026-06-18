package config

import (
	"time"

	"github.com/allegro/bigcache/v3"
	"github.com/twmb/franz-go/pkg/kgo"
	nebula "github.com/vesoft-inc/nebula-go/v3"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	alert_model "gitee.com/tddh/mutong/models/alert"
)

type Config struct {
	Kubernetes       KubeConfig        `yaml:"kubernetes" json:"kubernetes"`
	Postgres         PostgresConfig    `yaml:"postgres" json:"postgres"`
	Kafka            Kafka             `yaml:"kafka" json:"kafka"`
	Nebula           Nebula            `yaml:"nebula" json:"nebula"`
	NebulaCleanup    NebulaCleanupConf `yaml:"nebulaCleanup" json:"nebulaCleanup"`
	Cache            Cache             `yaml:"cache" json:"cache"`
	Log              Log               `yaml:"log" json:"log"`
	Alert            Alert             `yaml:"alert" json:"alert"`
	Inspection       Inspection        `yaml:"inspection" json:"inspection"`
	Diagnosis        Diagnosis         `yaml:"diagnosis" json:"diagnosis"`
	Cluster          *Cluster
	BigCache         *bigcache.BigCache
	DB               *gorm.DB
	Logger           *zap.Logger
	ExcludeLabels    []string                   `yaml:"excludeLabels" json:"excludeLabels,omitempty"`
	Prometheus       PrometheusConf             `yaml:"prometheus" json:"prometheus"`
	Elasticsearch    ElasticsearchConf          `yaml:"elasticsearch" json:"elasticsearch"`
	Executor         ExecutorConf               `yaml:"executor" json:"executor"`
	ResourceProfiles map[string]ResourceProfile `yaml:"resourceProfiles" json:"resourceProfiles,omitempty"`
	// ResourceProfileFiles 显式指定的画像文件路径列表（留空则自动扫描 configs/profiles/*.yml）
	ResourceProfileFiles []string `yaml:"resourceProfileFiles" json:"resourceProfileFiles,omitempty"`
	// RoleConfig provides role-based access control (RBAC) configuration
	Role RoleConf `yaml:"roles" json:"roles"`
	// BusinessTopology provides business topology configuration
	BusinessTopology BusinessTopologyConf `yaml:"businessTopology" json:"businessTopology"`
	// Retrospective provides postmortem/retrospective configuration
	Retrospective RetrospectiveConf `yaml:"retrospective" json:"retrospective"`
	// Server holds HTTP server tuning parameters（导出硬编码为可配置项）
	Server ServerConfig `yaml:"server" json:"server"`
	// AlertSources holds external alert source configurations for cross-system alert integration
	AlertSources AlertSourceConfig `yaml:"alertSources" json:"alertSources"`
	// Auth holds authentication and authorization configuration
	Auth AuthConfig `yaml:"auth" json:"auth"`
	// OpenTelemetry holds distributed tracing configuration
	OpenTelemetry OpenTelemetryConf `yaml:"opentelemetry" json:"opentelemetry"`
	// ExternalSearch holds external knowledge base search configuration
	ExternalSearch ExternalSearchConfig `yaml:"external_search" json:"external_search"`
	// Sanitizer holds PII redaction and security rules configuration (applies to all external calls)
	Sanitizer SanitizerConfig `yaml:"sanitizer" json:"sanitizer"`
}

type ResourceProfile struct {
	Metrics  []MetricProfile   `yaml:"metrics" json:"metrics,omitempty"`
	Topology []TopologyProfile `yaml:"topology" json:"topology,omitempty"`
	Evidence []EvidenceProfile `yaml:"evidence" json:"evidence,omitempty"`
}

type MetricProfile struct {
	Name   string `yaml:"name" json:"name"`
	PromQL string `yaml:"promql" json:"promql"`
	Unit   string `yaml:"unit" json:"unit,omitempty"`
}

type TopologyProfile struct {
	Relation   string `yaml:"relation" json:"relation"`
	Direction  string `yaml:"direction" json:"direction"`
	Label      string `yaml:"label" json:"label,omitempty"`
	MaxResults int    `yaml:"maxResults" json:"maxResults,omitempty"`
}

type EvidenceProfile struct {
	Source   string `yaml:"source" json:"source"`
	Index    string `yaml:"index" json:"index,omitempty"`
	Query    string `yaml:"query" json:"query,omitempty"`
	Level    string `yaml:"level" json:"level,omitempty"`
	Limit    int    `yaml:"limit" json:"limit,omitempty"`
	Fallback string `yaml:"fallback" json:"fallback,omitempty"`
}

// ResourceProfileFile 表示单个资源类型的画像配置文件格式
// 用于 configs/profiles/ 目录下的独立 profile YAML 文件
type ResourceProfileFile struct {
	Kind     string            `yaml:"kind" json:"kind"`
	Metrics  []MetricProfile   `yaml:"metrics" json:"metrics,omitempty"`
	Topology []TopologyProfile `yaml:"topology" json:"topology,omitempty"`
	Evidence []EvidenceProfile `yaml:"evidence" json:"evidence,omitempty"`
}

// OpenTelemetryConf holds the configuration for OpenTelemetry tracing integration
// It is used by the trace querying services to connect to the collector (Jaeger/Tempo/OTLP).
type OpenTelemetryConf struct {
	Enabled      bool   `yaml:"enabled" json:"enabled"`
	CollectorURL string `yaml:"collectorURL" json:"collectorURL"`
	ServiceName  string `yaml:"serviceName" json:"serviceName"`
	Timeout      int    `yaml:"timeout" json:"timeout"`
}

type ExcludeLabels struct {
	ExcludeLabels map[string]string `yaml:"excludeLabels,inline" json:"excludeLabels,omitempty"`
}

type Cache struct {
	LifeTime         int  `yaml:"lifeTime" json:"lifeTime,omitempty"`                 // BigCache 条目存活时间（分钟），默认 40
	CleanWindow      int  `yaml:"cleanWindow" json:"cleanWindow,omitempty"`           // BigCache 清理窗口（分钟），默认 10
	Enable           bool `yaml:"enable" json:"enable,omitempty"`                     // 是否启用 ResourceCache
	HardMaxCacheSize int  `yaml:"hardMaxCacheSize" json:"hardMaxCacheSize,omitempty"` // 最大缓存大小（MB）
	Shards           int  `yaml:"shards" json:"shards,omitempty"`                     // BigCache 分片数
	ResyncInterval   int  `yaml:"resyncInterval" json:"resyncInterval,omitempty"`     // Informer resync 间隔（分钟），默认 30
}

type PostgresConfig struct {
	Host     string `yaml:"host" json:"host,omitempty"`
	Port     string `yaml:"port" json:"port,omitempty"`
	User     string `yaml:"user" json:"user,omitempty"`
	Pass     string `yaml:"pass" json:"pass,omitempty"`
	Database string `yaml:"database" json:"database,omitempty"`
	Pool     int    `yaml:"pool" json:"pool,omitempty"`
}

type Kafka struct {
	Broker              string `yaml:"broker" json:"broker"`
	Topic               string `yaml:"topic" json:"topic"`
	Group               string `yaml:"group" json:"group"`
	DeadLetterTopic     string `yaml:"deadLetterTopic" json:"deadLetterTopic,omitempty"`
	MaxRetries          int    `yaml:"maxRetries" json:"maxRetries,omitempty"`
	RetryBackoffMs      int    `yaml:"retryBackoffMs" json:"retryBackoffMs,omitempty"`
	MaxRecordBytes      int32  `yaml:"maxRecordBytes" json:"maxRecordBytes,omitempty"`
	MaxBrokerWriteBytes int32  `yaml:"maxBrokerWriteBytes" json:"maxBrokerWriteBytes,omitempty"`
	FetchMaxBytes       int32  `yaml:"fetchMaxBytes" json:"fetchMaxBytes,omitempty"`
	FetchMaxPartBytes   int32  `yaml:"fetchMaxPartBytes" json:"fetchMaxPartBytes,omitempty"`
	Client              *kgo.Client
	// Trace 专用 Kafka 配置（用于消费 OTel Collector 导出的 Traces）
	TraceTopic  string `yaml:"traceTopic" json:"traceTopic,omitempty"`
	TraceGroup  string `yaml:"traceGroup" json:"traceGroup,omitempty"`
	TraceClient *kgo.Client

	// Business Workload 专用 Kafka 配置（用于 BusinessLabelSyncer 消费 Workload 变更）
	BusinessWorkloadTopic  string `yaml:"businessWorkloadTopic" json:"businessWorkloadTopic,omitempty"`
	BusinessWorkloadGroup  string `yaml:"businessWorkloadGroup" json:"businessWorkloadGroup,omitempty"`
	BusinessWorkloadClient *kgo.Client
	WorkloadKinds          []string `yaml:"workloadKinds" json:"workloadKinds,omitempty"`
	WorkerPoolSize         int      `yaml:"workerPoolSize" json:"workerPoolSize,omitempty"`
	TaskChanBuffer         int      `yaml:"taskChanBuffer" json:"taskChanBuffer,omitempty"`
	MaxBackoffMs           int      `yaml:"maxBackoffMs" json:"maxBackoffMs,omitempty"`
	PublishTimeoutSec      int      `yaml:"publishTimeoutSec" json:"publishTimeoutSec,omitempty"`
	KafkaSemaphore         int      `yaml:"kafkaSemaphore" json:"kafkaSemaphore,omitempty"`
	BizPublishChanBuffer   int      `yaml:"bizPublishChanBuffer" json:"bizPublishChanBuffer,omitempty"`
}

type Nebula struct {
	Host  string `yaml:"host" json:"host"`
	Port  int    `yaml:"port" json:"port"`
	User  string `yaml:"user" json:"user"`
	Pass  string `yaml:"pass" json:"pass"`
	Space string `yaml:"space" json:"space"`
	// Pool        map[string]string `yaml:"pool" json:"pool"`
	SessionPool *nebula.SessionPool
}

// NebulaCleanupConf holds configuration for periodic cleanup of deleted K8s resources
type NebulaCleanupConf struct {
	Enabled         bool `yaml:"enabled" json:"enabled"`                 // 是否启用定时清理
	RetentionDays   int  `yaml:"retentionDays" json:"retentionDays"`     // 已删除资源保留天数
	CleanupInterval int  `yaml:"cleanupInterval" json:"cleanupInterval"` // 清理执行间隔（小时）
	BatchSize       int  `yaml:"batchSize" json:"batchSize"`             // 每批删除数量
}

type Log struct {
	Level   string `yaml:"level" json:"level,omitempty"`
	Path    string `yaml:"path" json:"path,omitempty"`
	MaxSize int    `yaml:"max_size" json:"maxSize,omitempty"`
	MaxAge  int    `yaml:"max_age" json:"maxAge,omitempty"`
	// Format 表示日志输出格式: json 或 console
	Format string `yaml:"format" json:"format,omitempty"`
	// Output 指定输出目标: stdout, file, 或 both
	Output string `yaml:"output" json:"output,omitempty"`
}

// Alert 告警配置
type Alert struct {
	Routing           AlertRouting    `yaml:"routing" json:"routing"`
	Notifier          AlertNotifier   `yaml:"notifier" json:"notifier"`
	Suppressor        AlertSuppressor `yaml:"suppressor" json:"suppressor"`
	Cleanup           AlertCleanup    `yaml:"cleanup" json:"cleanup"`
	Storage           AlertStorage    `yaml:"storage" json:"storage"`
	StatsSyncInterval int             `yaml:"statsSyncInterval" json:"statsSyncInterval"`
}

// AlertStorage 告警存储配置
type AlertStorage struct {
	Type string `yaml:"type" json:"type"` // memory, mysql
}

// AlertRouting 告警路由配置
type AlertRouting struct {
	DefaultReceiver        string            `yaml:"defaultReceiver" json:"defaultReceiver"`
	DefaultChannel         string            `yaml:"defaultChannel" json:"defaultChannel"`
	DefaultSeverity        string            `yaml:"defaultSeverity" json:"defaultSeverity"`
	SeverityMapping        map[string]int    `yaml:"severityMapping" json:"severityMapping"`
	TeamRouting            map[string]string `yaml:"teamRouting" json:"teamRouting"`
	BusinessContextRouting map[string]string `yaml:"businessContextRouting" json:"businessContextRouting"`
	ServiceRouting         map[string]string `yaml:"serviceRouting" json:"serviceRouting"`
	SeverityChannelRules   map[string]string `yaml:"severityChannelRules" json:"severityChannelRules"`
}

// AlertNotifier 告警通知配置
type AlertNotifier struct {
	SlackWebhookURL string            `yaml:"slackWebhookUrl" json:"slackWebhookUrl"`
	PagerDutyAPIKey string            `yaml:"pagerdutyApiKey" json:"pagerdutyApiKey"`
	DingTalkWebhook string            `yaml:"dingtalkWebhook" json:"dingtalkWebhook"`
	EmailConfig     *AlertEmailConfig `yaml:"emailConfig" json:"emailConfig"`
	ChannelEnabled  map[string]bool   `yaml:"channelEnabled" json:"channelEnabled"`
}

// AlertEmailConfig 邮件通知配置
type AlertEmailConfig struct {
	SMTPHost     string `yaml:"smtpHost" json:"smtpHost"`
	SMTPPort     int    `yaml:"smtpPort" json:"smtpPort"`
	SMTPUser     string `yaml:"smtpUser" json:"smtpUser"`
	SMTPPassword string `yaml:"smtpPassword" json:"smtpPassword"`
	FromAddress  string `yaml:"fromAddress" json:"fromAddress"`
}

// AlertSuppressor 告警抑制配置
type AlertSuppressor struct {
	RuleChains            []string `yaml:"ruleChains" json:"ruleChains"`
	TimeWindowSeconds     int      `yaml:"timeWindowSeconds" json:"timeWindowSeconds"`
	MaxDepth              int      `yaml:"maxDepth" json:"maxDepth"`
	SeverityExceptions    []string `yaml:"severityExceptions" json:"severityExceptions"`
	CriticalityExceptions []string `yaml:"criticalityExceptions" json:"criticalityExceptions"`
	MaxAlertAgeMinutes    int      `yaml:"maxAlertAgeMinutes" json:"maxAlertAgeMinutes"`
	CleanupIntervalSec    int      `yaml:"cleanupIntervalSec" json:"cleanupIntervalSec"`
}

// AlertCleanup 告警清理配置
type AlertCleanup struct {
	CleanupInterval   int `yaml:"cleanupInterval"`   // 清理间隔 (小时)
	RetentionDuration int `yaml:"retentionDuration"` // 保留时长 (小时)
}

type AlertSourceConfig struct {
	Sources []alert_model.AlertSource `yaml:"sources" json:"sources"`
}

type KubeConfig struct {
	Config   string         `yaml:"config" json:"config,omitempty"`
	Clusters []ClusterEntry `yaml:"clusters" json:"clusters,omitempty"`
}

type ClusterEntry struct {
	Name    string `yaml:"name" json:"name"`
	Config  string `yaml:"config" json:"config"`
	Context string `yaml:"context" json:"context"`
	Region  string `yaml:"region" json:"region"`
	Enable  bool   `yaml:"enable" json:"enable"`
}

// Inspection 巡检配置
type Inspection struct {
	Enabled   bool     `yaml:"enabled"`
	CronSpec  string   `yaml:"cronSpec"`
	RuleFiles []string `yaml:"ruleFiles"` // 巡检规则文件路径，为空则使用 configs/rules/inspection/
}

type Diagnosis struct {
	Enabled             bool         `yaml:"enabled" json:"enabled"`
	ConfidenceThreshold float64      `yaml:"confidenceThreshold" json:"confidenceThreshold"`
	CacheTTL            int          `yaml:"cacheTTL" json:"cacheTTL"`
	ResultRetentionDays int          `yaml:"resultRetentionDays" json:"resultRetentionDays"`
	StatsSyncInterval   int          `yaml:"statsSyncInterval" json:"statsSyncInterval"`
	LLM                 DiagnosisLLM `yaml:"llm" json:"llm"`
	Session             SessionConf  `yaml:"session" json:"session"`
}

type SessionConf struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	Redis    string `yaml:"redis" json:"redis"`
	Password string `yaml:"password" json:"password"`
	DB       int    `yaml:"db" json:"db"`
	TTL      int    `yaml:"ttl" json:"ttl"`
}

type DiagnosisLLM struct {
	Provider      string          `yaml:"provider" json:"provider"`
	Model         string          `yaml:"model" json:"model"`
	APIKey        string          `yaml:"apiKey" json:"apiKey"`
	BaseURL       string          `yaml:"baseURL" json:"baseURL"`
	Endpoint      string          `yaml:"endpoint" json:"endpoint"`
	Timeout       int             `yaml:"timeout" json:"timeout"`
	MaxTokens     int             `yaml:"maxTokens" json:"maxTokens"`
	ContextWindow int             `yaml:"contextWindow" json:"contextWindow"`
	Embedding     EmbeddingConfig `yaml:"embedding" json:"embedding"`
}

type EmbeddingConfig struct {
	Model   string `yaml:"model" json:"model"`
	APIKey  string `yaml:"apiKey" json:"apiKey"`
	BaseURL string `yaml:"baseURL" json:"baseURL"`
}

type PrometheusConf struct {
	Enabled bool                `yaml:"enabled" json:"enabled"`
	URL     string              `yaml:"url" json:"url"`
	Timeout int                 `yaml:"timeout" json:"timeout"`
	Cache   PrometheusCacheConf `yaml:"cache" json:"cache"`
}

// PrometheusCacheConf defines cache settings for Prometheus query results
// that are safe to cache (e.g. range queries). Cache is optional and is
// controlled by PrometheusConf.Cache.Enabled.
type PrometheusCacheConf struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	TTL     int  `yaml:"ttl" json:"ttl"`         // seconds
	MaxSize int  `yaml:"maxSize" json:"maxSize"` // maximum number of entries (approximate)
}

type ElasticsearchConf struct {
	Addresses      []string          `yaml:"addresses" json:"addresses"`
	IndexPattern   string            `yaml:"indexPattern" json:"indexPattern"`
	ServiceToIndex map[string]string `yaml:"serviceToIndex" json:"serviceToIndex,omitempty"`
	Username       string            `yaml:"username" json:"username"`
	Password       string            `yaml:"password" json:"password"`
	Timeout        int               `yaml:"timeout" json:"timeout"`
}

// ExecutorConf contains configuration for the self-healing executor
type ExecutorConf struct {
	Enabled              bool                  `yaml:"enabled" json:"enabled"`
	AutoMode             bool                  `yaml:"autoMode" json:"autoMode"`
	AuditLog             AuditLogConf          `yaml:"auditLog" json:"auditLog"`
	Actions              map[string]ActionConf `yaml:"actions" json:"actions"`
	DefaultNamespace     string                `yaml:"defaultNamespace" json:"defaultNamespace,omitempty"`
	MaxRestartCount      int                   `yaml:"maxRestartCount" json:"maxRestartCount,omitempty"`
	CoolDownMinutes      int                   `yaml:"coolDownMinutes" json:"coolDownMinutes,omitempty"`
	GracePeriodSec       int                   `yaml:"gracePeriodSec" json:"gracePeriodSec,omitempty"`
	DeleteGracePeriodSec int                   `yaml:"deleteGracePeriodSec" json:"deleteGracePeriodSec,omitempty"`
	MaxReplicas          int                   `yaml:"maxReplicas" json:"maxReplicas,omitempty"`
	DefaultHPA           DefaultHPAConfig      `yaml:"defaultHPA" json:"defaultHPA,omitempty"`
}

type DefaultHPAConfig struct {
	MinReplicas int32 `yaml:"minReplicas" json:"minReplicas"`
	MaxReplicas int32 `yaml:"maxReplicas" json:"maxReplicas"`
	TargetCPU   int32 `yaml:"targetCPU" json:"targetCPU"`
}

// AuditLogConf defines how audit logs are stored/persisted for executor
// This supports both in-memory storage and MySQL-based persistence.
// Backward compatibility is preserved via the existing ConnectionString/Table
// fields, while a new nested MySQL config can be supplied for explicit MySQL
// based persistence.
type AuditLogConf struct {
	Type             string            `yaml:"type" json:"type"`
	ConnectionString string            `yaml:"connectionString" json:"connectionString"`
	Table            string            `yaml:"table" json:"table"`
	MySQL            AuditLogMySQLConf `yaml:"mysql" json:"mysql"`
}

// AuditLogMySQLConf holds MySQL specific configuration for audit log storage
// when Type is set to "mysql".
type AuditLogMySQLConf struct {
	Host     string `yaml:"host" json:"host"`
	Port     string `yaml:"port" json:"port"`
	User     string `yaml:"user" json:"user"`
	Password string `yaml:"password" json:"password"`
	Database string `yaml:"database" json:"database"`
	Pool     int    `yaml:"pool" json:"pool"`
	Table    string `yaml:"table" json:"table"`
}

// ActionConf defines per-action auto-approval thresholds and risk
type ActionConf struct {
	Risk          string  `yaml:"risk" json:"risk"`
	AutoThreshold float64 `yaml:"autoThreshold" json:"autoThreshold"`
}

type ResourceMap struct {
	Name     string
	Resource string
	Labels   map[string]string
	Yaml     interface{}
	Type     string
}

// RoleConf holds RBAC configuration loaded from config.yaml
type RoleConf struct {
	Enabled     bool             `yaml:"enabled" json:"enabled"`
	DefaultRole string           `yaml:"defaultRole" json:"defaultRole"`
	Roles       []RoleDefinition `yaml:"roles" json:"roles"`
}

// RoleDefinition describes a single role and its permissions and dashboard
type RoleDefinition struct {
	Name        string   `yaml:"name" json:"name"`
	Permissions []string `yaml:"permissions" json:"permissions"`
	Dashboard   string   `yaml:"dashboard" json:"dashboard"`
}

// Cluster 集群信息
type Cluster struct {
	gorm.Model       `json:"-"`
	Region           string                       `gorm:"column:region;index:idx_cluster,unique;size:20;; not null"`
	Name             string                       `gorm:"column:name;index:idx_cluster,unique;size:64; not null"`
	Config           string                       `gorm:"column:config;type:text; not null"`
	Context          string                       `gorm:"column:context; not null"`
	Endpoint         string                       `gorm:"column:endpoint;size:256"`
	Status           string                       `gorm:"column:status;size:20;default:unknown"`
	LastHealthCheck  time.Time                    `gorm:"column:last_health_check"`
	K8sClusterClient map[string]*K8sClusterClient `gorm:"-"`
	Enable           bool                         `gorm:"column:enable;index; not null"`
}

type K8sClusterClient struct {
	Name                string
	RootRawConfig       *api.Config
	RootRestConfig      *rest.Config
	RootKubeClientSet   *kubernetes.Clientset
	RootDiscoveryClient *discovery.DiscoveryClient
	RootClientConfig    *clientcmd.OverridingClientConfig
	Factory             *informers.SharedInformerFactory
	Status              string
	LastHealthCheck     time.Time
}

func (c Cluster) TableName() string {
	return "cluster"
}

// GetExecutorConf returns the executor configuration
func (c *Config) GetExecutorConf() ExecutorConf {
	return c.Executor
}

// BusinessTopologyConf holds business topology configuration
type BusinessTopologyConf struct {
	Enabled              bool                             `yaml:"enabled" json:"enabled"`
	NamespaceMapping     map[string]NamespaceMappingEntry `yaml:"namespaceMapping" json:"namespaceMapping"`
	AppNameNormalization AppNameNormalizationConf         `yaml:"appNameNormalization" json:"appNameNormalization"`
	KnownServices        map[string]KnownServiceEntry     `yaml:"knownServices" json:"knownServices"`
}

// KnownServiceEntry 定义静态配置的 IP 到服务映射
type KnownServiceEntry struct {
	Name      string `yaml:"name" json:"name"`
	Namespace string `yaml:"namespace" json:"namespace"`
}

// AppNameNormalizationConf defines automatic app name normalization rules.
// When no business label is found, these rules are applied to derive a
// normalized app name from the K8s resource name before falling back to
// the raw resource name.
type AppNameNormalizationConf struct {
	Enabled bool                     `yaml:"enabled" json:"enabled"`
	Rules   []NormalizationRuleEntry `yaml:"rules" json:"rules"`
}

// NormalizationRuleEntry defines a single normalization rule.
// Each rule can be scoped to specific namespaces and workload kinds.
type NormalizationRuleEntry struct {
	Name           string   `yaml:"name" json:"name"`               // Human-readable rule description
	NamespaceMatch string   `yaml:"namespaceMatch,omitempty"`       // Glob pattern for namespace (empty = all)
	KindMatch      []string `yaml:"kindMatch,omitempty"`            // Workload kinds to match (empty = all)
	Pattern        string   `yaml:"pattern" json:"pattern"`         // Regex pattern to match
	Replacement    string   `yaml:"replacement" json:"replacement"` // Replacement template ($1, $2, ...)
}

// NamespaceMappingEntry defines how a namespace maps to business attributes
type NamespaceMappingEntry struct {
	BusinessUnit string `yaml:"businessUnit" json:"businessUnit"`
	Team         string `yaml:"team" json:"team"`
	Criticality  string `yaml:"criticality" json:"criticality"`
	Environment  string `yaml:"environment" json:"environment"`
}

// RetrospectiveConf holds postmortem/retrospective configuration
type RetrospectiveConf struct {
	RetentionDays int                   `yaml:"retentionDays" json:"retentionDays"`
	AutoTrigger   AutoRetrospectiveConf `yaml:"autoTrigger" json:"autoTrigger"`
}

// AutoRetrospectiveConf holds auto-trigger configuration for retrospective generation
type AutoRetrospectiveConf struct {
	Enabled                  bool   `yaml:"enabled" json:"enabled"`
	DelayMinutes             int    `yaml:"delayMinutes" json:"delayMinutes"`
	MinSeverity              string `yaml:"minSeverity" json:"minSeverity"`
	MaxPerHour               int    `yaml:"maxPerHour" json:"maxPerHour"`
	SkipIfDiagnosisOlderThan string `yaml:"skipIfDiagnosisOlderThan" json:"skipIfDiagnosisOlderThan"`
}

// ZitadelConfig holds Zitadel OIDC configuration.
type ZitadelConfig struct {
	Issuer       string   `yaml:"issuer" json:"issuer"`
	ClientID     string   `yaml:"client_id" json:"client_id"`
	ClientSecret string   `yaml:"client_secret" json:"client_secret"`
	RedirectURI  string   `yaml:"redirect_uri" json:"redirect_uri"`
	Scopes       []string `yaml:"scopes" json:"scopes"`
	Audience     string   `yaml:"audience" json:"audience"`
	DeviceClient string   `yaml:"device_client" json:"device_client"`
}

// AuthConfig holds authentication and authorization configuration.
type AuthConfig struct {
	Mode         string        `yaml:"mode" json:"mode"`
	GlobalSecret string        `yaml:"global_secret" json:"global_secret"`
	Zitadel      ZitadelConfig `yaml:"zitadel" json:"zitadel"`
}

// ServerConfig holds HTTP server tuning parameters.
type ServerConfig struct {
	Address            string `mapstructure:"address"`
	Port               int    `mapstructure:"port"`
	ReadTimeoutSec     int    `mapstructure:"readTimeoutSec"`
	WriteTimeoutSec    int    `mapstructure:"writeTimeoutSec"`
	IdleTimeoutSec     int    `mapstructure:"idleTimeoutSec"`
	ShutdownTimeoutSec int    `mapstructure:"shutdownTimeoutSec"`
	RateLimitPerSec    int    `mapstructure:"rateLimitPerSec"`
}
