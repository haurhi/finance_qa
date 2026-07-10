package query

import "testing"

func TestIsRealishQueryEntityRejectsSyntheticTemporalAndGenericFragments(t *testing.T) {
	cases := []struct {
		name   string
		entity string
		want   bool
	}{
		{name: "real business entity", entity: "飞未云科", want: true},
		{name: "empty", entity: "", want: false},
		{name: "generic metric", entity: "收入", want: false},
		{name: "temporal fragment", entity: "Q1", want: false},
		{name: "loose two digit year range", entity: "25年到26年", want: false},
		{name: "loose four digit year range", entity: "2025年至2026年", want: false},
		{name: "synthetic question fragment", entity: "单笔最大流入来自谁", want: false},
		{name: "invoice payment state fragment", entity: "已开票未", want: false},
		{name: "short invoice payment state fragment", entity: "已开票未付", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRealishQueryEntity(tc.entity); got != tc.want {
				t.Fatalf("isRealishQueryEntity(%q) = %t, want %t", tc.entity, got, tc.want)
			}
		})
	}
}

func TestIsRealishQueryEntityRejectsStructuralQuestionFragments(t *testing.T) {
	for _, fragment := range []string{"包括", "包含", "期间", "每个", "对应"} {
		t.Run(fragment, func(t *testing.T) {
			if got := isRealishQueryEntity(fragment); got {
				t.Fatalf("isRealishQueryEntity(%q) = true, want false", fragment)
			}
		})
	}
}

func TestExtractNamedEntityRejectsCompanyScopeInvoicePaymentQuestion(t *testing.T) {
	if got := extractNamedEntityFromQuestion("2026年3月已开票未付款的合同有哪些"); got != "" {
		t.Fatalf("extractNamedEntityFromQuestion() = %q, want empty company-scope entity", got)
	}
}

func TestExtractNamedEntityRejectsLooseYearRangeProjectRosterQuestion(t *testing.T) {
	if got := extractNamedEntityFromQuestion("25年到26年有哪些项目还有未付款？"); got != "" {
		t.Fatalf("extractNamedEntityFromQuestion() = %q, want empty company-scope entity", got)
	}
}

func TestEntityAppearsInQuestionTextSupportsCoordinatedShortAliases(t *testing.T) {
	question := "百度和阿里2025年Q4未付款项目明细"
	for _, entity := range []string{
		"百度在线网络技术(北京)有限公司",
		"阿里云计算有限公司",
	} {
		if !entityAppearsInQuestionText(question, entity) {
			t.Fatalf("entityAppearsInQuestionText(%q, %q) = false, want true", question, entity)
		}
	}
}
