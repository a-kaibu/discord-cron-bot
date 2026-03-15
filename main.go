package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultConfigPath = "config.json"
	maxDiscordLength  = 2000
)

type scheduleEntry struct {
	keys []string
}

type jsonConfig struct {
	Schedules map[string][]string `json:"schedules"`
}

type messageCatalog struct {
	secrets map[string]string
	vars    map[string]string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		return fmt.Errorf("load JST timezone: %w", err)
	}

	now := time.Now().In(jst)
	configPath := getenv("CONFIG_FILE", defaultConfigPath)
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		return errors.New("DISCORD_WEBHOOK_URL is required")
	}

	schedules, err := loadSchedule(configPath)
	if err != nil {
		return err
	}

	messages, err := loadMessageCatalog(
		os.Getenv("ACTIONS_SECRETS_JSON"),
		os.Getenv("ACTIONS_VARS_JSON"),
	)
	if err != nil {
		return err
	}

	targetKeys := collectTodayMessageKeys(schedules, now)
	if len(targetKeys) == 0 {
		fmt.Printf("no message scheduled for %s\n", now.Format("2006-01-02"))
		return nil
	}

	client := &http.Client{Timeout: 15 * time.Second}
	for _, key := range targetKeys {
		content, ok := messages.lookup(key)
		if !ok {
			return fmt.Errorf("message key %q was scheduled but not found in GitHub Actions secrets or vars", key)
		}
		if err := postDiscordWebhook(client, webhookURL, content); err != nil {
			return fmt.Errorf("send %q: %w", key, err)
		}
		fmt.Printf("sent message for key=%s date=%s\n", key, now.Format("2006-01-02"))
	}

	return nil
}

func loadSchedule(path string) (map[string]scheduleEntry, error) {
	body, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}

	var cfg jsonConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", path, err)
	}

	result := make(map[string]scheduleEntry, len(cfg.Schedules))
	for key, messageKeys := range cfg.Schedules {
		if !isSupportedDateKey(key) {
			return nil, fmt.Errorf("unsupported date key %q in %s", key, path)
		}
		if len(messageKeys) == 0 {
			return nil, fmt.Errorf("date key %q in %s has no message keys", key, path)
		}
		normalized := make([]string, 0, len(messageKeys))
		for _, messageKey := range messageKeys {
			value := strings.TrimSpace(messageKey)
			if value != "" {
				normalized = append(normalized, value)
			}
		}
		if len(normalized) == 0 {
			return nil, fmt.Errorf("date key %q in %s has only empty message keys", key, path)
		}
		result[key] = scheduleEntry{keys: normalized}
	}

	return result, nil
}

func loadMessageCatalog(secretsJSON, varsJSON string) (messageCatalog, error) {
	secretsMap, err := parseActionContextJSON(secretsJSON)
	if err != nil {
		return messageCatalog{}, fmt.Errorf("parse ACTIONS_SECRETS_JSON: %w", err)
	}
	varsMap, err := parseActionContextJSON(varsJSON)
	if err != nil {
		return messageCatalog{}, fmt.Errorf("parse ACTIONS_VARS_JSON: %w", err)
	}

	return messageCatalog{
		secrets: secretsMap,
		vars:    varsMap,
	}, nil
}

func parseActionContextJSON(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]string{}, nil
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, err
	}
	if result == nil {
		return map[string]string{}, nil
	}
	return result, nil
}

func (c messageCatalog) lookup(key string) (string, bool) {
	for _, candidate := range candidateKeys(key) {
		if value, ok := c.secrets[candidate]; ok && strings.TrimSpace(value) != "" {
			return strings.ReplaceAll(value, `\n`, "\n"), true
		}
		if value, ok := c.vars[candidate]; ok && strings.TrimSpace(value) != "" {
			return strings.ReplaceAll(value, `\n`, "\n"), true
		}
	}
	return "", false
}

func candidateKeys(key string) []string {
	base := strings.TrimSpace(key)
	if base == "" {
		return nil
	}

	candidates := []string{base}
	upper := strings.ToUpper(base)
	if upper != base {
		candidates = append(candidates, upper)
	}
	return candidates
}

func collectTodayMessageKeys(schedules map[string]scheduleEntry, now time.Time) []string {
	dateKeys := []string{
		now.Format("2006-01-02"),
		now.Format("01-02"),
	}

	seen := make(map[string]struct{})
	var result []string
	for _, dateKey := range dateKeys {
		entry, ok := schedules[dateKey]
		if !ok {
			continue
		}
		for _, key := range entry.keys {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, key)
		}
	}
	return result
}

func postDiscordWebhook(client *http.Client, webhookURL, content string) error {
	if strings.TrimSpace(content) == "" {
		return errors.New("message content is empty")
	}
	if len(content) > maxDiscordLength {
		return fmt.Errorf("message length exceeds Discord limit (%d)", maxDiscordLength)
	}

	payload, err := json.Marshal(map[string]string{
		"content": content,
	})
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 == 2 {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("discord webhook returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
}

func isSupportedDateKey(value string) bool {
	if _, err := time.Parse("2006-01-02", value); err == nil {
		return true
	}
	if _, err := time.Parse("01-02", value); err == nil {
		return true
	}
	return false
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
