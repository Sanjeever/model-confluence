package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Command string

const (
	CommandServe         Command = "serve"
	CommandResetPassword Command = "reset-password"
)

type Config struct {
	Command               Command
	ListenAddress         string
	DatabasePath          string
	AdminPassword         string
	TrustedProxyCIDRs     []string
	ConnectTimeout        time.Duration
	ResponseHeaderTimeout time.Duration
	StreamIdleTimeout     time.Duration
	MaxRequestBytes       int64
}

func Load(args []string) (Config, error) {
	command := CommandServe
	if len(args) >= 2 && args[0] == "admin" && args[1] == "reset-password" {
		command = CommandResetPassword
		args = args[2:]
	}

	defaultDataDir := os.Getenv("MODEL_CONFLUENCE_DATA_DIR")
	if defaultDataDir == "" {
		defaultDataDir = "data"
	}

	set := flag.NewFlagSet("model-confluence", flag.ContinueOnError)
	listen := set.String("listen", envOr("MODEL_CONFLUENCE_LISTEN", "127.0.0.1:8080"), "HTTP listen address")
	dataDir := set.String("data-dir", defaultDataDir, "data directory")
	password := set.String("admin-password", os.Getenv("MODEL_CONFLUENCE_ADMIN_PASSWORD"), "initial or replacement admin password")
	trustedProxies := set.String("trusted-proxies", os.Getenv("MODEL_CONFLUENCE_TRUSTED_PROXIES"), "comma-separated trusted proxy CIDRs")
	connectTimeout := set.Duration("connect-timeout", 10*time.Second, "upstream connect timeout")
	responseHeaderTimeout := set.Duration("response-header-timeout", 5*time.Minute, "upstream response header timeout")
	streamIdleTimeout := set.Duration("stream-idle-timeout", 5*time.Minute, "upstream stream idle timeout")
	maxRequestBytes := set.Int64("max-request-bytes", 64<<20, "maximum inbound request body size")
	if err := set.Parse(args); err != nil {
		return Config{}, err
	}

	if command == CommandResetPassword && *password == "" {
		return Config{}, errors.New("admin reset-password requires --admin-password or MODEL_CONFLUENCE_ADMIN_PASSWORD")
	}
	if *maxRequestBytes <= 0 {
		return Config{}, errors.New("max-request-bytes must be positive")
	}

	absDataDir, err := filepath.Abs(*dataDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve data directory: %w", err)
	}

	return Config{
		Command:               command,
		ListenAddress:         *listen,
		DatabasePath:          filepath.Join(absDataDir, "model-confluence.db"),
		AdminPassword:         *password,
		TrustedProxyCIDRs:     splitCSV(*trustedProxies),
		ConnectTimeout:        *connectTimeout,
		ResponseHeaderTimeout: *responseHeaderTimeout,
		StreamIdleTimeout:     *streamIdleTimeout,
		MaxRequestBytes:       *maxRequestBytes,
	}, nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
