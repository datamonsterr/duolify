package accounts_test

import (
	"encoding/json"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"strings"
	"testing"
)

func TestOptionalContextBoundaries(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"goal":"界界界","locale":"vi-VN","time_zone":"Asia/Ho_Chi_Minh"}`, true},
		{`{"goal":"界界界界"}`, false}, {`{"background":"😀😀😀"}`, true},
		{`{"goal":"","locale":"","time_zone":"","daily_minutes":null,"daily_xp":null,"reminder_enabled":null}`, true},
		{`{"level":"expert","daily_minutes":1440,"daily_xp":10000,"reminder_time":"23:59","reminder_enabled":true}`, true},
		{`{"goal":null}`, false}, {`{"topic":4}`, false}, {`{"daily_minutes":1.5}`, false}, {`{"daily_xp":0}`, false}, {`{"level":"Beginner"}`, false},
		{`{"time_zone":"Local"}`, false}, {`{"time_zone":"+07:00"}`, false}, {`{"locale":"en_XX"}`, false}, {`{"reminder_time":"9:30"}`, false}, {`{"reminder_enabled":"yes"}`, false}, {`{"analytics_opt_out":false}`, false},
	} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(tc.body), &fields); err != nil {
			t.Fatal(err)
		}
		_, errors := accounts.ValidateContextPatch(fields, 3)
		if (len(errors) == 0) != tc.valid {
			t.Errorf("%s: %v", tc.body, errors)
		}
		raw, _ := json.Marshal(errors)
		if strings.Contains(string(raw), "界") {
			t.Fatal("validation echoes private text")
		}
	}
}
func TestProfileConfigRejectsUnboundedLimits(t *testing.T) {
	if _, err := accounts.ProfileOptionsFromEnv(func(string) string { return "0" }); err == nil {
		t.Fatal("zero limit accepted")
	}
	if _, err := accounts.ProfileOptionsFromEnv(func(string) string { return "not-an-int" }); err == nil {
		t.Fatal("invalid limit accepted")
	}
	cfg, err := accounts.ProfileOptionsFromEnv(func(string) string { return "" })
	if err != nil || cfg.TextLimit != 2000 {
		t.Fatal("default limit", err)
	}
}
