// Package store: 待执行 decisions 黑板（P2-3），交易员产出后写入，状态=待审计；风控通过后执行并更新状态（不依赖 kernel，避免循环引用）
package store

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const (
	PendingStatusPendingAudit = "pending_audit" // 待审计
	PendingStatusApproved     = "approved"     // 已通过，已执行或待执行
	PendingStatusRejected     = "rejected"     // 已驳回
)

// AgentPendingDecisionDB 待执行决策表
type AgentPendingDecisionDB struct {
	ID              int64     `gorm:"primaryKey;autoIncrement"`
	TraderID        string    `gorm:"column:trader_id;not null;index:idx_pending_trader_time"`
	StrategyID      string    `gorm:"column:strategy_id;default:'';index"`
	AnalystReportID int64     `gorm:"column:analyst_report_id;default:0"`
	DecisionsJSON   string    `gorm:"column:decisions_json;type:text;not null"` // 本轮 decisions JSON
	Status          string    `gorm:"column:status;not null;default:'pending_audit';index"`
	RejectReason    string    `gorm:"column:reject_reason;type:text;default:''"`
	CreatedAt       time.Time `gorm:"column:created_at;not null;index:idx_pending_trader_time,sort:desc"`
	UpdatedAt       time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (AgentPendingDecisionDB) TableName() string { return "agent_pending_decisions" }

// AgentPendingDecision 对外结构；DecisionsJSON 由调用方按需 json.Unmarshal 为 []kernel.Decision
type AgentPendingDecision struct {
	ID              int64
	TraderID        string
	StrategyID      string
	AnalystReportID int64
	DecisionsJSON   string
	Status          string
	RejectReason    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// AgentPendingStore 待执行决策存储
type AgentPendingStore struct {
	db *gorm.DB
}

func NewAgentPendingStore(db *gorm.DB) *AgentPendingStore {
	return &AgentPendingStore{db: db}
}

func (s *AgentPendingStore) initTables() error {
	if s.db.Dialector.Name() == "postgres" {
		var n int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'agent_pending_decisions'`).Scan(&n)
		if n > 0 {
			// 已有表：补 Agent 相关列
			for _, q := range []string{
				`ALTER TABLE agent_pending_decisions ADD COLUMN IF NOT EXISTS strategy_id varchar(255) DEFAULT ''`,
				`ALTER TABLE agent_pending_decisions ADD COLUMN IF NOT EXISTS analyst_report_id bigint DEFAULT 0`,
			} {
				if err := s.db.Exec(q).Error; err != nil {
					return fmt.Errorf("failed to migrate agent_pending_decisions: %w", err)
				}
			}
			return nil
		}
	}
	return s.db.AutoMigrate(&AgentPendingDecisionDB{})
}

// Create 写入待审计批次，decisions 可为任意可 JSON 序列化类型（如 []kernel.Decision），返回 ID
func (s *AgentPendingStore) Create(traderID, strategyID string, analystReportID int64, decisions interface{}) (*AgentPendingDecision, error) {
	raw, err := json.Marshal(decisions)
	if err != nil {
		return nil, err
	}
	row := &AgentPendingDecisionDB{
		TraderID:        traderID,
		StrategyID:      strategyID,
		AnalystReportID: analystReportID,
		DecisionsJSON:   string(raw),
		Status:          PendingStatusPendingAudit,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := s.db.Create(row).Error; err != nil {
		return nil, err
	}
	return dbToPending(row), nil
}

// GetByID 按 ID 获取
func (s *AgentPendingStore) GetByID(id int64) (*AgentPendingDecision, error) {
	var row AgentPendingDecisionDB
	if err := s.db.Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return dbToPending(&row), nil
}

// UpdateStatus 更新状态（approved / rejected），rejectReason 在驳回时填写
func (s *AgentPendingStore) UpdateStatus(id int64, status, rejectReason string) error {
	up := map[string]interface{}{"status": status, "updated_at": time.Now().UTC()}
	if rejectReason != "" {
		up["reject_reason"] = rejectReason
	}
	return s.db.Model(&AgentPendingDecisionDB{}).Where("id = ?", id).Updates(up).Error
}

// GetLatestPendingByTrader 获取某交易员最新一条待审计记录（供风控官读取）
func (s *AgentPendingStore) GetLatestPendingByTrader(traderID string) (*AgentPendingDecision, error) {
	var row AgentPendingDecisionDB
	err := s.db.Where("trader_id = ? AND status = ?", traderID, PendingStatusPendingAudit).
		Order("created_at DESC").Limit(1).First(&row).Error
	if err != nil {
		return nil, err
	}
	return dbToPending(&row), nil
}

func dbToPending(db *AgentPendingDecisionDB) *AgentPendingDecision {
	return &AgentPendingDecision{
		ID:              db.ID,
		TraderID:        db.TraderID,
		StrategyID:      db.StrategyID,
		AnalystReportID: db.AnalystReportID,
		DecisionsJSON:   db.DecisionsJSON,
		Status:          db.Status,
		RejectReason:    db.RejectReason,
		CreatedAt:       db.CreatedAt,
		UpdatedAt:       db.UpdatedAt,
	}
}
