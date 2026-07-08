package query

import (
	"regexp"
	"strings"
	"time"
)

type businessPeriodAnchorResolution struct {
	Anchor   time.Time
	Advanced bool
	Reason   string
}

func (e *Engine) periodParserAnchorForQuestion(question string, anchor time.Time) time.Time {
	return e.resolveBusinessPeriodAnchor(question, anchor).Anchor
}

func (e *Engine) resolveBusinessPeriodAnchor(question string, anchor time.Time) businessPeriodAnchorResolution {
	if !e.asOfAnchor.IsZero() || anchor.IsZero() {
		return businessPeriodAnchorResolution{Anchor: anchor}
	}
	reason := businessPeriodCompleteWindowReason(question, anchor)
	if reason != "" {
		return businessPeriodAnchorResolution{
			Anchor:   anchor.AddDate(0, 1, 0),
			Advanced: true,
			Reason:   reason,
		}
	}
	return businessPeriodAnchorResolution{Anchor: anchor}
}

func shouldAdvanceDataMonthAnchorForCompleteWindow(question string, anchor time.Time) bool {
	return businessPeriodCompleteWindowReason(question, anchor) != ""
}

func businessPeriodCompleteWindowReason(question string, anchor time.Time) string {
	q := strings.TrimSpace(question)
	if q == "" {
		return ""
	}
	if hasExplicitStartToCurrentCompleteWindow(q) {
		return "explicit_start_to_current_complete_window"
	}
	if hasPreviousCompleteMonthToken(q) {
		return "previous_complete_month_token"
	}
	if looseYearRangeEndsAtYear(q, anchor.Year()) {
		return "loose_year_range_current_year"
	}
	return ""
}

func hasExplicitStartToCurrentCompleteWindow(q string) bool {
	return regexp.MustCompile(`(?:从|自)?\s*(20\d{2}|\d{2}|今年|本年|去年)\s*年?\s*([0-1]?\d|[一二三四五六七八九十两]{1,3})月?(?:底|末)?\s*(?:起|开始)?\s*(?:到|至|截至|截止)?\s*(?:至今|现在|目前)`).MatchString(q)
}

func hasPreviousCompleteMonthToken(q string) bool {
	return containsAny(q, []string{
		"上一个完整自然月", "上个完整自然月",
		"上一个完整月", "上个完整月",
		"最新完整月份", "最新完整月", "最近完整月份", "最近完整月",
		"上个月", "上月",
	})
}
