// Package config reads the process configuration from the environment
// (12-factor). Every setting has one name, documented in deploy/.env.example.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the whole process configuration.
type Config struct {
	Environment   string // development | production
	HTTPAddr      string
	PublicURL     string // https://roadpass.sensifai.com, used in NGSI-LD Link headers and invitation links
	DatabaseURL   string
	ClickHouseURL string
	RedisURL      string
	SigningKey    string // SIGNING_MASTER_KEY, 32 bytes hex/base64
	DemoMode      bool
	DemoPublic    bool
	// The public demo workspace (Capital Paving): created on start, rebuilt daily.
	DemoTenant        bool
	DemoPassword      string // shared password of its users
	DemoResetHour     int    // UTC hour of the daily rebuild
	DemoRecordsPerDay int    // records visitors may add per day
	DemoWritesPerDay  int    // write requests per day for the workspace
	DemoWritesPerIP   int    // write requests per day from one address
	SessionTTL        time.Duration
	FabricDir         string // fabric/ (crypto material); empty disables the ledger
	FabricPeer        string
	FabricChannel     string
	FabricOrg         string
	SealInterval      time.Duration
	SealMax           int
	WorkerName        string
	// OpenBao / Vault Transit holding signing keys (VAULT_ADDR empty: keys are
	// sealed in the database under SIGNING_MASTER_KEY, development only).
	VaultAddr      string
	VaultToken     string
	VaultTokenFile string
	VaultMount     string
	VaultKVMount   string // key-value store for secrets the service reads back
	// Notifications: the operator's mail relay and SMS account (organisations
	// may bring their own per channel), and outbound-call policy.
	SMTPHost, SMTPPort, SMTPUser, SMTPPassword, SMTPFrom, SMTPTLS string
	TwilioSID, TwilioToken, TwilioFrom, TwilioBase                string
	TelegramBase                                                  string
	AlertsAllowPrivate                                            bool // webhooks to private addresses (local stacks only)
	AlertsInterval                                                time.Duration
	PushContact                                                   string
	LogFormat                                                     string
	APIRatePerMin                                                 int
	TileURL                                                       string // map tiles, e.g. https://tile.openstreetmap.org/{z}/{x}/{y}.png ("" = outline maps only)
	TileAttribution                                               string
	// Cloudflare Turnstile on the public contact form (both empty: no challenge).
	TurnstileSiteKey, TurnstileSecret, TurnstileVerifyURL string
}

// fileOrEnv reads K, or the file named by K_FILE (Docker/Kubernetes secrets).
func fileOrEnv(k string) string {
	if f := os.Getenv(k + "_FILE"); f != "" {
		if b, err := os.ReadFile(f); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return os.Getenv(k)
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func envDur(k string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func envBool(k string) bool {
	v := strings.ToLower(os.Getenv(k))
	return v == "1" || v == "true" || v == "yes"
}

// Load reads and validates the configuration.
func Load() (Config, error) {
	host, _ := os.Hostname()
	addr := env("HTTP_ADDR", ":8080")
	if p := os.Getenv("PORT"); p != "" { // the PaaS contract wins
		addr = ":" + p
	}
	c := Config{
		Environment:        env("ENVIRONMENT", "development"),
		HTTPAddr:           addr,
		PublicURL:          strings.TrimRight(env("PUBLIC_URL", "http://localhost:8080"), "/"),
		DatabaseURL:        env("DATABASE_URL", "postgres://roadpass:roadpass@127.0.0.1:15433/roadpass?sslmode=disable"),
		ClickHouseURL:      env("CLICKHOUSE_URL", "clickhouse://roadpass:roadpass@127.0.0.1:19100/roadpass"),
		RedisURL:           env("REDIS_URL", "redis://127.0.0.1:16379/0"),
		SigningKey:         os.Getenv("SIGNING_MASTER_KEY"),
		DemoMode:           envBool("DEMO_MODE"),
		DemoPublic:         envBool("DEMO_MODE_PUBLIC"),
		DemoTenant:         envBool("DEMO_TENANT"),
		DemoPassword:       fileOrEnv("DEMO_TENANT_PASSWORD"),
		DemoResetHour:      envInt("DEMO_RESET_HOUR_UTC", 3),
		DemoRecordsPerDay:  envInt("DEMO_RECORDS_PER_DAY", 60),
		DemoWritesPerDay:   envInt("DEMO_WRITES_PER_DAY", 400),
		DemoWritesPerIP:    envInt("DEMO_WRITES_PER_IP", 120),
		SessionTTL:         envDur("SESSION_TTL", 12*time.Hour),
		FabricDir:          os.Getenv("FABRIC_DIR"),
		FabricPeer:         env("FABRIC_PEER", "127.0.0.1:7051"),
		FabricChannel:      env("FABRIC_CHANNEL", "roadpass"),
		FabricOrg:          env("FABRIC_ORG", "sensifai"),
		SealInterval:       envDur("SEAL_INTERVAL", 30*time.Second),
		SealMax:            envInt("SEAL_MAX_LEAVES", 4096),
		WorkerName:         env("WORKER_NAME", host),
		VaultAddr:          os.Getenv("VAULT_ADDR"),
		VaultToken:         os.Getenv("VAULT_TOKEN"),
		VaultTokenFile:     os.Getenv("VAULT_TOKEN_FILE"),
		VaultMount:         env("VAULT_TRANSIT_MOUNT", "transit"),
		VaultKVMount:       env("VAULT_KV_MOUNT", "roadpass-kv"),
		SMTPHost:           os.Getenv("SMTP_HOST"),
		SMTPPort:           os.Getenv("SMTP_PORT"),
		SMTPUser:           os.Getenv("SMTP_USERNAME"),
		SMTPPassword:       fileOrEnv("SMTP_PASSWORD"),
		SMTPFrom:           os.Getenv("SMTP_FROM"),
		SMTPTLS:            env("SMTP_TLS", "starttls"),
		TwilioSID:          os.Getenv("TWILIO_ACCOUNT_SID"),
		TwilioToken:        fileOrEnv("TWILIO_AUTH_TOKEN"),
		TwilioFrom:         os.Getenv("TWILIO_FROM"),
		TwilioBase:         os.Getenv("TWILIO_API_BASE"),
		TelegramBase:       os.Getenv("TELEGRAM_API_BASE"),
		AlertsAllowPrivate: envBool("ALERTS_ALLOW_PRIVATE"),
		AlertsInterval:     envDur("ALERTS_INTERVAL", 30*time.Second),
		PushContact:        env("PUSH_CONTACT", "mailto:alerts@roadpass.invalid"),
		LogFormat:          env("LOG_FORMAT", "text"),
		APIRatePerMin:      envInt("API_RATE_PER_MIN", 600),
		TileURL:            tileURL(env("MAP_TILE_URL", "https://tile.openstreetmap.org/{z}/{x}/{y}.png")),
		TileAttribution:    env("MAP_TILE_ATTRIBUTION", "© OpenStreetMap contributors"),
		TurnstileSiteKey:   os.Getenv("TURNSTILE_SITE_KEY"),
		TurnstileSecret:    fileOrEnv("TURNSTILE_SECRET_KEY"),
		TurnstileVerifyURL: env("TURNSTILE_VERIFY_URL", "https://challenges.cloudflare.com/turnstile/v0/siteverify"),
	}
	switch c.Environment {
	case "development", "production":
	default:
		return c, fmt.Errorf("ENVIRONMENT must be development or production, got %q", c.Environment)
	}
	if c.SigningKey == "" {
		if c.Production() {
			return c, fmt.Errorf("SIGNING_MASTER_KEY is required in production")
		}
		// A fixed development key: signatures made in development are not meant to
		// be trusted anywhere else.
		c.SigningKey = strings.Repeat("d0", 32)
	}
	if c.DemoPassword == "" {
		c.DemoPassword = "roadpass-demo-2026"
	}
	if c.DemoResetHour < 0 || c.DemoResetHour > 23 {
		return c, fmt.Errorf("DEMO_RESET_HOUR_UTC must be 0..23")
	}
	if c.DemoMode && c.Production() && !c.DemoPublic {
		return c, fmt.Errorf("DEMO_MODE in production also needs DEMO_MODE_PUBLIC=1 (it lets anyone sign in as a demo user)")
	}
	if c.Production() && c.VaultAddr == "" {
		return c, fmt.Errorf("VAULT_ADDR is required in production: signing keys live in OpenBao Transit")
	}
	if c.VaultAddr != "" && c.VaultToken == "" && c.VaultTokenFile == "" {
		return c, fmt.Errorf("VAULT_ADDR needs VAULT_TOKEN or VAULT_TOKEN_FILE")
	}
	if c.Production() && c.AlertsAllowPrivate {
		return c, fmt.Errorf("ALERTS_ALLOW_PRIVATE lets webhooks reach private networks; it is for local stacks only")
	}
	if c.SealMax < 1 || c.SealMax > 4096 {
		return c, fmt.Errorf("SEAL_MAX_LEAVES must be 1..4096")
	}
	return c, nil
}

// Production reports whether this is a production deployment.
func (c Config) Production() bool { return c.Environment == "production" }

// LedgerEnabled reports whether a Fabric gateway is configured.
func (c Config) LedgerEnabled() bool { return c.FabricDir != "" }

// tileURL: "off" disables street maps (air-gapped installs); maps then fall back to outlines.
func tileURL(v string) string {
	if strings.EqualFold(v, "off") || strings.EqualFold(v, "none") {
		return ""
	}
	return v
}
