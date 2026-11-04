package receipt

import (
	"github.com/Chaebin-Park/naru/internal/environment"
	"testing"
)

func notRunFixture() Record {
	r := recordFixture()
	result, duration := ResultNotRun, int64(0)
	r.Phase = PhaseTerminal
	r.FinishedAt = &r.StartedAt
	r.Execution.Result = &result
	r.Execution.DurationMS = &duration
	r.Execution.Unavailable = &environment.Unavailable{Reason: "missing_tool", Subject: "docker"}
	r.Inputs.Before = InputDigest{}
	r.Inputs.Validity = InputsUnobserved
	return r
}
func TestNotRunRecordValidation(t *testing.T) {
	r := notRunFixture()
	if _, err := DecodeRecord(jsonRecord(t, r), r.Check); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Record){
		"exit code":        func(r *Record) { n := 0; r.Execution.ExitCode = &n },
		"observed inputs":  func(r *Record) { r.Inputs.Before = recordFixture().Inputs.Before },
		"after":            func(r *Record) { d := recordFixture().Inputs.Before; r.Inputs.After = &d },
		"duration":         func(r *Record) { n := int64(1); r.Execution.DurationMS = &n },
		"missing duration": func(r *Record) { r.Execution.DurationMS = nil },
		"missing reason":   func(r *Record) { r.Execution.Unavailable = nil },
		"unknown reason":   func(r *Record) { r.Execution.Unavailable.Reason = "unknown" },
		"empty subject":    func(r *Record) { r.Execution.Unavailable.Subject = "" },
		"wrong validity":   func(r *Record) { r.Inputs.Validity = InputsMatched },
		"running":          func(r *Record) { r.Phase = PhaseRunning },
		"missing finish":   func(r *Record) { r.FinishedAt = nil },
		"pass":             func(r *Record) { s := ResultPass; r.Execution.Result = &s },
	} {
		t.Run(name, func(t *testing.T) {
			r := notRunFixture()
			mutate(&r)
			if _, err := DecodeRecord(jsonRecord(t, r), r.Check); err == nil {
				t.Fatal("invalid not-run accepted")
			}
		})
	}
	r = terminalFixture()
	r.Execution.Unavailable = notRunFixture().Execution.Unavailable
	if _, err := DecodeRecord(jsonRecord(t, r), r.Check); err == nil {
		t.Fatal("pass with unavailable accepted")
	}
}
