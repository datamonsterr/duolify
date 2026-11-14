package steering

import (
	"reflect"
	"testing"
)

func TestPlanFork(t *testing.T) {
	stub := func(id, status string, reused bool) ForkStub { return ForkStub{ID: id, Status: status, Reused: reused} }
	for name, tc := range map[string]struct {
		in       []ForkStub
		marker   string
		unreused []string
		kept     int
	}{
		"first locked lesson is the marker":     {[]ForkStub{stub("a", "done", true), stub("b", "current", true), stub("c", "locked", true), stub("d", "locked", true)}, "c", []string{"c", "d"}, 2},
		"ready to generate counts as unlocked":  {[]ForkStub{stub("a", "done", true), stub("b", "ready_to_generate", false), stub("c", "locked", false)}, "c", nil, 2},
		"generating counts as unlocked":         {[]ForkStub{stub("a", "generating", false), stub("b", "locked", true)}, "b", []string{"b"}, 1},
		"all unlocked: no marker":               {[]ForkStub{stub("a", "done", true), stub("b", "current", true)}, "", nil, 2},
		"nothing unlocked":                      {[]ForkStub{stub("a", "locked", true)}, "a", []string{"a"}, 0},
		"locked but never reused is kept as is": {[]ForkStub{stub("a", "current", true), stub("b", "locked", false)}, "b", nil, 1},
		"empty":                                 {nil, "", nil, 0},
	} {
		t.Run(name, func(t *testing.T) {
			p := PlanFork(tc.in)
			if p.MarkerLessonID != tc.marker || p.KeptLessons != tc.kept || !reflect.DeepEqual(p.Unreuse, tc.unreused) {
				t.Fatalf("%+v", p)
			}
		})
	}
}

func TestForkConfirmationIsAConflict(t *testing.T) {
	if statusFor("fork_confirmation_required") != 409 {
		t.Fatal("status")
	}
}
