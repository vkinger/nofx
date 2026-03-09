-- PostgreSQL: Agent 模式兼容迁移（多 Agent / 风控官）
-- 用于已有库的兼容；应用启动时各 store 的 initTables 也会自动执行等效的 ADD COLUMN IF NOT EXISTS。
-- 执行: psql -U user -d nofx -f scripts/pg_traders_agent_columns.sql
-- 若某表尚未创建（如从未用过 Agent），可注释掉该表对应的 ALTER 或先启动应用再执行本脚本。

-- traders: 多 Agent / 风控官开关与专用模型
ALTER TABLE traders ADD COLUMN IF NOT EXISTS use_analyst_flow boolean DEFAULT false;
ALTER TABLE traders ADD COLUMN IF NOT EXISTS use_compliance_flow boolean DEFAULT false;
ALTER TABLE traders ADD COLUMN IF NOT EXISTS analyst_model_id varchar(255) DEFAULT '';
ALTER TABLE traders ADD COLUMN IF NOT EXISTS compliance_model_id varchar(255) DEFAULT '';

-- decision_records: 关联分析师报告与风控审计
ALTER TABLE decision_records ADD COLUMN IF NOT EXISTS analyst_report_id bigint DEFAULT 0;
ALTER TABLE decision_records ADD COLUMN IF NOT EXISTS pending_decision_id bigint DEFAULT 0;
ALTER TABLE decision_records ADD COLUMN IF NOT EXISTS compliance_audit_id bigint DEFAULT 0;

-- agent_analyst_reports: 可选 strategy_id / symbol；分析师 系统/用户 提示词（单轮详情展示）
ALTER TABLE agent_analyst_reports ADD COLUMN IF NOT EXISTS strategy_id varchar(255) DEFAULT '';
ALTER TABLE agent_analyst_reports ADD COLUMN IF NOT EXISTS symbol varchar(255) DEFAULT '';
ALTER TABLE agent_analyst_reports ADD COLUMN IF NOT EXISTS system_prompt text DEFAULT '';
ALTER TABLE agent_analyst_reports ADD COLUMN IF NOT EXISTS user_prompt text DEFAULT '';

-- agent_compliance_audits: 风控官 系统/用户 提示词（单轮详情展示）
ALTER TABLE agent_compliance_audits ADD COLUMN IF NOT EXISTS system_prompt text DEFAULT '';
ALTER TABLE agent_compliance_audits ADD COLUMN IF NOT EXISTS user_prompt text DEFAULT '';

-- agent_pending_decisions: 策略与分析师报告关联
ALTER TABLE agent_pending_decisions ADD COLUMN IF NOT EXISTS strategy_id varchar(255) DEFAULT '';
ALTER TABLE agent_pending_decisions ADD COLUMN IF NOT EXISTS analyst_report_id bigint DEFAULT 0;
