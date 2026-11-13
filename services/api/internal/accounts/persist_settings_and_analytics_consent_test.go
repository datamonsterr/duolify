package accounts_test

import (
	"encoding/json"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

func TestSettingsPatchValidation(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"appearance":"dark","motion":"reduce","narration":true,"narration_speed":1.25,"captions":true,"text_size":1.2,"ui_sounds":false,"haptics":"system"}`, true},
		{`{"appearance":"neon"}`, false}, {`{"motion":"fast"}`, false},
		{`{"narration_speed":8}`, false}, {`{"text_size":0.5}`, false},
		{`{"haptics":"always"}`, false}, {`{"analytics_opt_out":true}`, true},
	} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(tc.body), &fields); err != nil {
			t.Fatal(err)
		}
		_, failures := accounts.ValidateSettingsPatch(fields, 2000)
		if (len(failures) == 0) != tc.valid {
			t.Errorf("%s: %v", tc.body, failures)
		}
	}
}
