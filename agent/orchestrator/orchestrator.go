// Package orchestrator runs the multi-agent flow: Analyst → Blackboard → Trader (with analyst report in prompt).
// Original single-model flow remains unchanged; this package provides an alternative entry that uses the analyst first.
package orchestrator

import (
	"fmt"

	"nofx/kernel"
	"nofx/logger"
	"nofx/mcp"
	"nofx/store"

	"nofx/agent/analyst"
)

// RunOneRound 执行一轮多 Agent 流程：1）运行分析师写黑板 2）用分析师报告注入 prompt 后跑交易员决策
// 返回分析师报告、本轮关联的分析师报告 ID（用于 P1-6 写入 decision_records.analyst_report_id）、完整决策、错误。调用方负责执行 decisions。
func RunOneRound(
	ctx *kernel.Context,
	engine *kernel.StrategyEngine,
	analystClient mcp.AIClient,
	traderClient mcp.AIClient,
	st *store.Store,
	traderID, strategyID, userID string,
) (analystReport *analyst.AnalystReport, analystReportID int64, fullDecision *kernel.FullDecision, err error) {
	if st == nil {
		return nil, 0, nil, fmt.Errorf("orchestrator: store is required")
	}

	// 1. Run analyst (P4-4 阶段日志)
	logger.Infof("[Phase] analyst_start trader_id=%s", traderID)
	logger.Infof("[Orchestrator] Running analyst...")
	analystReport, err = analyst.RunAnalyst(ctx, engine, analystClient)
	if err != nil {
		logger.Infof("[Phase] analyst_end error=%v", err)
		return nil, 0, nil, fmt.Errorf("analyst failed: %w", err)
	}
	logger.Infof("[Phase] analyst_end trader_id=%s bias=%s confidence=%d", traderID, analystReport.Bias, analystReport.Confidence)

	// 2. Write to blackboard
	bb := st.AgentBlackboard()
	written, writeErr := bb.WriteAnalystReport(traderID, strategyID, userID, "", store.AnalystBias(analystReport.Bias), analystReport.Confidence, analystReport.ReportText, analystReport.Raw)
	if writeErr != nil {
		logger.Warnf("[Orchestrator] Failed to write analyst report to blackboard: %v", writeErr)
	} else {
		logger.Infof("[Orchestrator] Analyst report written to blackboard (bias=%s, confidence=%d)", analystReport.Bias, analystReport.Confidence)
	}

	// 3. Build analyst suffix for trader prompt: prefer reading from blackboard (P1-4), fallback to in-memory; capture report ID for P1-6
	var analystSuffix string
	if latest, getErr := bb.GetLatestAnalystReport(traderID); getErr == nil && latest != nil {
		analystSuffix = fmt.Sprintf("Bias: %s | Confidence: %d\n%s", latest.Bias, latest.Confidence, latest.ReportText)
		analystReportID = latest.ID
	} else {
		analystSuffix = fmt.Sprintf("Bias: %s | Confidence: %d\n%s", analystReport.Bias, analystReport.Confidence, analystReport.ReportText)
		if written != nil {
			analystReportID = written.ID
		}
	}

	// 4. Run trader with analyst report in prompt (P4-4 trader 阶段在 auto_trader 内打日志)
	logger.Infof("[Orchestrator] Running trader with analyst report...")
	fullDecision, err = kernel.GetFullDecisionWithStrategy(ctx, traderClient, engine, "balanced", analystSuffix)
	if err != nil {
		return analystReport, analystReportID, nil, fmt.Errorf("trader decision failed: %w", err)
	}
	return analystReport, analystReportID, fullDecision, nil
}
