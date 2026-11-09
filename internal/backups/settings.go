package backups

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Key is the settings key of the backup document.
const Key = "backup"

// Allowed ranges (docs/api.md, the sidecar checks the same on read).
const (
	keepDaysMax    = 3650
	keepMinMax     = 1000
	maxAgeHoursMin = 25
	maxAgeHoursMax = 720
)

var backupTimeRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// Doc is the settings document "backup". A nil override means the sidecar's
// environment default.
type Doc struct {
	BackupTime  *string    `json:"backup_time"`
	KeepDays    *int       `json:"keep_days"`
	KeepMin     *int       `json:"keep_min"`
	MaxAgeHours *int       `json:"max_age_hours"`
	Paused      bool       `json:"paused"`
	PausedAt    *time.Time `json:"paused_at"`
}

// SettingsInput is BackupSettingsInput: a missing field is null.
type SettingsInput struct {
	BackupTime  *string `json:"backup_time"`
	KeepDays    *int    `json:"keep_days"`
	KeepMin     *int    `json:"keep_min"`
	MaxAgeHours *int    `json:"max_age_hours"`
}

// validate checks the input and returns it as overrides, or a problem that
// names the field and its allowed range. An empty backup_time means null.
func (in SettingsInput) validate() (SettingsInput, string) {
	if in.BackupTime != nil {
		v := strings.TrimSpace(*in.BackupTime)
		switch {
		case v == "":
			in.BackupTime = nil
		case !validTime(v):
			return in, "backup_time must be a 24-hour UTC time from 00:00 to 23:59 (HH:MM)"
		default:
			in.BackupTime = &v
		}
	}
	for _, c := range []struct {
		name     string
		v        *int
		min, max int
	}{
		{"keep_days", in.KeepDays, 0, keepDaysMax},
		{"keep_min", in.KeepMin, 0, keepMinMax},
		{"max_age_hours", in.MaxAgeHours, maxAgeHoursMin, maxAgeHoursMax},
	} {
		if c.v != nil && (*c.v < c.min || *c.v > c.max) {
			return in, fmt.Sprintf("%s must be between %d and %d", c.name, c.min, c.max)
		}
	}
	return in, ""
}

func validTime(s string) bool { return backupTimeRe.MatchString(s) }

func inRange(v *int, lo, hi int) *int {
	if v == nil || *v < lo || *v > hi {
		return nil
	}
	return v
}

// parseDoc reads a stored document leniently: a value of the wrong type or
// out of range counts as no override, like the sidecar does. raw may be
// empty or JSON null.
func parseDoc(raw []byte) Doc {
	var m map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
		return Doc{}
	}
	var d Doc
	var s string
	if json.Unmarshal(m["backup_time"], &s) == nil && validTime(s) {
		d.BackupTime = &s
	}
	d.KeepDays = inRange(intField(m["keep_days"]), 0, keepDaysMax)
	d.KeepMin = inRange(intField(m["keep_min"]), 0, keepMinMax)
	d.MaxAgeHours = inRange(intField(m["max_age_hours"]), maxAgeHoursMin, maxAgeHoursMax)
	_ = json.Unmarshal(m["paused"], &d.Paused)
	var at time.Time
	if d.Paused && json.Unmarshal(m["paused_at"], &at) == nil && !at.IsZero() {
		at = at.UTC()
		d.PausedAt = &at
	}
	return d
}

// intField decodes a JSON integer, or a string of digits (the sidecar reads
// value->>'key' as text, so it accepts both). Anything else is nil.
func intField(raw json.RawMessage) *int {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return &n
	}
	var s string
	if json.Unmarshal(raw, &s) != nil || s == "" || len(s) > 9 || strings.Trim(s, "0123456789") != "" {
		return nil
	}
	n, _ = strconv.Atoi(s)
	return &n
}
