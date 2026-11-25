package chat

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/jackc/pgx/v5"
)

const (
	maxTeachBackPoints = 8
	maxTeachBackSlides = 16
	maxPointRunes      = 300
)

type teachBackSlide struct {
	position               int
	topics                 []string
	explanation, narration string
	regions                []player.Region
}

func (s *Service) teachBackPoints(ctx context.Context, owner, lesson string, plan []string) ([]*enginev1.TeachBackPoint, error) {
	rows, err := s.opts.Pool.Query(ctx, `SELECT position, topic_ids, narration FROM lesson_slides
		WHERE lesson_stub_id=$1::uuid AND learner_id=$2::uuid AND position<$3 ORDER BY position`, lesson, owner, maxTeachBackSlides)
	if err != nil {
		return nil, err
	}
	var slides []teachBackSlide
	for rows.Next() {
		var slide teachBackSlide
		if err = rows.Scan(&slide.position, &slide.topics, &slide.narration); err != nil {
			rows.Close()
			return nil, err
		}
		slides = append(slides, slide)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if s.opts.Slides != nil {
		// Some playable copies only have object slides. The player cache keeps the authorized
		// manifest immutable and avoids re-reading the object for every position.
		if len(slides) == 0 {
			for i := range maxTeachBackSlides {
				slide, err := s.opts.Slides.SlideAt(ctx, owner, lesson, i)
				if errors.Is(err, player.ErrSlideNotFound) || errors.Is(err, pgx.ErrNoRows) {
					break
				}
				if err != nil {
					return nil, err
				}
				slides = append(slides, teachBackSlide{position: i, explanation: slide.Explanation, narration: slide.NarrationText, regions: slide.Regions})
			}
		} else {
			for i := range slides {
				if slide, err := s.opts.Slides.SlideAt(ctx, owner, lesson, slides[i].position); err == nil {
					slides[i].regions, slides[i].explanation = slide.Regions, slide.Explanation
				}
			}
		}
	}
	return buildTeachBackPoints(plan, slides), nil
}

func buildTeachBackPoints(plan []string, slides []teachBackSlide) []*enginev1.TeachBackPoint {
	if len(slides) == 0 {
		return nil
	}
	points := make([]*enginev1.TeachBackPoint, 0, maxTeachBackPoints)
	seenText := make(map[string]bool)
	add := func(text string, position int) {
		text = strings.Join(strings.Fields(text), " ")
		if runes := []rune(text); len(runes) > maxPointRunes {
			text = string(runes[:maxPointRunes])
		}
		normalized := strings.ToLower(text)
		if text == "" || !player.PlainText(text) || seenText[normalized] || len(points) == cap(points) {
			return
		}
		seenText[normalized] = true
		points = append(points, &enginev1.TeachBackPoint{
			Id: strconv.Itoa(len(points) + 1), Text: text,
			SlideIndex: int32(position), //nolint:gosec // loaded from bounded, non-negative Slide positions
		})
	}
	for i, text := range plan {
		// Immutable Lesson plans currently contain key point text, without Slide IDs.
		// Prefer the Slide whose own content matches, then use the plan's teaching order.
		position := slides[min(i*len(slides)/len(plan), len(slides)-1)].position
		best := 0
		for _, slide := range slides {
			if score := keyPointMatch(text, slide); score > best {
				best, position = score, slide.position
			}
		}
		add(text, position)
	}
	if len(points) > 0 {
		return points
	}
	seenTopics := make(map[string]bool)
	for _, slide := range slides {
		var descriptions []string
		for _, role := range []string{"explanation", "diagram", "example", "code", "caption", "title"} {
			for _, region := range slide.regions {
				if region.Role == role && strings.TrimSpace(region.Description) != "" {
					descriptions = append(descriptions, region.Description)
				}
			}
		}
		descriptions = append(descriptions, slide.explanation, slide.narration)
		if len(slide.topics) == 0 {
			for _, description := range descriptions {
				before := len(points)
				add(description, slide.position)
				if len(points) != before {
					break
				}
			}
			continue
		}
		for _, topic := range slide.topics {
			if seenTopics[topic] {
				continue
			}
			before := len(points)
			for _, description := range descriptions {
				add(description, slide.position)
				if len(points) != before {
					seenTopics[topic] = true
					break
				}
			}
		}
	}
	return points
}

func keyPointMatch(text string, slide teachBackSlide) int {
	var content strings.Builder
	content.WriteString(slide.explanation + " " + slide.narration + " " + strings.Join(slide.topics, " "))
	for _, region := range slide.regions {
		content.WriteString(" " + region.Description)
	}
	normalize := func(value string) []string {
		return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	}
	pointWords, slideWords := normalize(text), normalize(content.String())
	if len(pointWords) > 0 && strings.Contains(strings.Join(slideWords, " "), strings.Join(pointWords, " ")) {
		return 1000 + len(pointWords)
	}
	known := make(map[string]bool, len(slideWords))
	for _, word := range slideWords {
		known[word] = true
	}
	score := 0
	for _, word := range pointWords {
		if known[word] && len([]rune(word)) > 2 {
			score++
			delete(known, word)
		}
	}
	return score
}
