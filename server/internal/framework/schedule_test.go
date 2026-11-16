package framework

import (
	"strings"
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

const schedSrc = "package console\n\nfunc f() {\n\tschedule.Command(\"x\").Cron(\"*/5 * * * *\")\n}\n"

func TestSchedule_Positive_InlayHintAfterCron(t *testing.T) {
	hs := ScheduleInlayHints("/p/app/console/kernel.go", schedSrc)
	if len(hs) != 1 || !strings.Contains(hs[0].Label, "Every 5 minutes") {
		t.Fatalf("expected 'Every 5 minutes' hint, got %+v", hs)
	}
}

func TestSchedule_Positive_HoverOnCron(t *testing.T) {
	off := strings.Index(schedSrc, "*/5") + 1
	if h := ScheduleHover("/p/app/console/kernel.go", schedSrc, off); h == nil {
		t.Error("expected hover over cron literal")
	}
}

func TestSchedule_Negative_NoCronNoHints(t *testing.T) {
	if hs := ScheduleInlayHints("/p/a.go", "package x\nfunc f() {}\n"); hs != nil {
		t.Errorf("got %+v", hs)
	}
}

func TestSchedule_Negative_NonLiteralArgIgnored(t *testing.T) {
	src := "package x\nfunc f(e string) { s.Cron(e) }\n"
	if hs := ScheduleInlayHints("/p/a.go", src); len(hs) != 0 {
		t.Errorf("got %+v", hs)
	}
}

func TestSchedule_Negative_EmptyCronIgnored(t *testing.T) {
	src := "package x\nfunc f() { s.Cron(\"\") }\n"
	if hs := ScheduleInlayHints("/p/a.go", src); len(hs) != 0 {
		t.Errorf("got %+v", hs)
	}
}

func TestSchedule_Negative_HoverOutsideCron(t *testing.T) {
	if h := ScheduleHover("/p/a.go", schedSrc, 0); h != nil {
		t.Errorf("expected nil hover, got %+v", h)
	}
}

func TestSchedule_Negative_SyntaxError(t *testing.T) {
	if hs := ScheduleInlayHints("/p/a.go", "package x\nfunc ( .Cron("); hs != nil {
		t.Errorf("got %+v", hs)
	}
}

func TestHumanCron_Positive(t *testing.T) {
	cases := map[string]string{
		"* * * * *":    "Every minute",
		"*/10 * * * *": "Every 10 minutes",
		"0 */2 * * *":  "Every 2 hours",
		"15 * * * *":   "Every hour at :15",
		"30 4 * * *":   "Daily at 04:30",
	}
	for in, want := range cases {
		if got := indexer.HumanCron(in); got != want {
			t.Errorf("HumanCron(%q)=%q want %q", in, got, want)
		}
	}
}

func TestHumanCron_Negative(t *testing.T) {
	if got := indexer.HumanCron("bad"); got != "bad" {
		t.Errorf("short expr should echo input, got %q", got)
	}
	if got := indexer.HumanCron(""); got != "" {
		t.Errorf("empty should stay empty, got %q", got)
	}
	if got := indexer.HumanCron("0 0 1 1 1"); !strings.HasPrefix(got, "Cron (") {
		t.Errorf("complex expr falls back to raw form, got %q", got)
	}
}

func TestHumanScheduleMethod(t *testing.T) {
	if indexer.HumanScheduleMethod("EveryMinute", "") != "Every minute" {
		t.Error("EveryMinute")
	}
	if indexer.HumanScheduleMethod("DailyAt", "13:00") != "Every day at 13:00" {
		t.Error("DailyAt with arg")
	}
	if indexer.HumanScheduleMethod("DailyAt", "") != "Every day" {
		t.Error("DailyAt without arg")
	}
	if indexer.HumanScheduleMethod("Mystery", "") != "Mystery" {
		t.Error("unknown method echoes name")
	}
}
