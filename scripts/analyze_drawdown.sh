#!/bin/bash
# 历史回撤触发分析脚本
# 功能：分析交易系统的回撤触发记录，生成统计报告
# 用法：./scripts/analyze_drawdown.sh [日志目录] [输出文件]

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 显示帮助信息
show_help() {
    cat << EOF
历史回撤触发分析脚本

用法:
    $0 [选项] [日志目录] [输出文件]

选项:
    -h, --help      显示此帮助信息

参数:
    日志目录        日志文件所在目录（默认: data）
    输出文件        可选，如果指定则保存报告到文件

示例:
    $0                          # 分析 data 目录，输出到终端
    $0 data                     # 分析 data 目录，输出到终端
    $0 data report.txt          # 分析 data 目录，保存到 report.txt
    $0 data reports/drawdown_$(date +%Y%m%d).txt  # 保存到带日期的文件

EOF
}

# 解析参数
if [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
    show_help
    exit 0
fi

# 默认参数
LOG_DIR="${1:-data}"
OUTPUT_FILE="${2:-}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
LOG_DIR_ABS="$PROJECT_DIR/$LOG_DIR"

# 检查日志目录
if [ ! -d "$LOG_DIR_ABS" ]; then
    echo -e "${RED}错误: 日志目录不存在: $LOG_DIR_ABS${NC}" >&2
    exit 1
fi

# 临时文件
TEMP_FILE=$(mktemp /tmp/drawdown_analysis.XXXXXX)
trap "rm -f $TEMP_FILE" EXIT

# 查找所有触发记录
TRIGGER_PATTERN="🚨 Drawdown close position condition triggered"
SUCCESS_PATTERN="✅ Drawdown close position succeeded"
FAILED_PATTERN="❌ Drawdown close position failed"

echo "正在分析日志文件..."
grep -h "$TRIGGER_PATTERN" "$LOG_DIR_ABS"/*.log > "$TEMP_FILE" 2>/dev/null || true

TOTAL_TRIGGERS=$(wc -l < "$TEMP_FILE" | tr -d ' ')

if [ "$TOTAL_TRIGGERS" -eq 0 ]; then
    echo -e "${YELLOW}未找到回撤触发记录${NC}"
    exit 0
fi

# 输出函数
output() {
    if [ -n "$OUTPUT_FILE" ]; then
        echo "$1" >> "$OUTPUT_FILE"
    else
        echo "$1"
    fi
}

# 生成报告
generate_report() {
    output "=========================================="
    output "  历史回撤触发分析报告"
    output "=========================================="
    output ""
    output "生成时间: $(date '+%Y-%m-%d %H:%M:%S')"
    output "日志目录: $LOG_DIR_ABS"
    output "分析记录数: $TOTAL_TRIGGERS"
    output ""
    
    # 1. 总体统计
    output "【1. 总体统计】"
    output "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    output "总触发次数: $TOTAL_TRIGGERS"
    
    SUCCESS_COUNT=$(grep -h "$SUCCESS_PATTERN" "$LOG_DIR_ABS"/*.log 2>/dev/null | wc -l | tr -d ' ')
    FAILED_COUNT=$(grep -h "$FAILED_PATTERN" "$LOG_DIR_ABS"/*.log 2>/dev/null | wc -l | tr -d ' ')
    
    if [ "$SUCCESS_COUNT" -gt 0 ] || [ "$FAILED_COUNT" -gt 0 ]; then
        TOTAL_EXEC=$((SUCCESS_COUNT + FAILED_COUNT))
        if [ "$TOTAL_EXEC" -gt 0 ]; then
            SUCCESS_RATE=$(echo "scale=2; $SUCCESS_COUNT * 100 / $TOTAL_EXEC" | bc)
            output "平仓成功: $SUCCESS_COUNT 次"
            output "平仓失败: $FAILED_COUNT 次"
            output "成功率: ${SUCCESS_RATE}%"
        fi
    fi
    output ""
    
    # 2. 按交易对统计
    output "【2. 按交易对统计】"
    output "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    grep -oE "triggered: [A-Z0-9]+ [a-z]+" "$TEMP_FILE" | \
        sed 's/triggered: //' | \
        awk '{print $1}' | \
        sort | uniq -c | sort -rn | \
        awk '{printf "  %-20s: %3d 次 (%.1f%%)\n", $2, $1, ($1/'$TOTAL_TRIGGERS')*100}' | \
        while IFS= read -r line; do output "$line"; done
    output ""
    
    # 3. 按日期统计
    output "【3. 按日期统计】"
    output "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    grep -h "$TRIGGER_PATTERN" "$LOG_DIR_ABS"/*.log 2>/dev/null | \
        sed -E 's|.*nofx_([0-9]{4}-[0-9]{2}-[0-9]{2})\.log.*|\1|' | \
        sort | uniq -c | sort -k2 | \
        awk '{printf "  %s: %3d 次\n", $2, $1}' | \
        while IFS= read -r line; do output "$line"; done
    output ""
    
    # 4. 回撤幅度分析
    output "【4. 回撤幅度分析】"
    output "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    DRAWDOWN_STATS=$(grep -oE "Drawdown: [0-9.]+%" "$TEMP_FILE" | \
        sed 's/Drawdown: //' | sed 's/%//' | \
        awk '{
            sum+=$1; count++; 
            if ($1 > max || max == 0) max=$1;
            if ($1 < min || min == 0) min=$1;
            if ($1 < 40) c1++;
            else if ($1 < 50) c2++;
            else if ($1 < 60) c3++;
            else c4++;
        }
        END {
            printf "%.2f %.2f %.2f %.2f %d %d %d %d %d\n", 
                sum/count, max, min, sum, c1, c2, c3, c4, count
        }')
    
    AVG_DD=$(echo "$DRAWDOWN_STATS" | awk '{print $1}')
    MAX_DD=$(echo "$DRAWDOWN_STATS" | awk '{print $2}')
    MIN_DD=$(echo "$DRAWDOWN_STATS" | awk '{print $3}')
    C1=$(echo "$DRAWDOWN_STATS" | awk '{print $5}')
    C2=$(echo "$DRAWDOWN_STATS" | awk '{print $6}')
    C3=$(echo "$DRAWDOWN_STATS" | awk '{print $7}')
    C4=$(echo "$DRAWDOWN_STATS" | awk '{print $8}')
    TOTAL=$(echo "$DRAWDOWN_STATS" | awk '{print $9}')
    
    output "平均回撤: ${AVG_DD}%"
    output "最高回撤: ${MAX_DD}%"
    output "最低回撤: ${MIN_DD}%"
    output ""
    output "回撤分布:"
    output "  <40%:  $C1 次 ($(echo "scale=1; $C1 * 100 / $TOTAL" | bc)%)"
    output "  40-50%: $C2 次 ($(echo "scale=1; $C2 * 100 / $TOTAL" | bc)%)"
    output "  50-60%: $C3 次 ($(echo "scale=1; $C3 * 100 / $TOTAL" | bc)%)"
    output "  >=60%: $C4 次 ($(echo "scale=1; $C4 * 100 / $TOTAL" | bc)%)"
    output ""
    
    # 5. 峰值利润分析
    output "【5. 峰值利润分析】"
    output "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    PEAK_STATS=$(grep -oE "Peak profit: [0-9.]+%" "$TEMP_FILE" | \
        sed 's/Peak profit: //' | sed 's/%//' | \
        awk '{
            sum+=$1; count++; 
            if ($1 > max || max == 0) max=$1;
            if ($1 < min || min == 0) min=$1;
        }
        END {
            printf "%.2f %.2f %.2f\n", sum/count, max, min
        }')
    
    AVG_PEAK=$(echo "$PEAK_STATS" | awk '{print $1}')
    MAX_PEAK=$(echo "$PEAK_STATS" | awk '{print $2}')
    MIN_PEAK=$(echo "$PEAK_STATS" | awk '{print $3}')
    
    output "平均峰值利润: ${AVG_PEAK}%"
    output "最高峰值利润: ${MAX_PEAK}%"
    output "最低峰值利润: ${MIN_PEAK}%"
    output ""
    
    # 6. 触发时利润分析
    output "【6. 触发时利润分析】"
    output "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    PROFIT_STATS=$(grep -oE "(Current profit|Real profit): [0-9.]+%" "$TEMP_FILE" | \
        sed 's/.*profit: //' | sed 's/%//' | \
        awk '{
            sum+=$1; count++; 
            if ($1 > max || max == 0) max=$1;
            if ($1 < min || min == 0) min=$1;
        }
        END {
            printf "%.2f %.2f %.2f\n", sum/count, max, min
        }')
    
    AVG_PROFIT=$(echo "$PROFIT_STATS" | awk '{print $1}')
    MAX_PROFIT=$(echo "$PROFIT_STATS" | awk '{print $2}')
    MIN_PROFIT=$(echo "$PROFIT_STATS" | awk '{print $3}')
    
    output "平均触发利润: ${AVG_PROFIT}%"
    output "最高触发利润: ${MAX_PROFIT}%"
    output "最低触发利润: ${MIN_PROFIT}%"
    output ""
    
    # 7. 最新触发记录
    output "【7. 最新触发记录（最近5次）】"
    output "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    tail -5 "$TEMP_FILE" | \
        sed -E 's|.*nofx_([0-9]{4}-[0-9]{2}-[0-9]{2})\.log:([0-9]{2}:[0-9]{2}:[0-9]{2}).*triggered: |\1 \2 |' | \
        awk '{printf "  %s %s %s\n", $1, $2, substr($0, index($0,$3))}' | \
        while IFS= read -r line; do output "$line"; done
    output ""
    
    # 8. 高回撤记录（>=60%）
    output "【8. 高回撤记录（>=60%）】"
    output "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    HIGH_DD_COUNT=0
    while IFS= read -r line; do
        DRAWDOWN=$(echo "$line" | grep -oE "Drawdown: [0-9.]+%" | sed 's/Drawdown: //' | sed 's/%//')
        if [ -n "$DRAWDOWN" ]; then
            if (( $(echo "$DRAWDOWN >= 60" | bc -l) )); then
                HIGH_DD_COUNT=$((HIGH_DD_COUNT + 1))
                SYMBOL=$(echo "$line" | grep -oE "triggered: [A-Z0-9]+" | sed 's/triggered: //')
                DATE_TIME=$(echo "$line" | sed -E 's|.*nofx_([0-9]{4}-[0-9]{2}-[0-9]{2})\.log:([0-9]{2}:[0-9]{2}:[0-9]{2}).*|\1 \2|')
                PEAK=$(echo "$line" | grep -oE "Peak profit: [0-9.]+%" | sed 's/Peak profit: //')
                PROFIT=$(echo "$line" | grep -oE "(Current profit|Real profit): [0-9.]+%" | sed 's/.*profit: //')
                output "  $DATE_TIME - $SYMBOL | 峰值: $PEAK | 触发: $PROFIT | 回撤: ${DRAWDOWN}%"
            fi
        fi
    done < "$TEMP_FILE"
    
    if [ "$HIGH_DD_COUNT" -eq 0 ]; then
        output "  无高回撤记录"
    fi
    output ""
    
    # 9. 阈值分析（新版本日志）
    output "【9. 阈值分析（新版本）】"
    output "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    THRESHOLD_COUNT=$(grep -c "threshold:" "$TEMP_FILE" || echo "0")
    if [ "$THRESHOLD_COUNT" -gt 0 ]; then
        output "新版本记录数: $THRESHOLD_COUNT"
        grep "threshold:" "$TEMP_FILE" | \
            grep -oE "threshold: [0-9.]+%" | \
            sed 's/threshold: //' | sed 's/%//' | \
            sort | uniq -c | sort -rn | \
            awk '{printf "  阈值 %s%%: %d 次\n", $2, $1}' | \
            while IFS= read -r line; do output "$line"; done
    else
        output "  无新版本记录（所有记录均为旧版本）"
    fi
    output ""
    
    # 10. 关键发现
    output "【10. 关键发现与建议】"
    output "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    
    if [ "$SUCCESS_COUNT" -gt 0 ] && [ "$FAILED_COUNT" -eq 0 ]; then
        output "✅ 平仓成功率 100%，系统稳定可靠"
    fi
    
    if (( $(echo "$AVG_DD < 50" | bc -l) )); then
        output "✅ 平均回撤 ${AVG_DD}%，阈值设置合理"
    else
        output "⚠️  平均回撤 ${AVG_DD}%，建议考虑收紧阈值"
    fi
    
    if [ "$C4" -gt 0 ]; then
        output "⚠️  发现 $C4 次高回撤（>=60%），建议关注"
    fi
    
    if (( $(echo "$AVG_PEAK > 15" | bc -l) )); then
        output "✅ 平均峰值利润 ${AVG_PEAK}%，系统能够捕获高利润"
    fi
    
    output ""
    output "=========================================="
    output "报告生成完成"
    output "=========================================="
}

# 主函数
main() {
    if [ -n "$OUTPUT_FILE" ]; then
        OUTPUT_FILE_ABS="$PROJECT_DIR/$OUTPUT_FILE"
        OUTPUT_DIR=$(dirname "$OUTPUT_FILE_ABS")
        # 创建输出目录（如果不存在）
        if [ ! -d "$OUTPUT_DIR" ]; then
            mkdir -p "$OUTPUT_DIR"
        fi
        echo "正在生成报告到: $OUTPUT_FILE_ABS"
        generate_report
        echo -e "${GREEN}报告已保存到: $OUTPUT_FILE_ABS${NC}"
    else
        generate_report
    fi
}

# 显示帮助信息
show_help() {
    cat << EOF
历史回撤触发分析脚本

用法:
    $0 [选项] [日志目录] [输出文件]

选项:
    -h, --help      显示此帮助信息

参数:
    日志目录        日志文件所在目录（默认: data）
    输出文件        可选，如果指定则保存报告到文件

示例:
    $0                          # 分析 data 目录，输出到终端
    $0 data                     # 分析 data 目录，输出到终端
    $0 data report.txt          # 分析 data 目录，保存到 report.txt
    $0 data reports/drawdown_$(date +%Y%m%d).txt  # 保存到带日期的文件

EOF
}

# 解析参数
if [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
    show_help
    exit 0
fi

# 执行主函数
main
