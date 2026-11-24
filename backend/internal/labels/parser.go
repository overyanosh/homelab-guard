package labels

import (
	"strconv"
	"strings"
	"time"
)

type GuardConfig struct {
	Enabled         bool          `json:"enabled"`
	Group           string        `json:"group"`
	UpdateMode      string        `json:"update_mode"`
	UpdateSchedule  string        `json:"update_schedule"`
	TestStrategy    string        `json:"test_strategy"`
	TestEndpoint    string        `json:"test_endpoint"`
	TestCmd         string        `json:"test_cmd"`
	TestTimeout     time.Duration `json:"test_timeout"`
	AIContext       string        `json:"ai_context"`
	BackupPreUpdate bool          `json:"backup_pre_update"`
}

func ParseDockerLabels(raw map[string]string) GuardConfig {
	cfg := GuardConfig{
		Enabled:        false,
		Group:          "default",
		UpdateMode:     "notify",
		UpdateSchedule: "0 3 * * *",
		TestStrategy:   "healthcheck",
		TestTimeout:    60 * time.Second,
	}

	if val, ok := raw["guard.enable"]; ok {
		cfg.Enabled, _ = strconv.ParseBool(strings.TrimSpace(val))
	}
	if val, ok := raw["guard.group"]; ok && val != "" {
		cfg.Group = strings.TrimSpace(val)
	}
	if val, ok := raw["guard.update.mode"]; ok {
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "manual", "notify", "auto":
			cfg.UpdateMode = strings.ToLower(strings.TrimSpace(val))
		}
	}
	if val, ok := raw["guard.update.schedule"]; ok && val != "" {
		cfg.UpdateSchedule = strings.TrimSpace(val)
	}
	if val, ok := raw["guard.test.strategy"]; ok {
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "healthcheck", "http", "tcp", "exec":
			cfg.TestStrategy = strings.ToLower(strings.TrimSpace(val))
		}
	}
	if val, ok := raw["guard.test.endpoint"]; ok {
		cfg.TestEndpoint = strings.TrimSpace(val)
	}
	if val, ok := raw["guard.test.cmd"]; ok {
		cfg.TestCmd = strings.TrimSpace(val)
	}
	if val, ok := raw["guard.test.timeout"]; ok {
		if d, err := time.ParseDuration(strings.TrimSpace(val)); err == nil {
			cfg.TestTimeout = d
		}
	}
	if val, ok := raw["guard.ai.context"]; ok {
		cfg.AIContext = strings.TrimSpace(val)
	}
	if val, ok := raw["guard.backup.pre-update"]; ok {
		cfg.BackupPreUpdate, _ = strconv.ParseBool(strings.TrimSpace(val))
	}

	return cfg
}
