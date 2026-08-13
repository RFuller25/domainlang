package runner

import (
	"strings"
	"testing"

	"domain/codegen"
)

// The allocation wire format is implemented twice — WriteReport here, and
// dmAllocReport emitted into every compiled program — for the same reason the
// foreign block's format is: the process being measured and the process doing
// the measuring cannot share code.
//
// The end-to-end tests (TestAllocReportingInterpreted / ...Compiled) prove
// both halves work today. This one is the cheap guard that catches a change to
// one half that forgets the other, without needing a Go toolchain: the emitted
// helper must write the same four fields, in the same order, that parseReport
// reads.
func TestEmittedAllocHelperMatchesTheProtocol(t *testing.T) {
	src := codegen.DeclAllocReport()
	if !strings.Contains(src, EnvAllocReport) {
		t.Errorf("the emitted helper does not read %s:\n%s", EnvAllocReport, src)
	}
	for _, field := range []string{"m.TotalAlloc", "m.Mallocs", "m.HeapSys", "m.NumGC"} {
		if !strings.Contains(src, field) {
			t.Errorf("the emitted helper does not report %s:\n%s", field, src)
		}
	}
	// The order matters: parseReport reads them positionally.
	want := []string{"m.TotalAlloc", "m.Mallocs", "m.HeapSys", "m.NumGC"}
	at := 0
	for _, field := range want {
		i := strings.Index(src[at:], field)
		if i < 0 {
			t.Fatalf("field %s out of order in the emitted helper:\n%s", field, src)
		}
		at += i
	}
	if n := strings.Count(src, "%d"); n != 4 {
		t.Errorf("the emitted helper writes %d fields, parseReport reads 4", n)
	}
}
