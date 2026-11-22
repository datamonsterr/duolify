package ids

import "testing"

func TestArgBindsNullForAnythingButAUUID(t *testing.T) {
	for _, bad := range []string{"", "astronomy", "demo", "123", "0000000000000000000000000000000z", "not-a-uuid"} {
		if got := Arg(bad); got != nil {
			t.Errorf("Arg(%q) = %v, want nil", bad, got)
		}
	}
	for _, good := range []string{"00000000-0000-4000-8000-000000000000", "AAAAAAAA-0000-4000-8000-000000000000"} {
		if got := Arg(good); got == nil {
			t.Errorf("Arg(%q) = nil, want a uuid", good)
		}
	}
}
