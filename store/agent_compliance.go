// Package store: 风控审计记录表（P2-2 可选），便于回溯「哪次决策对应哪次审计」
package store

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// AgentComplianceAuditDB 风控审计记录；含 system/user prompt、单条审批 decisions_audit_json 供单轮详情展示
type AgentComplianceAuditDB struct {
	ID                 int64     `gorm:"primaryKey;autoIncrement"`
	PendingID          int64     `gorm:"column:pending_id;not null;index"`
	TraderID           string    `gorm:"column:trader_id;not null;index"`
	Approved           bool      `gorm:"column:approved;not null"`
	Reason             string    `gorm:"column:reason;type:text;default:''"`
	ViolationsJSON     string    `gorm:"column:violations_json;type:text;default:''"`
	DecisionsAuditJSON string    `gorm:"column:decisions_audit_json;type:text;default:''"` // 单条审批 [{"index":0,"approved":true,"reason":""},...]
	SystemPrompt       string    `gorm:"column:system_prompt;type:text;default:''"`
	UserPrompt         string    `gorm:"column:user_prompt;type:text;default:''"`
	CreatedAt          time.Time `gorm:"column:created_at;not null"`
}

func (AgentComplianceAuditDB) TableName() string { return "agent_compliance_audits" }

// AgentComplianceStore 风控审计存储
type AgentComplianceStore struct {
	db *gorm.DB
}

func NewAgentComplianceStore(db *gorm.DB) *AgentComplianceStore {
	return &AgentComplianceStore{db: db}
}

func (s *AgentComplianceStore) initTables() error {
	if s.db.Dialector.Name() == "postgres" {
		var n int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'agent_compliance_audits'`).Scan(&n)
		if n > 0 {
			for _, q := range []string{
				`ALTER TABLE agent_compliance_audits ADD COLUMN IF NOT EXISTS system_prompt text DEFAULT ''`,
				`ALTER TABLE agent_compliance_audits ADD COLUMN IF NOT EXISTS user_prompt text DEFAULT ''`,
				`ALTER TABLE agent_compliance_audits ADD COLUMN IF NOT EXISTS decisions_audit_json text DEFAULT ''`,
			} {
				if err := s.db.Exec(q).Error; err != nil {
					return fmt.Errorf("failed to migrate agent_compliance_audits: %w", err)
				}
			}
			return nil
		}
	}
	return s.db.AutoMigrate(&AgentComplianceAuditDB{})
}

// Create 写入一条审计记录，返回审计 ID；decisionsAuditJSON 为单条审批 JSON，可为空
func (s *AgentComplianceStore) Create(pendingID int64, traderID string, approved bool, reason, violationsJSON, decisionsAuditJSON, systemPrompt, userPrompt string) (int64, error) {
	row := &AgentComplianceAuditDB{
		PendingID:          pendingID,
		TraderID:           traderID,
		Approved:           approved,
		Reason:             reason,
		ViolationsJSON:     violationsJSON,
		DecisionsAuditJSON: decisionsAuditJSON,
		SystemPrompt:       systemPrompt,
		UserPrompt:         userPrompt,
		CreatedAt:          time.Now().UTC(),
	}
	if err := s.db.Create(row).Error; err != nil {
		return 0, err
	}
	return row.ID, nil
}

// GetByID 按 ID 获取审计记录（P4-3 Dashboard 用）
func (s *AgentComplianceStore) GetByID(id int64) (*AgentComplianceAuditDB, error) {
	var row AgentComplianceAuditDB
	if err := s.db.Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
