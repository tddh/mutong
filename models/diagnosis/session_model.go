package diagnosis

import "time"

// ChatSessionModel 会话主表 GORM 模型
type ChatSessionModel struct {
	ID               string     `gorm:"primaryKey;type:varchar(32);comment:会话ID" json:"id"`
	UserID           uint       `gorm:"index:idx_user_status;not null;default:0;comment:用户ID" json:"user_id"`
	Title            string     `gorm:"type:varchar(255);comment:会话标题" json:"title"`
	Status           int8       `gorm:"index:idx_user_status;type:smallint;not null;default:1;comment:1:active 2:closed 3:expired" json:"status"`
	AlertFingerprint string     `gorm:"type:varchar(64);index:idx_alert;comment:关联告警指纹" json:"alert_fingerprint"`
	ResourceKind     string     `gorm:"type:varchar(50);comment:资源类型" json:"resource_kind"`
	ResourceName     string     `gorm:"type:varchar(200);comment:资源名称" json:"resource_name"`
	Namespace        string     `gorm:"type:varchar(100);comment:命名空间" json:"namespace"`
	CreatedAt        time.Time  `gorm:"not null;autoCreateTime;comment:创建时间" json:"created_at"`
	LastActiveAt     time.Time  `gorm:"not null;autoUpdateTime;comment:最后活跃时间" json:"last_active_at"`
	ClosedAt         *time.Time `gorm:"comment:关闭时间" json:"closed_at"`
}

func (ChatSessionModel) TableName() string {
	return "chat_sessions"
}

// ChatMessageModel 消息表 GORM 模型
type ChatMessageModel struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID string    `gorm:"type:varchar(32);not null;index:idx_session;comment:关联会话" json:"session_id"`
	MsgID     string    `gorm:"type:varchar(50);not null;index:idx_msg_id;comment:消息唯一ID" json:"msg_id"`
	Role      string    `gorm:"type:varchar(20);not null;comment:user/assistant/system" json:"role"`
	Content   string    `gorm:"type:text;not null;comment:消息内容" json:"content"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime;comment:创建时间" json:"created_at"`
}

func (ChatMessageModel) TableName() string {
	return "chat_messages"
}

// DiagnosisContextModel 诊断上下文快照表 GORM 模型
type DiagnosisContextModel struct {
	SessionID      string    `gorm:"primaryKey;type:varchar(32);comment:会话ID" json:"session_id"`
	AlertInfo      *string   `gorm:"type:json;comment:告警信息快照" json:"alert_info"`
	TopologyRef    *string   `gorm:"type:json;comment:拓扑查询参数" json:"topology_ref"`
	MetricsQueries *string   `gorm:"type:json;comment:指标查询参数" json:"metrics_queries"`
	LogsQueries    *string   `gorm:"type:json;comment:日志查询参数" json:"logs_queries"`
	CreatedAt      time.Time `gorm:"not null;autoCreateTime;comment:创建时间" json:"created_at"`
}

func (DiagnosisContextModel) TableName() string {
	return "diagnosis_contexts"
}
