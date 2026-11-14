package player

import "encoding/json"

// Suppression also substitutes displayed speaker labels, never the stored dialogue or grading.
func overlaySpeakerLabels(raw json.RawMessage, segments map[string]json.RawMessage) map[string]json.RawMessage {
	var cast struct {
		Speakers map[string]struct {
			Suppressed bool `json:"suppressed"`
			Spec       struct {
				Info struct {
					Name string `json:"name"`
				} `json:"info"`
			} `json:"spec"`
		} `json:"speakers"`
	}
	if json.Unmarshal(raw, &cast) != nil {
		return segments
	}
	for slide, stored := range segments {
		var items []map[string]any
		if json.Unmarshal(stored, &items) != nil {
			continue
		}
		changed := false
		for _, item := range items {
			key, _ := item["speaker_key"].(string)
			speaker, ok := cast.Speakers[key]
			if ok && speaker.Suppressed {
				item["speaker_label"] = speaker.Spec.Info.Name
				changed = true
			}
		}
		if changed {
			segments[slide], _ = json.Marshal(items)
		}
	}
	return segments
}
