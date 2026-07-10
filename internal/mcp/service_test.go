package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestServiceRejectsFinanceQueryWithoutQuery(t *testing.T) {
	t.Parallel()

	svc := NewService(ServiceConfig{
		DBPath:  "unused",
		Company: "测试公司",
	})
	_, err := svc.RunTool(context.Background(), "finance-query", map[string]any{})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != -32602 {
		t.Fatalf("RunTool error = %#v, want -32602 ToolError", err)
	}
}

func TestServiceRejectsUnknownTool(t *testing.T) {
	t.Parallel()

	svc := NewService(ServiceConfig{})
	_, err := svc.RunTool(context.Background(), "missing-tool", map[string]any{})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != -32602 {
		t.Fatalf("RunTool error = %#v, want -32602 ToolError", err)
	}
}

func TestEffectiveFinanceQueryProtectsDynamicPeriodAndKeepsEntityHints(t *testing.T) {
	t.Parallel()

	raw := "从项目口径看，上个完整自然月百度这个客户还有多少没收回来？"
	rewritten := "2026年6月 百度在线网络技术(北京)有限公司 项目应收未收"

	got := effectiveFinanceQuery(rewritten, raw)

	for _, want := range []string{"上个完整自然月", "项目口径", "百度在线网络技术(北京)有限公司"} {
		if !strings.Contains(got, want) {
			t.Fatalf("effectiveFinanceQuery should preserve %q, got %q", want, got)
		}
	}
	for _, forbidden := range []string{"2026年6月", "2026-06"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("effectiveFinanceQuery should remove rewritten fixed period %q, got %q", forbidden, got)
		}
	}
}

func TestEffectiveFinanceQueryProtectsCompanyScopeAndContinuationSemantics(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		raw       string
		rewritten string
		want      []string
		forbid    []string
	}{
		{
			name:      "company_project_roster",
			raw:       "25年到26年未付款的项目都有哪些，对应的金额是多少？",
			rewritten: "2025年到2026年未付款的项目明细，包括项目名称和对应金额",
			want:      []string{"25年到26年", "都有哪些", "金额"},
			forbid:    []string{"补充识别：", "包括项目名称"},
		},
		{
			name:      "income_table_source",
			raw:       "帮我看下收入表中最新月份的营收数据。",
			rewritten: "最新月份营收数据",
			want:      []string{"收入表", "最新月份", "营收"},
		},
		{
			name:      "non_semantic_continuation",
			raw:       "最近完整月份净利润是多少？",
			rewritten: "再算一次",
			want:      []string{"最近完整月份", "净利润"},
			forbid:    []string{"再算一次", "补充识别："},
		},
		{
			name:      "specific_entity_hint_control",
			raw:       "上个完整自然月百度这个客户还有多少没收回来？",
			rewritten: "百度在线网络技术(北京)有限公司 项目应收未收",
			want:      []string{"上个完整自然月", "百度在线网络技术(北京)有限公司"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := effectiveFinanceQuery(tt.rewritten, tt.raw)

			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("effectiveFinanceQuery should preserve %q, got %q", want, got)
				}
			}
			for _, forbidden := range tt.forbid {
				if strings.Contains(got, forbidden) {
					t.Errorf("effectiveFinanceQuery should remove %q, got %q", forbidden, got)
				}
			}
		})
	}
}

func TestEffectiveFinanceQueryProtectsReconciliationDifferenceIntent(t *testing.T) {
	t.Parallel()

	raw := "上个完整自然月银行净流入和账上净利润差多少？"
	rewritten := "上个完整自然月银行净流入和账上净利润"

	got := effectiveFinanceQuery(rewritten, raw)

	for _, want := range []string{"上个完整自然月", "银行净流入", "账上净利润", "差多少"} {
		if !strings.Contains(got, want) {
			t.Fatalf("effectiveFinanceQuery should preserve %q, got %q", want, got)
		}
	}
}

func TestEffectiveFinanceQueryDropsInjectedAbsoluteMonthInsideRelativePeriod(t *testing.T) {
	t.Parallel()

	raw := "老板，上个完整自然月，银行净流入和账上净利润的差异是多少？"
	rewritten := "上个完整自然月（2026年6月）银行净流入和账上净利润的差异"

	got := effectiveFinanceQuery(rewritten, raw)

	for _, want := range []string{"上个完整自然月", "银行净流入", "账上净利润", "差异"} {
		if !strings.Contains(got, want) {
			t.Fatalf("effectiveFinanceQuery should preserve %q, got %q", want, got)
		}
	}
	for _, forbidden := range []string{"2026年6月", "2026-06"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("effectiveFinanceQuery should drop injected fixed month %q, got %q", forbidden, got)
		}
	}
}

func TestEffectiveFinanceQueryProtectsSpecificContractSubject(t *testing.T) {
	t.Parallel()

	raw := "按合同行业商品数据采购合同-A02来看，从2025年10月到上个完整自然月月底，项目应收未收合计是多少？"
	rewritten := "采购合同-A02 2025年10月到上个完整自然月月底 项目应收未收合计"

	got := effectiveFinanceQuery(rewritten, raw)

	for _, want := range []string{"行业商品数据采购合同-A02", "上个完整自然月", "项目应收未收"} {
		if !strings.Contains(got, want) {
			t.Fatalf("effectiveFinanceQuery should preserve %q, got %q", want, got)
		}
	}
	if !strings.Contains(got, "补充识别") || !strings.Contains(got, "采购合同-A02") {
		t.Fatalf("effectiveFinanceQuery should keep rewritten hints as supplemental context, got %q", got)
	}
}

func TestEffectiveFinanceQueryExtractsWrappedOriginalQuestion(t *testing.T) {
	t.Parallel()

	raw := `[巡检要求]
这是一条只读巡检请求。回答前必须先调用 finance-query 获取最新事实。

[用户原问题]
按项目应收口径，2025年10月到上个完整自然月月底未回款合计多少？`
	rewritten := "按项目应收口径，2025年10月到2026年6月底未回款合计多少？"

	got := effectiveFinanceQuery(rewritten, raw)

	for _, want := range []string{"按项目应收口径", "上个完整自然月", "未回款合计"} {
		if !strings.Contains(got, want) {
			t.Fatalf("effectiveFinanceQuery should preserve original question term %q, got %q", want, got)
		}
	}
	for _, forbidden := range []string{"巡检要求", "只读巡检请求", "2026年6月", "2026-06"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("effectiveFinanceQuery should remove wrapper/fixed period %q, got %q", forbidden, got)
		}
	}
}

func TestEffectiveFinanceQueryKeepsDynamicPeriodForSourceSpecificResolution(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		raw       string
		rewritten string
		want      []string
		forbid    []string
	}{
		{
			name:      "bank_flow_latest_available_month",
			raw:       "银行卡上，上个完整自然月净现金流是多少？",
			rewritten: "2026年6月银行流水净现金流",
			want:      []string{"银行卡", "上个完整自然月", "净现金流"},
			forbid:    []string{"2026年6月", "2026-06"},
		},
		{
			name:      "project_income_latest_project_month",
			raw:       "收入表中上个完整自然月项目结算营收是多少？",
			rewritten: "2026年6月收入表项目结算营收",
			want:      []string{"收入表", "上个完整自然月", "项目结算营收"},
			forbid:    []string{"2026年6月", "2026-06"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := effectiveFinanceQuery(tt.rewritten, tt.raw)

			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("effectiveFinanceQuery should preserve %q, got %q", want, got)
				}
			}
			for _, forbidden := range tt.forbid {
				if strings.Contains(got, forbidden) {
					t.Fatalf("effectiveFinanceQuery should remove rewritten fixed period %q, got %q", forbidden, got)
				}
			}
		})
	}
}

func TestEffectiveFinanceQueryProtectsExplicitUserPeriod(t *testing.T) {
	t.Parallel()

	raw := "2026年6月银行卡净现金流是多少？"
	rewritten := "最近完整月份 银行卡净现金流"

	got := effectiveFinanceQuery(rewritten, raw)

	if !strings.Contains(got, "2026年6月") {
		t.Fatalf("effectiveFinanceQuery should keep explicit user period, got %q", got)
	}
	if strings.Contains(got, "最近完整月份") {
		t.Fatalf("effectiveFinanceQuery should not replace explicit user period with dynamic period, got %q", got)
	}
}
