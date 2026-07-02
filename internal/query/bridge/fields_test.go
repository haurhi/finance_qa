package bridge

import (
	"strings"
	"testing"
)

func TestBuildFinalAnswerAppendsSourceNotesToBossReplyText(t *testing.T) {
	data := map[string]any{
		"boss_reply_text":    "2026-03 项目应付 100.00 元。",
		"source_note":        "来源：《优集收入、成本计算表 - 上传.xlsx》",
		"source_update_note": "来源更新时间：2026-07-02 08:00:00",
	}

	finalAnswer := BuildFinalAnswer(data, "message should not win")

	for _, want := range []string{
		"2026-03 项目应付 100.00 元。",
		"来源：《优集收入、成本计算表 - 上传.xlsx》",
		"来源更新时间：2026-07-02 08:00:00",
	} {
		if !strings.Contains(finalAnswer, want) {
			t.Fatalf("final_answer should contain %q, got %q", want, finalAnswer)
		}
	}
}

func TestBuildFinalAnswerDoesNotDuplicateExistingSourceNotes(t *testing.T) {
	data := map[string]any{
		"boss_reply_text":    "2026-03 项目应付 100.00 元。\n来源：《A.xlsx》\n来源更新时间：2026-07-02 08:00:00",
		"source_note":        "来源：《A.xlsx》",
		"source_update_note": "来源更新时间：2026-07-02 08:00:00",
	}

	finalAnswer := BuildFinalAnswer(data, "message should not win")

	if strings.Count(finalAnswer, "来源：") != 1 {
		t.Fatalf("final_answer should not duplicate source note, got %q", finalAnswer)
	}
	if strings.Count(finalAnswer, "来源更新时间：") != 1 {
		t.Fatalf("final_answer should not duplicate source update note, got %q", finalAnswer)
	}
}

func TestBuildFinalAnswerAppendsStructuredSourceWhenTextHasGenericSource(t *testing.T) {
	data := map[string]any{
		"boss_reply_text":    "2026-03 项目应付 100.00 元。\n来源：项目表",
		"source_note":        "来源：《优集收入、成本计算表 - 上传.xlsx》",
		"source_update_note": "来源更新时间：2026-07-02 08:00:00",
	}

	finalAnswer := BuildFinalAnswer(data, "message should not win")

	for _, want := range []string{
		"来源：项目表",
		"来源：《优集收入、成本计算表 - 上传.xlsx》",
		"来源更新时间：2026-07-02 08:00:00",
	} {
		if !strings.Contains(finalAnswer, want) {
			t.Fatalf("final_answer should contain %q, got %q", want, finalAnswer)
		}
	}
}
