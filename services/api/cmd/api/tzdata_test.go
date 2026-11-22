package main

import (
	"os/exec"
	"strings"
	"testing"
)

// Learner time zones come from the browser (Intl), which reports legacy IANA aliases such as
// Asia/Saigon. The api and worker images do not reliably carry those (Debian moved them to
// tzdata-legacy), and credits.Reserve fails closed on an unknown zone, so every paid action
// failed for those learners. The binary must embed the time zone database.
func TestBinaryEmbedsTheTimeZoneDatabase(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if dep == "time/tzdata" {
			return
		}
	}
	t.Fatal("cmd/api does not link time/tzdata; add `import _ \"time/tzdata\"`")
}
