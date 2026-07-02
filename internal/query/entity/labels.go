package entity

import "strings"

func LooksLikeBusinessDimensionLabel(value string) bool {
	normalized := normalizeEntityText(value)
	if normalized == "" {
		return false
	}
	labels := []string{
		"客户", "供应商", "项目", "合同", "协议",
		"口径", "维度",
		"主体", "公司", "单位", "对象", "对方", "合作方",
		"甲方", "乙方", "开票方", "购买方", "销售方", "收款方", "付款方",
		"明细", "列表", "汇总", "统计", "情况",
	}
	for _, label := range labels {
		if normalized == normalizeEntityText(label) {
			return true
		}
	}
	return false
}

func normalizeEntityText(s string) string {
	replacer := strings.NewReplacer(" ", "", "\t", "", "\n", "", "（", "", "）", "", "(", "", ")", "", "-", "", "_", "", ",", "", "，", "", ".", "", "。", "")
	return replacer.Replace(strings.TrimSpace(s))
}
