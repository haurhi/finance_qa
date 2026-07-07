package period

import (
	"regexp"
	"strings"
	"time"
)

// ExtractPeriodWithNow 从自然语言提取账期。
func ExtractPeriodWithNow(question string, anchor time.Time) (string, string) {
	year := anchor.Year()
	anchorMonth := int(anchor.Month())
	q := strings.TrimSpace(question)

	if from, to, ok := extractFullYearRange(q, year); ok {
		return from, to
	}
	if from, to, ok := extractLooseYearRange(q, anchor); ok {
		return from, to
	}
	if from, to, ok := extractHalfYearRange(q, year, anchorMonth); ok {
		return from, to
	}
	if from, to, ok := extractQuarterRange(q, year, anchorMonth); ok {
		return from, to
	}
	if from, to, ok := extractExplicitMonthRange(q, year); ok {
		return from, to
	}
	if from, to, ok := extractExplicitStartToRelativeMonthEndRange(q, anchor); ok {
		return from, to
	}
	if from, to, ok := extractExplicitStartToCurrentCompleteRange(q, anchor); ok {
		return from, to
	}
	if from, to, ok := extractRelativeYearMonthRange(q, year); ok {
		return from, to
	}
	if from, to, ok := extractYearCumulativeRange(q, year, anchorMonth); ok {
		return from, to
	}
	if from, to, ok := extractExplicitYearMonthRange(q); ok {
		return from, to
	}
	if from, to, ok := extractRelativeNamedRange(q, anchor); ok {
		return from, to
	}
	if from, to, ok := extractAdjacentMonthRange(q, year, anchorMonth); ok {
		return from, to
	}
	if from, to, ok := extractRelativeMonthRange(q, year, anchorMonth); ok {
		return from, to
	}
	if from, to, ok := extractImplicitCumulativeRange(q, year, anchorMonth); ok {
		return from, to
	}

	period := anchor.Format("2006-01")
	return period, period
}

func extractFullYearRange(q string, anchorYear int) (string, string, bool) {
	fullYearRe := regexp.MustCompile(`(20\d{2})年\s*(?:全年|整年|全年度|年度)`)
	if m := fullYearRe.FindStringSubmatch(q); len(m) == 2 {
		y := mustAtoi(m[1])
		return formatPeriodValue(y, 1), formatPeriodValue(y, 12), true
	}
	if strings.Contains(q, "今年全年") || strings.Contains(q, "本年全年") {
		return formatPeriodValue(anchorYear, 1), formatPeriodValue(anchorYear, 12), true
	}
	return "", "", false
}

func extractLooseYearRange(q string, anchor time.Time) (string, string, bool) {
	re := regexp.MustCompile(`(?i)(20\d{2}|\d{2})\s*年?\s*(?:到|至|-|~)\s*(20\d{2}|\d{2})\s*年`)
	m := re.FindStringSubmatch(q)
	if len(m) != 3 {
		return "", "", false
	}
	startYear := normalizeYearToken(m[1])
	endYear := normalizeYearToken(m[2])
	if startYear == 0 || endYear == 0 || startYear > endYear {
		return "", "", false
	}
	endMonth := 12
	if endYear == anchor.Year() {
		endMonth = int(anchor.Month()) - 1
		if endMonth < 1 {
			endMonth = 1
		}
	}
	return formatPeriodValue(startYear, 1), formatPeriodValue(endYear, endMonth), true
}

func extractHalfYearRange(q string, anchorYear, anchorMonth int) (string, string, bool) {
	explicitHalfRe := regexp.MustCompile(`(20\d{2})年\s*(上半年|下半年)`)
	if m := explicitHalfRe.FindStringSubmatch(q); len(m) == 3 {
		y := mustAtoi(m[1])
		if strings.Contains(m[2], "上") {
			from, to := halfRange(y, 1)
			return from, to, true
		}
		from, to := halfRange(y, 2)
		return from, to, true
	}
	if strings.Contains(q, "上半年") || strings.Contains(q, "下半年") {
		target := "上半年"
		if strings.Contains(q, "下半年") {
			target = "下半年"
		}
		from, to := resolveRelativeHalfRange(anchorYear, anchorMonth, target)
		return from, to, true
	}
	return "", "", false
}

func extractQuarterRange(q string, anchorYear, anchorMonth int) (string, string, bool) {
	explicitQuarterRe := regexp.MustCompile(`(?i)(20\d{2}|\d{2})\s*年?\s*(?:第?\s*([一二三四1234])\s*季度|Q\s*([1-4]))`)
	if m := explicitQuarterRe.FindStringSubmatch(q); len(m) == 4 {
		y := normalizeYearToken(m[1])
		token := m[2]
		if token == "" {
			token = m[3]
		}
		if quarter := parseQuarterToken(token); quarter >= 1 && quarter <= 4 {
			from, to := quarterRange(y, quarter)
			return from, to, true
		}
	}
	relativeQuarterRe := regexp.MustCompile(`(?:第?\s*([一二三四1234])\s*季度|Q\s*([1-4]))`)
	if m := relativeQuarterRe.FindStringSubmatch(q); len(m) == 3 {
		token := m[1]
		if token == "" {
			token = m[2]
		}
		if quarter := parseQuarterToken(token); quarter >= 1 && quarter <= 4 {
			from, to := resolveRelativeQuarterRange(anchorYear, anchorMonth, token)
			return from, to, true
		}
	}
	return "", "", false
}

func extractExplicitMonthRange(q string, anchorYear int) (string, string, bool) {
	rangeRe := regexp.MustCompile(`(?:从)?\s*(20\d{2}|\d{2}|今年|本年|去年)\s*年?\s*([0-1]?\d|[一二三四五六七八九十两]{1,3})月?(?:底|末)?\s*(?:到|至|-|~)\s*(20\d{2}|\d{2}|今年|本年|去年)?\s*年?\s*([0-1]?\d|[一二三四五六七八九十两]{1,3})月?(?:底|末)?`)
	m := rangeRe.FindStringSubmatch(q)
	if len(m) == 5 {
		y1 := resolveRelativeYearToken(m[1], anchorYear)
		y2 := y1
		if strings.TrimSpace(m[3]) != "" {
			y2 = resolveRelativeYearToken(m[3], anchorYear)
		}
		m1 := parseChineseOrDigitMonth(m[2])
		m2 := parseChineseOrDigitMonth(m[4])
		if !validMonth(m1) || !validMonth(m2) {
			return "", "", false
		}
		return formatPeriodValue(y1, m1), formatPeriodValue(y2, m2), true
	}

	sameYearRangeRe := regexp.MustCompile(`(20\d{2})年\s*([0-1]?\d|[一二三四五六七八九十两]{1,3})\s*月?\s*(?:到|至|-|~)\s*([0-1]?\d|[一二三四五六七八九十两]{1,3})月`)
	if m := sameYearRangeRe.FindStringSubmatch(q); len(m) == 4 {
		y := mustAtoi(m[1])
		m1 := parseChineseOrDigitMonth(m[2])
		m2 := parseChineseOrDigitMonth(m[3])
		if !validMonth(m1) || !validMonth(m2) {
			return "", "", false
		}
		return formatPeriodValue(y, m1), formatPeriodValue(y, m2), true
	}

	return "", "", false
}

func extractExplicitStartToRelativeMonthEndRange(q string, anchor time.Time) (string, string, bool) {
	re := regexp.MustCompile(`(?:从|自)?\s*(20\d{2}|\d{2}|今年|本年|去年)\s*年?\s*([0-1]?\d|[一二三四五六七八九十两]{1,3})月?(?:底|末)?\s*(?:起|开始)?\s*(?:到|至|截至|截止)\s*(上一个完整自然月|上个完整自然月|上一个完整月份|上个完整月份|上一个完整月|上个完整月|上个月|上月)(?:月底|月末|底|末)?`)
	m := re.FindStringSubmatch(q)
	if len(m) != 4 {
		return "", "", false
	}
	y := resolveRelativeYearToken(m[1], anchor.Year())
	month := parseChineseOrDigitMonth(m[2])
	if !validMonth(month) {
		return "", "", false
	}
	end := previousMonthPeriod(anchor)
	return formatPeriodValue(y, month), end, true
}

func extractExplicitStartToCurrentCompleteRange(q string, anchor time.Time) (string, string, bool) {
	re := regexp.MustCompile(`(?:从|自)?\s*(20\d{2}|\d{2}|今年|本年|去年)\s*年?\s*([0-1]?\d|[一二三四五六七八九十两]{1,3})月?(?:底|末)?\s*(?:起|开始)?\s*(?:到|至|截至|截止)?\s*(?:至今|现在|当前|目前)`)
	m := re.FindStringSubmatch(q)
	if len(m) != 3 {
		return "", "", false
	}
	y := resolveRelativeYearToken(m[1], anchor.Year())
	month := parseChineseOrDigitMonth(m[2])
	if !validMonth(month) {
		return "", "", false
	}
	return formatPeriodValue(y, month), previousMonthPeriod(anchor), true
}

func extractRelativeYearMonthRange(q string, anchorYear int) (string, string, bool) {
	re := regexp.MustCompile(`(今年|本年|去年)\s*([0-1]?\d|[一二三四五六七八九十两]{1,3})月?(?:底|末)?`)
	if m := re.FindStringSubmatch(q); len(m) == 3 {
		y := resolveRelativeYearToken(m[1], anchorYear)
		month := parseChineseOrDigitMonth(m[2])
		if !validMonth(month) {
			return "", "", false
		}
		if containsAnyPeriodKeyword(q, "截止", "截至") {
			return formatPeriodValue(y, 1), formatPeriodValue(y, month), true
		}
		p := formatPeriodValue(y, month)
		return p, p, true
	}
	return "", "", false
}

func extractYearCumulativeRange(q string, anchorYear, anchorMonth int) (string, string, bool) {
	yearCumulativeRe := regexp.MustCompile(`(20\d{2})年?\s*(?:累计|年内|累计销售额|累计收入|累计营收|累计回款)`)
	if m := yearCumulativeRe.FindStringSubmatch(q); len(m) == 2 {
		y := mustAtoi(m[1])
		endMonth := 12
		if y == anchorYear {
			endMonth = anchorMonth
		}
		return formatPeriodValue(y, 1), formatPeriodValue(y, endMonth), true
	}
	return "", "", false
}

func extractImplicitCumulativeRange(q string, anchorYear, anchorMonth int) (string, string, bool) {
	if strings.Contains(q, "累计") || strings.Contains(q, "年内") {
		return formatPeriodValue(anchorYear, 1), formatPeriodValue(anchorYear, anchorMonth), true
	}
	return "", "", false
}

func extractExplicitYearMonthRange(q string) (string, string, bool) {
	type ym struct {
		year  int
		month int
	}
	ymRe := regexp.MustCompile(`(20\d{2})年\s*([0-1]?\d|[一二三四五六七八九十两]{1,3})月`)
	yms := ymRe.FindAllStringSubmatch(q, -1)
	if len(yms) >= 2 {
		first := ym{year: mustAtoi(yms[0][1]), month: parseChineseOrDigitMonth(yms[0][2])}
		last := ym{year: mustAtoi(yms[len(yms)-1][1]), month: parseChineseOrDigitMonth(yms[len(yms)-1][2])}
		if validMonth(first.month) && validMonth(last.month) {
			return formatPeriodValue(first.year, first.month), formatPeriodValue(last.year, last.month), true
		}
	}
	if len(yms) == 1 {
		y := mustAtoi(yms[0][1])
		m := parseChineseOrDigitMonth(yms[0][2])
		if validMonth(m) {
			p := formatPeriodValue(y, m)
			return p, p, true
		}
	}
	return "", "", false
}

func extractRelativeNamedRange(q string, anchor time.Time) (string, string, bool) {
	switch {
	case strings.Contains(q, "今年") || strings.Contains(q, "本年"):
		return formatPeriodValue(anchor.Year(), 1), anchor.Format("2006-01"), true
	case strings.Contains(q, "去年"):
		y := anchor.Year() - 1
		return formatPeriodValue(y, 1), formatPeriodValue(y, 12), true
	case strings.Contains(q, "上个月"):
		t := anchor.AddDate(0, -1, 0)
		p := t.Format("2006-01")
		return p, p, true
	case strings.Contains(q, "下个月"):
		t := anchor.AddDate(0, 1, 0)
		p := t.Format("2006-01")
		return p, p, true
	case strings.Contains(q, "本月") || strings.Contains(q, "这个月") || strings.Contains(q, "当月"):
		p := anchor.Format("2006-01")
		return p, p, true
	}
	return "", "", false
}

func extractAdjacentMonthRange(q string, anchorYear, anchorMonth int) (string, string, bool) {
	re := regexp.MustCompile(`([一二三四五六七八九十两])([一二三四五六七八九十两])月(?:份)?`)
	if m := re.FindStringSubmatch(q); len(m) == 3 {
		m1 := parseChineseOrDigitMonth(m[1])
		m2 := parseChineseOrDigitMonth(m[2])
		if !validMonth(m1) || !validMonth(m2) || m1 > m2 {
			return "", "", false
		}
		y := anchorYear
		if m1 > anchorMonth && (m1-anchorMonth) >= 6 {
			y = anchorYear - 1
		}
		return formatPeriodValue(y, m1), formatPeriodValue(y, m2), true
	}
	return "", "", false
}

func extractRelativeMonthRange(q string, anchorYear, anchorMonth int) (string, string, bool) {
	monthRe := regexp.MustCompile(`([0-1]?\d|[一二三四五六七八九十两]{1,3})月`)
	if m := monthRe.FindStringSubmatch(q); len(m) == 2 {
		month := parseChineseOrDigitMonth(m[1])
		if validMonth(month) {
			y := anchorYear
			if month > anchorMonth && (month-anchorMonth) >= 6 {
				y = anchorYear - 1
			}
			p := formatPeriodValue(y, month)
			return p, p, true
		}
	}
	return "", "", false
}

func previousMonthPeriod(anchor time.Time) string {
	t := time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, anchor.Location()).AddDate(0, -1, 0)
	return t.Format("2006-01")
}

func resolveRelativeYearToken(raw string, anchorYear int) int {
	token := strings.TrimSpace(raw)
	switch token {
	case "今年", "本年":
		return anchorYear
	case "去年":
		return anchorYear - 1
	}
	return normalizeYearToken(token)
}

func containsAnyPeriodKeyword(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}
