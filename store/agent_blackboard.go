// Package store: Agent blackboard for multi-agent flow (analyst reports, etc.)
package store

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// AnalystBias 分析师宏观偏向
type AnalystBias string

const (
	AnalystBiasBullish   AnalystBias = "bullish"
	AnalystBiasBearish   AnalystBias = "bearish"
	AnalystBiasNeutral   AnalystBias = "neutral"
	AnalystBiasStrongBull AnalystBias = "strong_bullish"
	AnalystBiasStrongBear AnalystBias = "strong_bearish"
)

// AgentAnalystReportDB 分析师报告表（黑板），P1-1 含可选 symbol
type AgentAnalystReportDB struct {
	ID         int64     `gorm:"primaryKey;autoIncrement"`
	TraderID   string    `gorm:"column:trader_id;not null;index:idx_agent_analyst_trader_time"`
	StrategyID string    `gorm:"column:strategy_id;default:'';index"`
	UserID     string    `gorm:"column:user_id;default:'';index"`
	Symbol     string    `gorm:"column:symbol;default:'';index"` // 可选，单币种分析时填写
	Bias       string    `gorm:"column:bias;not null"`          // bullish / bearish / neutral / strong_bullish / strong_bearish
	Confidence int       `gorm:"column:confidence;default:0"`   // 0-100
	ReportText string    `gorm:"column:report_text;type:text;default:''"`
	RawJSON    string    `gorm:"column:raw_json;type:text;default:''"`
	CreatedAt  time.Time `gorm:"column:created_at;not null;index:idx_agent_analyst_trader_time,sort:desc"`
}

func (AgentAnalystReportDB) TableName() string { return "agent_analyst_reports" }

// AgentAnalystReport 对外使用的分析师报告
type AgentAnalystReport struct {
	ID         int64       `json:"id"`
	TraderID   string      `json:"trader_id"`
	StrategyID string      `json:"strategy_id"`
	UserID     string      `json:"user_id"`
	Symbol     string      `json:"symbol,omitempty"` // 可选
	Bias       AnalystBias `json:"bias"`
	Confidence int         `json:"confidence"`
	ReportText string      `json:"report_text"`
	RawJSON    string      `json:"raw_json,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
}

// AgentBlackboardStore 多 Agent 黑板存储
type AgentBlackboardStore struct {
	db *gorm.DB
}

// NewAgentBlackboardStore 创建黑板存储
func NewAgentBlackboardStore(db *gorm.DB) *AgentBlackboardStore {
	return &AgentBlackboardStore{db: db}
}

func (s *AgentBlackboardStore) initTables() error {
	if s.db.Dialector.Name() == "postgres" {
		var tableExists int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'agent_analyst_reports'`).Scan(&tableExists)
		if tableExists > 0 {
			// 已有表：补可选列
			for _, q := range []string{
				`ALTER TABLE agent_analyst_reports ADD COLUMN IF NOT EXISTS strategy_id varchar(255) DEFAULT ''`,
				`ALTER TABLE agent_analyst_reports ADD COLUMN IF NOT EXISTS symbol varchar(255) DEFAULT ''`,
			} {
				if err := s.db.Exec(q).Error; err != nil {
					return fmt.Errorf("failed to migrate agent_analyst_reports: %w", err)
				}
			}
			return nil
		}
	}
	return s.db.AutoMigrate(&AgentAnalystReportDB{})
}

// WriteAnalystReport 写入分析师报告（写黑板）；symbol 可选，宏观报告传 ""
func (s *AgentBlackboardStore) WriteAnalystReport(traderID, strategyID, userID, symbol string, bias AnalystBias, confidence int, reportText, rawJSON string) (*AgentAnalystReport, error) {
	now := time.Now().UTC()
	db := &AgentAnalystReportDB{
		TraderID:   traderID,
		StrategyID: strategyID,
		UserID:     userID,
		Symbol:     symbol,
		Bias:       string(bias),
		Confidence: confidence,
		ReportText: reportText,
		RawJSON:    rawJSON,
		CreatedAt:  now,
	}
	if err := s.db.Create(db).Error; err != nil {
		return nil, err
	}
	return dbToReport(db), nil
}

// GetAnalystReportByID 按 ID 获取分析师报告（P4-3 Dashboard 单轮详情用）
func (s *AgentBlackboardStore) GetAnalystReportByID(id int64) (*AgentAnalystReport, error) {
	var db AgentAnalystReportDB
	if err := s.db.Where("id = ?", id).First(&db).Error; err != nil {
		return nil, err
	}
	return dbToReport(&db), nil
}

// GetLatestAnalystReport 获取指定 Trader 最新一条分析师报告（读黑板）
func (s *AgentBlackboardStore) GetLatestAnalystReport(traderID string) (*AgentAnalystReport, error) {
	var db AgentAnalystReportDB
	err := s.db.Where("trader_id = ?", traderID).Order("created_at DESC").Limit(1).First(&db).Error
	if err != nil {
		return nil, err
	}
	return dbToReport(&db), nil
}

// GetLatestAnalystReportForStrategy 获取指定 Trader+Strategy 最新一条报告
func (s *AgentBlackboardStore) GetLatestAnalystReportForStrategy(traderID, strategyID string) (*AgentAnalystReport, error) {
	var db AgentAnalystReportDB
	q := s.db.Where("trader_id = ?", traderID)
	if strategyID != "" {
		q = q.Where("strategy_id = ?", strategyID)
	}
	err := q.Order("created_at DESC").Limit(1).First(&db).Error
	if err != nil {
		return nil, err
	}
	return dbToReport(&db), nil
}

func dbToReport(db *AgentAnalystReportDB) *AgentAnalystReport {
	return &AgentAnalystReport{
		ID:         db.ID,
		TraderID:   db.TraderID,
		StrategyID: db.StrategyID,
		UserID:     db.UserID,
		Symbol:     db.Symbol,
		Bias:       AnalystBias(db.Bias),
		Confidence: db.Confidence,
		ReportText: db.ReportText,
		RawJSON:    db.RawJSON,
		CreatedAt:  db.CreatedAt,
	}
}
