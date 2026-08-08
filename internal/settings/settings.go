package settings

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultPort          = "8080"
	DefaultDataDir       = "./data"
	DefaultRetentionDays = 30
)

type Settings struct {
	Port          string
	DataDir       string
	AdminPassword string
	SecretKey     string
	SecureCookies bool
	SMTP          SMTPConfig
	RetentionDays int
}

type SMTPConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
}

func Parse() (Settings, error) {
	s := Settings{
		Port:          envOr("PORT", DefaultPort),
		DataDir:       envOr("DATA_DIR", DefaultDataDir),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		SecretKey:     os.Getenv("SECRET_KEY"),
		RetentionDays: DefaultRetentionDays,
		SMTP: SMTPConfig{
			Host:     os.Getenv("SMTP_HOST"),
			Port:     os.Getenv("SMTP_PORT"),
			Username: os.Getenv("SMTP_USERNAME"),
			Password: os.Getenv("SMTP_PASSWORD"),
			From:     os.Getenv("SMTP_FROM"),
		},
	}

	if v := os.Getenv("RETENTION_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Settings{}, fmt.Errorf("RETENTION_DAYS must be a number, got %q", v)
		}
		s.RetentionDays = n
	}

	if v := os.Getenv("SECURE_COOKIES"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Settings{}, fmt.Errorf("SECURE_COOKIES must be true or false, got %q", v)
		}
		s.SecureCookies = b
	}

	if err := s.Validate(); err != nil {
		return Settings{}, err
	}

	if s.SecretKey == "" {
		s.SecretKey = generateSecretKey()
		slog.Warn("SECRET_KEY not set; generated a random one. Sessions will be invalidated on restart")
	}

	return s, nil
}

func (s Settings) Validate() error {
	if s.Port == "" {
		return errors.New("PORT must not be empty")
	}
	if s.DataDir == "" {
		return errors.New("DATA_DIR must not be empty")
	}
	if s.RetentionDays < 1 {
		return errors.New("RETENTION_DAYS must be at least 1")
	}
	return validateSMTP(s.SMTP)
}

func validateSMTP(s SMTPConfig) error {
	fields := []struct {
		name  string
		value string
	}{
		{"SMTP_HOST", s.Host},
		{"SMTP_PORT", s.Port},
		{"SMTP_USERNAME", s.Username},
		{"SMTP_PASSWORD", s.Password},
		{"SMTP_FROM", s.From},
	}

	set := 0
	var missing []string
	for _, f := range fields {
		if f.value != "" {
			set++
		} else {
			missing = append(missing, f.name)
		}
	}
	if set == 0 || set == len(fields) {
		return nil
	}
	return fmt.Errorf(
		"SMTP configuration is incomplete: if any SMTP_* variable is set, all must be set; missing: %s",
		strings.Join(missing, ", "),
	)
}

func generateSecretKey() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("crypto/rand failure: %v", err))
	}
	return hex.EncodeToString(buf)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
