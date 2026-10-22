package logs

import "testing"

func TestStripANSI(t *testing.T) {
	tests := []struct{ in, want string }{
		{"\x1b[31mERROR\x1b[0m boom", "ERROR boom"},
		{"plain", "plain"},
	}
	for _, tt := range tests {
		if got := StripANSI(tt.in); got != tt.want {
			t.Errorf("StripANSI(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The same cases as the web log viewer (frontend/src/features/logs/lines.test.ts).
func TestLevelOf(t *testing.T) {
	tests := []struct {
		line string
		want Level
	}{
		{"ERROR could not connect", Error},
		{"panic: runtime error", Error},
		{`{"level":"error","msg":"x"}`, Error},
		{"level=fatal msg=bye", Error},
		{"Traceback (most recent call last):", Error},
		{"WARN slow query", Warn},
		{"level=warn msg=x", Warn},
		{"GET /healthz 200", Plain},
		{"errors_total=0", Plain},
		// A stated level wins over words in the message or field names.
		{`{"level":"WARN","msg":"cluster request failed","code":"forbidden","err":"pods is forbidden"}`, Warn},
		{`{"level":"INFO","msg":"retrying","err":"timeout"}`, Plain},
		{`{"severity":"CRITICAL","message":"disk full"}`, Error},
		{`{"level":"debug","msg":"ERROR budget fine"}`, Plain},
		{`level=info msg="slow" err="WARN: context canceled"`, Plain},
		{"time=1 level=WARNING msg=x", Warn},
		// Kubernetes components (klog).
		{`E1022 10:00:01.123456       1 controller.go:42] "sync failed"`, Error},
		{"W1022 10:00:01.123456       1 reflector.go:7] watch closed", Warn},
		{"I1022 10:00:01.123456       1 main.go:9] ERROR-free start", Plain},
		// No stated level: a key named err isn't an error by itself.
		{`{"msg":"done","err":null}`, Plain},
		{"request failed err=timeout", Plain},
		{"ERROR: could not connect", Error},
		{"panic: boom", Error},
		{"Warning: deprecated flag", Warn},
	}
	for _, tt := range tests {
		if got := LevelOf(tt.line); got != tt.want {
			t.Errorf("LevelOf(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}
