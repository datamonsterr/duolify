package chat

import "testing"

func TestParseLessonRef(t *testing.T) {
	const id = "7d4b3c0e-8f0a-4a8e-9a53-0c1f6d9c2a11"
	for name, tc := range map[string]struct {
		in *lessonInput
		ok bool
	}{
		"absent":         {nil, true},
		"first slide":    {&lessonInput{LessonID: id, SlideIndex: 0}, true},
		"last slide":     {&lessonInput{LessonID: id, SlideIndex: maxSlideIndex}, true},
		"negative":       {&lessonInput{LessonID: id, SlideIndex: -1}, false},
		"too large":      {&lessonInput{LessonID: id, SlideIndex: maxSlideIndex + 1}, false},
		"not a uuid":     {&lessonInput{LessonID: "nope"}, false},
		"empty id":       {&lessonInput{}, false},
		"uppercase uuid": {&lessonInput{LessonID: "7D4B3C0E-8F0A-4A8E-9A53-0C1F6D9C2A11"}, true},
	} {
		if got := tc.in.valid(); got != tc.ok {
			t.Errorf("%s: valid=%v want %v", name, got, tc.ok)
		}
	}
}
