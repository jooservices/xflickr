package process_test

import (
	"testing"

	"github.com/jooservices/xflickr/internal/platform/metrics/adapters/process"
)

func TestProcCPUParse(t *testing.T) {
	// Synthetic /proc/self/stat: pid (comm) state ppid ... utime stime ...
	// Fields after ') ': state=R, ppid=1, pgrp=1, session=1, tty=0, tpgid=-1,
	// flags=0, minflt=0, cminflt=0, majflt=0, cmajflt=0, utime=100, stime=50
	stat := "1234 (xflickr) R 1 1 1 0 -1 0 0 0 0 0 100 50 0 0 20 0 1 0 12345 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"
	got, err := process.ParseStatCPU(stat)
	if err != nil {
		t.Fatal(err)
	}
	if got.Jiffies != 150 {
		t.Fatalf("jiffies=%d want 150", got.Jiffies)
	}
	statSpaceComm := "9 (go test) S 1 1 1 0 -1 0 0 0 0 0 200 100 0 0 20 0 1 0 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"
	got, err = process.ParseStatCPU(statSpaceComm)
	if err != nil {
		t.Fatal(err)
	}
	if got.Jiffies != 300 {
		t.Fatalf("jiffies with spaced comm=%d want 300", got.Jiffies)
	}
	if _, err := process.ParseStatCPU("broken"); err == nil {
		t.Fatal("expected parse error")
	}
	short := "1 (x) R 1"
	if _, err := process.ParseStatCPU(short); err == nil {
		t.Fatal("expected short stat error")
	}
	badUtime := "1 (x) R 1 1 1 0 -1 0 0 0 0 0 x 50 0 0 20 0 1 0 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"
	if _, err := process.ParseStatCPU(badUtime); err == nil {
		t.Fatal("expected utime parse error")
	}
	badStime := "1 (x) R 1 1 1 0 -1 0 0 0 0 0 100 y 0 0 20 0 1 0 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"
	if _, err := process.ParseStatCPU(badStime); err == nil {
		t.Fatal("expected stime parse error")
	}
	percent := process.CPUPercent(process.CPUSample{Jiffies: 100}, process.CPUSample{Jiffies: 200}, 1, 100)
	if percent != 100 {
		t.Fatalf("CPUPercent=%v want 100", percent)
	}
	if process.CPUPercent(process.CPUSample{Jiffies: 1}, process.CPUSample{Jiffies: 2}, 1, 0) != 0 {
		t.Fatal("expected zero for zero clock ticks")
	}
}
