package settings

import (
	"os"
	"strings"
	"testing"
)

var allEnvKeys = []string{
	"PORT",
	"DATA_DIR",
	"ADMIN_PASSWORD",
	"SECRET_KEY",
	"SMTP_HOST",
	"SMTP_PORT",
	"SMTP_USERNAME",
	"SMTP_PASSWORD",
	"SMTP_FROM",
	"RETENTION_DAYS",
	"SECURE_COOKIES",
}

func clearEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for _, k := range allEnvKeys {
		os.Unsetenv(k)
	}
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

func TestDefaults(t *testing.T) {
	clearEnv(t, nil)

	s, err := Parse()
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if s.Port != DefaultPort {
		t.Errorf("Port = %q, want %q", s.Port, DefaultPort)
	}
	if s.DataDir != DefaultDataDir {
		t.Errorf("DataDir = %q, want %q", s.DataDir, DefaultDataDir)
	}
	if s.RetentionDays != DefaultRetentionDays {
		t.Errorf("RetentionDays = %d, want %d", s.RetentionDays, DefaultRetentionDays)
	}
	if s.AdminPassword != "" {
		t.Errorf("AdminPassword = %q, want empty", s.AdminPassword)
	}
	if s.SMTP != (SMTPConfig{}) {
		t.Errorf("SMTP = %+v, want zero value", s.SMTP)
	}
	if s.SecretKey == "" {
		t.Error("SecretKey is empty, want auto-generated value")
	}
	if len(s.SecretKey) != 64 {
		t.Errorf("SecretKey length = %d, want 64 (32 random bytes hex-encoded)", len(s.SecretKey))
	}
	if s.SecureCookies {
		t.Error("SecureCookies = true, want false by default")
	}
}

func TestEnvOverrides(t *testing.T) {
	clearEnv(t, map[string]string{
		"PORT":           "9090",
		"DATA_DIR":       "/var/lib/minitor",
		"ADMIN_PASSWORD": "hunter2",
		"SECRET_KEY":     "fixed-secret",
		"RETENTION_DAYS": "7",
	})

	s, err := Parse()
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if s.Port != "9090" {
		t.Errorf("Port = %q, want %q", s.Port, "9090")
	}
	if s.DataDir != "/var/lib/minitor" {
		t.Errorf("DataDir = %q, want %q", s.DataDir, "/var/lib/minitor")
	}
	if s.AdminPassword != "hunter2" {
		t.Errorf("AdminPassword = %q, want %q", s.AdminPassword, "hunter2")
	}
	if s.SecretKey != "fixed-secret" {
		t.Errorf("SecretKey = %q, want %q", s.SecretKey, "fixed-secret")
	}
	if s.RetentionDays != 7 {
		t.Errorf("RetentionDays = %d, want 7", s.RetentionDays)
	}
}

func TestSecureCookiesTrue(t *testing.T) {
	clearEnv(t, map[string]string{"SECURE_COOKIES": "true"})

	s, err := Parse()
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !s.SecureCookies {
		t.Error("SecureCookies = false, want true")
	}
}

func TestSecureCookiesInvalid(t *testing.T) {
	clearEnv(t, map[string]string{"SECURE_COOKIES": "yes"})

	_, err := Parse()
	if err == nil {
		t.Fatal("Parse returned nil error, want error for invalid SECURE_COOKIES")
	}
	if !strings.Contains(err.Error(), "SECURE_COOKIES") {
		t.Errorf("error %q does not mention SECURE_COOKIES", err)
	}
}

func TestSMTPAllSet(t *testing.T) {
	clearEnv(t, map[string]string{
		"SMTP_HOST":     "smtp.example.com",
		"SMTP_PORT":     "587",
		"SMTP_USERNAME": "alerts@example.com",
		"SMTP_PASSWORD": "smtp-secret",
		"SMTP_FROM":     "alerts@example.com",
	})

	s, err := Parse()
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if s.SMTP.Host != "smtp.example.com" {
		t.Errorf("SMTP.Host = %q, want %q", s.SMTP.Host, "smtp.example.com")
	}
	if s.SMTP.Port != "587" {
		t.Errorf("SMTP.Port = %q, want %q", s.SMTP.Port, "587")
	}
	if s.SMTP.Username != "alerts@example.com" {
		t.Errorf("SMTP.Username = %q, want %q", s.SMTP.Username, "alerts@example.com")
	}
	if s.SMTP.Password != "smtp-secret" {
		t.Errorf("SMTP.Password = %q, want %q", s.SMTP.Password, "smtp-secret")
	}
	if s.SMTP.From != "alerts@example.com" {
		t.Errorf("SMTP.From = %q, want %q", s.SMTP.From, "alerts@example.com")
	}
}

func TestSMTPPartialSet(t *testing.T) {
	clearEnv(t, map[string]string{"SMTP_HOST": "smtp.example.com"})

	_, err := Parse()
	if err == nil {
		t.Fatal("Parse returned nil error, want incomplete SMTP configuration error")
	}
	for _, missing := range []string{"SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM"} {
		if !strings.Contains(err.Error(), missing) {
			t.Errorf("error %q does not mention missing var %s", err, missing)
		}
	}
}

func TestSMTPSetIndividually(t *testing.T) {
	envKeys := []string{
		"SMTP_HOST",
		"SMTP_PORT",
		"SMTP_USERNAME",
		"SMTP_PASSWORD",
		"SMTP_FROM",
	}
	for _, key := range envKeys {
		t.Run(key, func(t *testing.T) {
			clearEnv(t, map[string]string{key: "x"})

			_, err := Parse()
			if err == nil {
				t.Fatalf("Parse with only %s set returned nil error, want incomplete SMTP configuration error", key)
			}
			if !strings.Contains(err.Error(), "SMTP configuration is incomplete") {
				t.Errorf("error %q does not mention incomplete SMTP configuration", err)
			}
		})
	}
}

func TestRetentionDaysInvalid(t *testing.T) {
	clearEnv(t, map[string]string{"RETENTION_DAYS": "abc"})

	_, err := Parse()
	if err == nil {
		t.Fatal("Parse returned nil error, want error for invalid RETENTION_DAYS")
	}
	if !strings.Contains(err.Error(), "RETENTION_DAYS") {
		t.Errorf("error %q does not mention RETENTION_DAYS", err)
	}
}

func TestRetentionDaysBelowMinimum(t *testing.T) {
	for _, v := range []string{"0", "-5"} {
		t.Run(v, func(t *testing.T) {
			clearEnv(t, map[string]string{"RETENTION_DAYS": v})

			_, err := Parse()
			if err == nil {
				t.Fatalf("Parse with RETENTION_DAYS=%s returned nil error, want error", v)
			}
			if !strings.Contains(err.Error(), "at least 1") {
				t.Errorf("error %q does not mention minimum of 1", err)
			}
		})
	}
}

func TestRetentionDaysOne(t *testing.T) {
	clearEnv(t, map[string]string{"RETENTION_DAYS": "1"})

	s, err := Parse()
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if s.RetentionDays != 1 {
		t.Errorf("RetentionDays = %d, want 1", s.RetentionDays)
	}
}
