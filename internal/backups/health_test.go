package backups

import (
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func TestHealth(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	agent := func(mod func(*agentRow)) *agentRow {
		a := &agentRow{Agent: Agent{State: "waiting", LastSeenAt: now.Add(-30 * time.Second)},
			envMaxAge: ptr(26), retryMinutes: ptr(30), firstDueAt: ptr(now.Add(-time.Hour))}
		if mod != nil {
			mod(a)
		}
		return a
	}
	okRun := &Run{Status: "ok"}
	failedRun := &Run{Status: "failed"}
	for name, tc := range map[string]struct {
		a       *agentRow
		doc     Doc
		lastOK  *time.Time
		lastRun *Run
		want    Health
	}{
		"no agent":       {a: nil, want: NotReporting},
		"seen 3m ago":    {a: agent(func(a *agentRow) { a.LastSeenAt = now.Add(-3 * time.Minute) }), want: Offline},
		"seen 2m59s ago": {a: agent(func(a *agentRow) { a.LastSeenAt = now.Add(-179 * time.Second) }), want: Waiting},
		"idle":           {a: agent(func(a *agentRow) { a.State = "idle" }), want: NotConfigured},
		"offline beats idle": {a: agent(func(a *agentRow) { a.State = "idle"; a.LastSeenAt = now.Add(-time.Hour) }),
			want: Offline},
		"agent paused":   {a: agent(func(a *agentRow) { a.State = "paused" }), want: Paused},
		"doc paused":     {a: agent(nil), doc: Doc{Paused: true}, lastOK: ptr(now.Add(-time.Hour)), want: Paused},
		"recent success": {a: agent(nil), lastOK: ptr(now.Add(-time.Hour)), lastRun: okRun, want: OK},
		"success exactly max age": {a: agent(nil), lastOK: ptr(now.Add(-26 * time.Hour)), lastRun: okRun,
			want: OK},
		"success just past max age": {a: agent(nil), lastOK: ptr(now.Add(-26*time.Hour - time.Second)),
			want: Stale},
		"override max age": {a: agent(nil), doc: Doc{MaxAgeHours: ptr(48)}, lastOK: ptr(now.Add(-40 * time.Hour)),
			want: OK},
		"no env max age uses 26": {a: agent(func(a *agentRow) { a.envMaxAge = nil }),
			lastOK: ptr(now.Add(-27 * time.Hour)), want: Stale},
		"agent last_success_at counts": {a: agent(func(a *agentRow) { a.lastSuccessAt = ptr(now.Add(-time.Hour)) }),
			want: OK},
		"recent success, last run failed": {a: agent(nil), lastOK: ptr(now.Add(-2 * time.Hour)),
			lastRun: failedRun, want: Warning},
		// retry 30 + 60 min of slack after first_due_at.
		"waiting inside window": {a: agent(func(a *agentRow) { a.firstDueAt = ptr(now.Add(-89 * time.Minute)) }),
			want: Waiting},
		"waiting window over": {a: agent(func(a *agentRow) { a.firstDueAt = ptr(now.Add(-90 * time.Minute)) }),
			want: Stale},
		"retry minutes default 30": {a: agent(func(a *agentRow) {
			a.retryMinutes = nil
			a.firstDueAt = ptr(now.Add(-91 * time.Minute))
		}), want: Stale},
		"no first_due_at yet": {a: agent(func(a *agentRow) { a.firstDueAt = nil }), want: Waiting},
		"failed and no success": {a: agent(func(a *agentRow) { a.firstDueAt = ptr(now.Add(-48 * time.Hour)) }),
			lastRun: failedRun, want: Stale},
	} {
		t.Run(name, func(t *testing.T) {
			st := Status{CheckedAt: now, LastRun: tc.lastRun}
			st.fill(tc.a, tc.doc, nil, tc.lastOK)
			if st.Health != tc.want {
				t.Errorf("health = %s, want %s", st.Health, tc.want)
			}
		})
	}
}

func TestFillSettings(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	applied := now.Add(-time.Minute)
	a := &agentRow{Agent: Agent{State: "waiting", LastSeenAt: now, SettingsAppliedAt: &applied},
		envTime: ptr("03:00"), envKeepDays: ptr(30), envKeepMin: ptr(7), envMaxAge: ptr(26), retryMinutes: ptr(30)}

	var st Status
	st.CheckedAt = now
	st.fill(a, Doc{KeepDays: ptr(14)}, ptr(now.Add(-2*time.Minute)), nil)
	s := st.Settings
	if *s.KeepDays.Value != 14 || *s.KeepDays.Env != 30 || *s.KeepDays.Override != 14 || s.KeepDays.Source != "override" {
		t.Errorf("keep_days = %+v", s.KeepDays)
	}
	if *s.BackupTime.Value != "03:00" || s.BackupTime.Override != nil || s.BackupTime.Source != "env" {
		t.Errorf("backup_time = %+v", s.BackupTime)
	}
	if s.Pending || *s.RetryMinutes != 30 || *s.OnStart {
		t.Errorf("settings = %+v", s)
	}

	st = Status{CheckedAt: now}
	st.fill(a, Doc{}, ptr(now), nil)
	if !st.Settings.Pending {
		t.Error("updated after settings_applied_at: want pending")
	}
	a.SettingsAppliedAt = nil
	st = Status{CheckedAt: now}
	st.fill(a, Doc{}, ptr(now.Add(-time.Hour)), nil)
	if !st.Settings.Pending {
		t.Error("never applied: want pending")
	}
	st = Status{CheckedAt: now}
	st.fill(a, Doc{}, nil, nil)
	if st.Settings.Pending {
		t.Error("no document: want not pending")
	}

	st = Status{CheckedAt: now}
	st.fill(nil, Doc{KeepMin: ptr(3)}, nil, nil)
	if st.Agent != nil || st.Destination.Kind != nil || st.Secrets != (Secrets{}) ||
		st.Settings.KeepMin.Env != nil || *st.Settings.KeepMin.Value != 3 || st.Settings.RetryMinutes != nil ||
		st.Settings.OnStart != nil {
		t.Errorf("no agent = %+v", st)
	}
}
