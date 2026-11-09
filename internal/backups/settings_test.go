package backups

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for name, tc := range map[string]struct {
		in      SettingsInput
		problem string // substring; "" = valid
	}{
		"all null":           {},
		"time 00:00":         {in: SettingsInput{BackupTime: ptr("00:00")}},
		"time 23:59":         {in: SettingsInput{BackupTime: ptr("23:59")}},
		"time 24:00":         {in: SettingsInput{BackupTime: ptr("24:00")}, problem: "backup_time"},
		"time 3:00":          {in: SettingsInput{BackupTime: ptr("3:00")}, problem: "backup_time"},
		"time 12:60":         {in: SettingsInput{BackupTime: ptr("12:60")}, problem: "backup_time"},
		"time with seconds":  {in: SettingsInput{BackupTime: ptr("12:00:00")}, problem: "backup_time"},
		"time empty is null": {in: SettingsInput{BackupTime: ptr("")}},
		"keep_days 0":        {in: SettingsInput{KeepDays: ptr(0)}},
		"keep_days 3650":     {in: SettingsInput{KeepDays: ptr(3650)}},
		"keep_days -1":       {in: SettingsInput{KeepDays: ptr(-1)}, problem: "keep_days must be between 0 and 3650"},
		"keep_days 3651":     {in: SettingsInput{KeepDays: ptr(3651)}, problem: "keep_days must be between 0 and 3650"},
		"keep_min 0":         {in: SettingsInput{KeepMin: ptr(0)}},
		"keep_min 1000":      {in: SettingsInput{KeepMin: ptr(1000)}},
		"keep_min -1":        {in: SettingsInput{KeepMin: ptr(-1)}, problem: "keep_min must be between 0 and 1000"},
		"keep_min 1001":      {in: SettingsInput{KeepMin: ptr(1001)}, problem: "keep_min must be between 0 and 1000"},
		"max_age 25":         {in: SettingsInput{MaxAgeHours: ptr(25)}},
		"max_age 720":        {in: SettingsInput{MaxAgeHours: ptr(720)}},
		"max_age 24":         {in: SettingsInput{MaxAgeHours: ptr(24)}, problem: "max_age_hours must be between 25 and 720"},
		"max_age 721":        {in: SettingsInput{MaxAgeHours: ptr(721)}, problem: "max_age_hours must be between 25 and 720"},
	} {
		t.Run(name, func(t *testing.T) {
			out, problem := tc.in.validate()
			if tc.problem == "" && problem != "" || !strings.Contains(problem, tc.problem) {
				t.Fatalf("problem = %q, want %q", problem, tc.problem)
			}
			if name == "time empty is null" && out.BackupTime != nil {
				t.Errorf("empty backup_time = %q, want nil", *out.BackupTime)
			}
		})
	}
}

func TestParseDoc(t *testing.T) {
	d := parseDoc([]byte(`{"backup_time":"25:00","keep_days":"14","keep_min":7.5,"max_age_hours":24,
		"paused":true,"paused_at":"2026-11-09T10:00:00+07:00","extra":1}`))
	if d.BackupTime != nil || d.KeepDays == nil || *d.KeepDays != 14 || d.KeepMin != nil || d.MaxAgeHours != nil {
		t.Errorf("lenient parse = %+v", d)
	}
	if !d.Paused || d.PausedAt == nil || d.PausedAt.Location().String() != "UTC" || d.PausedAt.Hour() != 3 {
		t.Errorf("paused = %v %v", d.Paused, d.PausedAt)
	}
	for _, raw := range []string{"", "null", "[]", `"x"`, `{"keep_days":"-3","keep_min":"1e3","paused":"yes"}`,
		`{"backup_time":null,"keep_days":null,"keep_min":null,"max_age_hours":null,"paused":false,"paused_at":null}`} {
		if got := parseDoc([]byte(raw)); got != (Doc{}) {
			t.Errorf("parseDoc(%q) = %+v, want empty", raw, got)
		}
	}
	if d := parseDoc([]byte(`{"paused":true,"paused_at":null}`)); !d.Paused || d.PausedAt != nil {
		t.Errorf("paused with null paused_at = %+v", d)
	}
	d = parseDoc([]byte(`{"backup_time":"03:30","keep_days":0,"keep_min":1000,"max_age_hours":720,"paused":false,
		"paused_at":"2026-11-09T10:00:00Z"}`))
	if *d.BackupTime != "03:30" || *d.KeepDays != 0 || *d.KeepMin != 1000 || *d.MaxAgeHours != 720 || d.PausedAt != nil {
		t.Errorf("valid parse = %+v", d)
	}
}
