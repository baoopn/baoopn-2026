package config

import (
	"strings"
	"testing"
)

func TestMailEnabled(t *testing.T) {
	full := Config{SMTPHost: "smtp.baoopn.com", MailFrom: "Bao Nguyen <bao@mail.baoopn.com>", SMTPPassword: "us_live"}
	for _, c := range []struct {
		name string
		edit func(*Config)
		want bool
	}{
		{"configured", func(*Config) {}, true},
		{"no host", func(c *Config) { c.SMTPHost = "" }, false},
		{"no from", func(c *Config) { c.MailFrom = "" }, false},
		{"no password", func(c *Config) { c.SMTPPassword = "" }, false},
		{"placeholder password", func(c *Config) { c.SMTPPassword = "REPLACE_WITH_USESEND_KEY" }, false},
		{"password mentioning replace", func(c *Config) { c.SMTPPassword = "key_REPLACE_" }, true},
	} {
		cfg := full
		c.edit(&cfg)
		if got := cfg.MailEnabled(); got != c.want {
			t.Errorf("%s: MailEnabled = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestS3Enabled(t *testing.T) {
	full := Config{S3Endpoint: "https://acct.r2.cloudflarestorage.com", S3Bucket: "media",
		S3AccessKeyID: "id", S3SecretAccessKey: "secret"}
	if !full.S3Enabled() || full.S3Partial() {
		t.Fatal("full config should be enabled")
	}
	if (Config{}).S3Enabled() || (Config{}).S3Partial() {
		t.Fatal("empty config should be off and not partial")
	}
	for _, edit := range []func(*Config){
		func(c *Config) { c.S3Endpoint = "" }, func(c *Config) { c.S3Bucket = "" },
		func(c *Config) { c.S3AccessKeyID = "" }, func(c *Config) { c.S3SecretAccessKey = "" },
	} {
		c := full
		edit(&c)
		if c.S3Enabled() || !c.S3Partial() {
			t.Errorf("%+v: enabled %v partial %v", c, c.S3Enabled(), c.S3Partial())
		}
	}
}

func TestLoadMediaPublicURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("S3_SECRET_ACCESS_KEY", "secret")
	t.Setenv("MEDIA_PUBLIC_URL", "https://media.example.com/")
	c, err := Load()
	if err != nil || c.MediaPublicURL != "https://media.example.com" || c.S3Region != "auto" ||
		c.S3SecretAccessKey != "secret" {
		t.Fatalf("Load = %+v, %v", c, err)
	}
	for _, bad := range []string{"media.example.com", "ftp://media.example.com", "https://media.example.com/x",
		"https://media.example.com?a=1", "https://"} {
		t.Setenv("DATABASE_URL", "postgres://x") // unset by every Load
		t.Setenv("MEDIA_PUBLIC_URL", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MEDIA_PUBLIC_URL") {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
