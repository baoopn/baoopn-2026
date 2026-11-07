package config

import "testing"

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
