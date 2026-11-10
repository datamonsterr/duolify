package lessontest_test

import (
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/config"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
)

func TestSpacedQueueOrdering(t *testing.T) {
	qB := lessontest.PublicQuestion{QuestionID: "qB", TopicID: "top-b"}
	qC := lessontest.PublicQuestion{QuestionID: "qC", TopicID: "top-c"}
	variant := lessontest.PublicQuestion{QuestionID: "v-qA", TopicID: "top-a", IsVariant: true}

	t.Run("inserts after min_gap questions when at least min_gap remain", func(t *testing.T) {
		remaining := []lessontest.PublicQuestion{qB, qC}
		updated := lessontest.InsertSpacedVariant(remaining, variant, 2)
		if len(updated) != 3 {
			t.Fatalf("expected 3 items, got %d", len(updated))
		}
		if updated[0].QuestionID != "qB" || updated[1].QuestionID != "qC" || updated[2].QuestionID != "v-qA" {
			t.Fatalf("unexpected order: %+v", updated)
		}
	})

	t.Run("inserts after min_gap questions when more than min_gap remain", func(t *testing.T) {
		qD := lessontest.PublicQuestion{QuestionID: "qD", TopicID: "top-d"}
		remaining := []lessontest.PublicQuestion{qB, qC, qD}
		updated := lessontest.InsertSpacedVariant(remaining, variant, 2)
		if len(updated) != 4 {
			t.Fatalf("expected 4 items, got %d", len(updated))
		}
		if updated[0].QuestionID != "qB" || updated[1].QuestionID != "qC" || updated[2].QuestionID != "v-qA" || updated[3].QuestionID != "qD" {
			t.Fatalf("unexpected order: %+v", updated)
		}
	})

	t.Run("places at end when fewer than min_gap remain but not empty", func(t *testing.T) {
		remaining := []lessontest.PublicQuestion{qB}
		updated := lessontest.InsertSpacedVariant(remaining, variant, 2)
		if len(updated) != 2 {
			t.Fatalf("expected 2 items, got %d", len(updated))
		}
		if updated[0].QuestionID != "qB" || updated[1].QuestionID != "v-qA" {
			t.Fatalf("unexpected order: %+v", updated)
		}
	})

	t.Run("becomes next question only when queue is empty", func(t *testing.T) {
		var remaining []lessontest.PublicQuestion
		updated := lessontest.InsertSpacedVariant(remaining, variant, 2)
		if len(updated) != 1 || updated[0].QuestionID != "v-qA" {
			t.Fatalf("unexpected order: %+v", updated)
		}
	})
}

func TestLessonTestConfigDefaultsAndOverrides(t *testing.T) {
	t.Run("defaults apply when env vars are unset", func(t *testing.T) {
		cfg, err := config.Load(func(key string) string {
			switch key {
			case "DATABASE_URL":
				return "postgres://user:pass@localhost:5432/db"
			case "REDIS_URL":
				return "redis://localhost:6379"
			case "ENGINE_ADDR":
				return "http://localhost:50051"
			default:
				return ""
			}
		})
		if err != nil {
			t.Fatalf("config load failed: %v", err)
		}
		if cfg.LessonTestMinGap != 2 {
			t.Errorf("expected LessonTestMinGap 2, got %d", cfg.LessonTestMinGap)
		}
		if cfg.LessonTestMaxVariantsPerTopic != 3 {
			t.Errorf("expected LessonTestMaxVariantsPerTopic 3, got %d", cfg.LessonTestMaxVariantsPerTopic)
		}
		if cfg.LessonTestMaxVariantsPerTest != 12 {
			t.Errorf("expected LessonTestMaxVariantsPerTest 12, got %d", cfg.LessonTestMaxVariantsPerTest)
		}
	})

	t.Run("overrides parsed correctly from env", func(t *testing.T) {
		cfg, err := config.Load(func(key string) string {
			switch key {
			case "DATABASE_URL":
				return "postgres://user:pass@localhost:5432/db"
			case "REDIS_URL":
				return "redis://localhost:6379"
			case "ENGINE_ADDR":
				return "http://localhost:50051"
			case "LESSON_TEST_MIN_GAP":
				return "4"
			case "LESSON_TEST_MAX_VARIANTS_PER_TOPIC":
				return "5"
			case "LESSON_TEST_MAX_VARIANTS_PER_TEST":
				return "15"
			default:
				return ""
			}
		})
		if err != nil {
			t.Fatalf("config load failed: %v", err)
		}
		if cfg.LessonTestMinGap != 4 {
			t.Errorf("expected LessonTestMinGap 4, got %d", cfg.LessonTestMinGap)
		}
		if cfg.LessonTestMaxVariantsPerTopic != 5 {
			t.Errorf("expected LessonTestMaxVariantsPerTopic 5, got %d", cfg.LessonTestMaxVariantsPerTopic)
		}
		if cfg.LessonTestMaxVariantsPerTest != 15 {
			t.Errorf("expected LessonTestMaxVariantsPerTest 15, got %d", cfg.LessonTestMaxVariantsPerTest)
		}
	})
}
