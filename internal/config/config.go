package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	// DefaultDevSessionSecret is only used when APP_ENV is not production.
	// It keeps sessions valid across restarts during development.
	DefaultDevSessionSecret = "medrouter-dev-only-session-secret-change-me"
	Version                 = "0.1.0"
)

type Config struct {
	AppEnv string
	Port   string
	// BasePath is the site prefix ("" at the root). APIPrefix is the prefix the
	// API lives under; the API is mounted at APIPrefix + "/api", so the default
	// "/medrouter" exposes it at /medrouter/api. "/" (normalized to "") puts it
	// at /api.
	BasePath       string
	APIPrefix      string
	PublicOrigin   string
	SystemName     string
	Version        string
	TrustedProxies []string

	SessionSecret        string
	SessionCookieName    string
	SessionCookieSecure  bool
	SessionTTL           time.Duration
	SessionTouchInterval time.Duration

	RegisterEnabled   bool
	InitialAdminUser  string
	InitialAdminPass  string
	InitialAdminEmail string

	SQLDSN         string
	SQLitePath     string
	DBMaxOpenConns int

	EmailVerificationEnabled bool
	EmailDebugMode           bool
	AllowEmailDebugInProd    bool
	SMTPHost                 string
	SMTPPort                 int
	SMTPUsername             string
	SMTPPassword             string
	SMTPFrom                 string
	SMTPTLSMode              string
	SMTPTimeout              time.Duration

	VerificationCodeTTL     time.Duration
	VerificationMaxAttempts int
	VerificationResend      time.Duration
	VerificationIPHourly    int

	GitHubClientID     string
	GitHubClientSecret string

	GoogleClientID     string
	GoogleClientSecret string

	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCDisplayName  string
	OIDCScopes       string

	OAuthRedirectBase string

	LoginRatePerMinute  int
	RegisterRatePerHour int

	ModelLibraryEnabled  bool
	ModelLibraryAPIURL   string
	ModelLibraryCacheTTL time.Duration
	ModelLibraryTimeout  time.Duration
	// ModelLibraryOverviewNamespace is where model overview ConfigMaps live
	// (default cubestack-system). It is read-only data for the overview tab.
	ModelLibraryOverviewNamespace string

	DatasetLibraryEnabled  bool
	DatasetLibraryAPIURL   string
	DatasetLibraryCacheTTL time.Duration
	DatasetLibraryTimeout  time.Duration

	// S3 powers the dataset and model file browser. Data itself stays in the
	// object store; MedRouter only lists keys and signs download URLs.
	S3Enabled            bool
	S3Endpoint           string
	S3PublicEndpoint     string
	S3Region             string
	S3PathStyle          bool
	S3CredentialsFile    string
	S3CredentialsProfile string
	S3AccessKeyID        string
	S3SecretAccessKey    string
	S3SessionToken       string
	S3PresignTTL         time.Duration
	S3ListCacheTTL       time.Duration
	S3HeadCacheTTL       time.Duration
	S3Timeout            time.Duration
	// S3StreamTimeout bounds one streaming response (preview content or a
	// proxied download). It is a write deadline, not a read timeout.
	S3StreamTimeout time.Duration
	// S3DownloadMode is "presign" (browser talks to the object store) or
	// "proxy" (bytes stream through MedRouter).
	S3DownloadMode string

	// S3Preview* control the in-browser preview of a single file.
	S3PreviewEnabled     bool
	S3PreviewTextWindow  int64
	S3PreviewImageMax    int64
	S3PreviewPDFMax      int64
	S3PreviewConcurrency int

	// S3Download* bound downloads. A zero limit means unlimited.
	S3DownloadMax            int64
	S3DownloadDailyQuota     int64
	S3DownloadLargeThreshold int64
	S3DownloadLargeTTL       time.Duration

	// Studio launches one-click workspaces from favorites. DryRun computes and
	// returns the plan without creating a Custom Resource, which lets the
	// resource matching be validated before write access is granted.
	StudioEnabled           bool
	StudioDryRun            bool
	StudioNamespace         string
	StudioNotebookImage     string
	StudioDatasetPVCPattern string
	StudioMaxWorkspaces     int
	StudioReconcileInterval time.Duration
	StudioTimeout           time.Duration

	// Fine-tuning runs a batch Job from a fixed recipe: one accelerator, a
	// baked image, and the adapter written under OutputPath.
	StudioFinetuneImage    string
	StudioFinetuneOutput   string
	StudioFinetuneDeadline time.Duration
	// StudioFinetuneMaxSamples caps the training rows. A single MXC550 runs
	// about 2 samples/s, so the full 182k-row MedMCQA set would need ~50h.
	StudioFinetuneMaxSamples int
	// StudioGatewayRouter is the AI gateway router an InferenceService
	// publishes through.
	StudioGatewayRouter string
}

// defaultFinetuneImage is pinned by digest: a fine-tune must run the exact
// stack it was validated against, not whatever a floating tag points at now.
const defaultFinetuneImage = "harbor.isuanova.com/clyang/medrouter-lora@sha256:88ebff1ee999637a2da0efaf893de03418d8fe7fca0e40f134c35198d6fa2f78"

// Load reads configuration from the environment. A .env file in the working
// directory is loaded when present (development convenience).
func Load() (*Config, error) {
	_ = godotenv.Load()

	appEnv := strings.ToLower(getenv("APP_ENV", "development"))

	basePath := normalizeBasePath(os.Getenv("BASE_PATH"))
	apiPrefix, err := normalizeAPIPrefix(os.Getenv("API_PREFIX"))
	if err != nil {
		return nil, err
	}

	// An empty S3 endpoint means the standard AWS S3 endpoint for the region.
	// Custom endpoints (Ceph RGW, MinIO) default to path-style addressing;
	// real AWS prefers virtual-hosted addressing.
	s3Endpoint := strings.TrimRight(strings.TrimSpace(os.Getenv("S3_ENDPOINT")), "/")
	s3PathStyleDefault := s3Endpoint != ""

	cfg := &Config{
		AppEnv:         appEnv,
		Port:           getenv("PORT", "3000"),
		BasePath:       basePath,
		APIPrefix:      apiPrefix,
		PublicOrigin:   strings.TrimRight(getenv("PUBLIC_ORIGIN", "http://localhost:5173"), "/"),
		SystemName:     getenv("SYSTEM_NAME", "MedRouter"),
		Version:        Version,
		TrustedProxies: splitCSV(os.Getenv("TRUSTED_PROXIES")),

		SessionSecret:        os.Getenv("SESSION_SECRET"),
		SessionCookieName:    getenv("SESSION_COOKIE_NAME", "medrouter_session"),
		SessionCookieSecure:  boolEnv("SESSION_COOKIE_SECURE", appEnv == "production"),
		SessionTTL:           daysEnv("SESSION_TTL_DAYS", 30),
		SessionTouchInterval: secondsEnv("SESSION_TOUCH_INTERVAL_SECONDS", 3600),

		RegisterEnabled:   boolEnv("REGISTER_ENABLED", true),
		InitialAdminUser:  os.Getenv("INITIAL_ADMIN_USERNAME"),
		InitialAdminPass:  os.Getenv("INITIAL_ADMIN_PASSWORD"),
		InitialAdminEmail: os.Getenv("INITIAL_ADMIN_EMAIL"),

		SQLDSN:         strings.TrimSpace(os.Getenv("SQL_DSN")),
		SQLitePath:     getenv("SQLITE_PATH", "data/medrouter.db"),
		DBMaxOpenConns: intEnv("DB_MAX_OPEN_CONNS", 10),

		EmailVerificationEnabled: boolEnv("EMAIL_VERIFICATION_ENABLED", true),
		EmailDebugMode:           boolEnv("EMAIL_DEBUG_MODE", appEnv != "production"),
		AllowEmailDebugInProd:    boolEnv("ALLOW_EMAIL_DEBUG_IN_PRODUCTION", false),
		SMTPHost:                 os.Getenv("SMTP_HOST"),
		SMTPPort:                 intEnv("SMTP_PORT", 465),
		SMTPUsername:             os.Getenv("SMTP_USERNAME"),
		SMTPPassword:             os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:                 getenv("SMTP_FROM", "MedRouter <no-reply@medrouter.local>"),
		SMTPTLSMode:              strings.ToLower(getenv("SMTP_TLS_MODE", "implicit")),
		SMTPTimeout:              secondsEnv("SMTP_TIMEOUT_SECONDS", 10),

		VerificationCodeTTL:     secondsEnv("VERIFICATION_CODE_TTL_SECONDS", 600),
		VerificationMaxAttempts: intEnv("VERIFICATION_MAX_ATTEMPTS", 5),
		VerificationResend:      secondsEnv("VERIFICATION_RESEND_COOLDOWN_SECONDS", 60),
		VerificationIPHourly:    intEnv("VERIFICATION_IP_HOURLY_LIMIT", 10),

		GitHubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),

		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),

		OIDCIssuerURL:    strings.TrimRight(os.Getenv("OIDC_ISSUER_URL"), "/"),
		OIDCClientID:     os.Getenv("OIDC_CLIENT_ID"),
		OIDCClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		OIDCDisplayName:  getenv("OIDC_DISPLAY_NAME", "SSO"),
		OIDCScopes:       getenv("OIDC_SCOPES", "openid email profile"),

		OAuthRedirectBase: strings.TrimRight(os.Getenv("OAUTH_REDIRECT_BASE"), "/"),

		LoginRatePerMinute:  intEnv("LOGIN_RATE_LIMIT_PER_MINUTE", 10),
		RegisterRatePerHour: intEnv("REGISTER_RATE_LIMIT_PER_HOUR", 5),

		ModelLibraryEnabled:           boolEnv("MODEL_LIBRARY_ENABLED", true),
		ModelLibraryAPIURL:            strings.TrimRight(strings.TrimSpace(os.Getenv("MODEL_LIBRARY_API_URL")), "/"),
		ModelLibraryCacheTTL:          secondsEnv("MODEL_LIBRARY_CACHE_TTL_SECONDS", 30),
		ModelLibraryOverviewNamespace: overviewNamespace(os.Getenv("MODEL_LIBRARY_OVERVIEW_NAMESPACE")),
		ModelLibraryTimeout:           secondsEnv("MODEL_LIBRARY_REQUEST_TIMEOUT_SECONDS", 5),

		DatasetLibraryEnabled:  boolEnv("DATASET_LIBRARY_ENABLED", true),
		DatasetLibraryAPIURL:   strings.TrimRight(strings.TrimSpace(os.Getenv("DATASET_LIBRARY_API_URL")), "/"),
		DatasetLibraryCacheTTL: secondsEnv("DATASET_LIBRARY_CACHE_TTL_SECONDS", 30),
		DatasetLibraryTimeout:  secondsEnv("DATASET_LIBRARY_REQUEST_TIMEOUT_SECONDS", 5),

		S3Enabled:            boolEnv("S3_ENABLED", false),
		S3Endpoint:           s3Endpoint,
		S3PublicEndpoint:     strings.TrimRight(strings.TrimSpace(os.Getenv("S3_PUBLIC_ENDPOINT")), "/"),
		S3Region:             firstNonEmpty(os.Getenv("S3_REGION"), os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), "us-east-1"),
		S3PathStyle:          boolEnv("S3_PATH_STYLE", s3PathStyleDefault),
		S3CredentialsFile:    firstNonEmpty(os.Getenv("S3_CREDENTIALS_FILE"), os.Getenv("AWS_SHARED_CREDENTIALS_FILE")),
		S3CredentialsProfile: firstNonEmpty(os.Getenv("S3_CREDENTIALS_PROFILE"), os.Getenv("AWS_PROFILE"), "default"),
		S3AccessKeyID:        firstNonEmpty(os.Getenv("S3_ACCESS_KEY_ID"), os.Getenv("AWS_ACCESS_KEY_ID")),
		S3SecretAccessKey:    firstNonEmpty(os.Getenv("S3_SECRET_ACCESS_KEY"), os.Getenv("AWS_SECRET_ACCESS_KEY")),
		S3SessionToken:       firstNonEmpty(os.Getenv("S3_SESSION_TOKEN"), os.Getenv("AWS_SESSION_TOKEN")),
		S3PresignTTL:         secondsEnv("S3_PRESIGN_TTL_SECONDS", 600),
		S3ListCacheTTL:       secondsEnv("S3_LIST_CACHE_TTL_SECONDS", 20),
		S3HeadCacheTTL:       secondsEnv("S3_HEAD_CACHE_TTL_SECONDS", 30),
		S3Timeout:            secondsEnv("S3_REQUEST_TIMEOUT_SECONDS", 5),
		S3StreamTimeout:      secondsEnv("S3_STREAM_TIMEOUT_SECONDS", 300),
		S3DownloadMode:       downloadModeEnv(os.Getenv("S3_DOWNLOAD_MODE")),

		S3PreviewEnabled:     boolEnv("S3_PREVIEW_ENABLED", true),
		S3PreviewTextWindow:  bytesEnv("S3_PREVIEW_TEXT_WINDOW_BYTES", 256<<10),
		S3PreviewImageMax:    bytesEnv("S3_PREVIEW_IMAGE_MAX_BYTES", 10<<20),
		S3PreviewPDFMax:      bytesEnv("S3_PREVIEW_PDF_MAX_BYTES", 32<<20),
		S3PreviewConcurrency: intEnv("S3_PREVIEW_CONCURRENCY", 16),

		S3DownloadMax:            bytesEnv("S3_DOWNLOAD_MAX_BYTES", 50<<30),
		S3DownloadDailyQuota:     bytesEnv("S3_DOWNLOAD_DAILY_QUOTA_BYTES", 100<<30),
		S3DownloadLargeThreshold: bytesEnv("S3_DOWNLOAD_LARGE_THRESHOLD_BYTES", 2<<30),
		S3DownloadLargeTTL:       secondsEnv("S3_DOWNLOAD_LARGE_TTL_SECONDS", 3600),

		StudioEnabled:            boolEnv("STUDIO_ENABLED", false),
		StudioDryRun:             boolEnv("STUDIO_DRY_RUN", true),
		StudioNamespace:          getenv("STUDIO_NAMESPACE", "medrouter-workspace"),
		StudioNotebookImage:      getenv("STUDIO_NOTEBOOK_IMAGE", "harbor.isuanova.com/suanova/jupyter-minimal:latest"),
		StudioDatasetPVCPattern:  getenv("STUDIO_DATASET_PVC_PATTERN", "%s-data"),
		StudioMaxWorkspaces:      intEnv("STUDIO_MAX_WORKSPACES", 3),
		StudioReconcileInterval:  secondsEnv("STUDIO_RECONCILE_INTERVAL_SECONDS", 3),
		StudioTimeout:            secondsEnv("STUDIO_REQUEST_TIMEOUT_SECONDS", 10),
		StudioFinetuneImage:      getenv("STUDIO_FINETUNE_IMAGE", defaultFinetuneImage),
		StudioFinetuneDeadline:   secondsEnv("STUDIO_FINETUNE_DEADLINE_SECONDS", 43200),
		StudioFinetuneMaxSamples: intEnv("STUDIO_FINETUNE_MAX_SAMPLES", 20000),
		StudioGatewayRouter:      getenv("STUDIO_AI_GATEWAY_ROUTER", "cubestack-ai-gateway-private"),
	}

	if cfg.SessionSecret == "" {
		if appEnv == "production" {
			return nil, fmt.Errorf("SESSION_SECRET is required in production")
		}
		cfg.SessionSecret = DefaultDevSessionSecret
	}
	if appEnv == "production" && len(cfg.SessionSecret) < 32 {
		return nil, fmt.Errorf("SESSION_SECRET must be at least 32 characters in production")
	}
	if appEnv == "production" && cfg.EmailDebugMode && !cfg.AllowEmailDebugInProd {
		return nil, fmt.Errorf("EMAIL_DEBUG_MODE must be false in production (set ALLOW_EMAIL_DEBUG_IN_PRODUCTION=true to override)")
	}
	if appEnv == "production" && cfg.EmailVerificationEnabled && !cfg.EmailDebugMode && cfg.SMTPHost == "" {
		return nil, fmt.Errorf("SMTP_HOST is required when EMAIL_VERIFICATION_ENABLED=true in production")
	}
	if cfg.OAuthRedirectBase == "" {
		cfg.OAuthRedirectBase = cfg.PublicOrigin + cfg.BasePath
	}
	// A text preview window above 1 MiB stops being a preview: it is then a
	// partial download, and the response would carry that much JSON.
	if cfg.S3PreviewTextWindow > 1<<20 {
		cfg.S3PreviewTextWindow = 1 << 20
	}

	return cfg, nil
}

func (c *Config) IsProduction() bool { return c.AppEnv == "production" }

// CallbackURL returns the OAuth callback registered with the provider.
//
// OAuthRedirectBase is the origin plus site prefix (it defaults to
// PublicOrigin + BasePath). The API path is expressed relative to that base,
// so with the default API_PREFIX=/medrouter and BasePath="" the callback is
// <origin>/medrouter/api/oauth/...; when the site itself is at /medrouter the
// relative API path is /api and the result stays <origin>/medrouter/api/....
func (c *Config) CallbackURL(provider string) string {
	return fmt.Sprintf("%s%s/oauth/%s/callback", c.OAuthRedirectBase, c.apiPath(), provider)
}

// APIPath is the absolute path the API routes are mounted at.
func (c *Config) APIPath() string {
	return c.APIPrefix + "/api"
}

// apiPath is the API path relative to OAuthRedirectBase, which already carries
// BASE_PATH.
func (c *Config) apiPath() string {
	return strings.TrimPrefix(c.APIPath(), c.BasePath)
}

// FrontendPath prefixes an absolute frontend path with BASE_PATH.
func (c *Config) FrontendPath(path string) string {
	if path == "" {
		return c.BasePath + "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.BasePath + path
}

func normalizeBasePath(raw string) string {
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	if raw == "" {
		return ""
	}
	return "/" + raw
}

// normalizeAPIPrefix resolves API_PREFIX, defaulting to "/medrouter" so the
// API lives at /medrouter/api. The result is a prefix with no trailing slash
// ("/medrouter"), or "" for the root, where the API is at /api.
func normalizeAPIPrefix(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/medrouter", nil
	}
	clean := strings.Trim(raw, "/")
	if clean == "" {
		// "/" means the API sits directly under /api.
		return "", nil
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("API_PREFIX must not contain empty, . or .. path segments, got %q", raw)
		}
	}
	return "/" + clean, nil
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// firstNonEmpty returns the first non-blank value. It is used for settings
// that accept both the MedRouter name and the conventional AWS name.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func boolEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

func intEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func secondsEnv(key string, fallback int) time.Duration {
	return time.Duration(intEnv(key, fallback)) * time.Second
}

// bytesEnv reads a byte count. Zero is a meaningful value (unlimited), so it
// is accepted instead of being replaced by the fallback the way intEnv does.
func bytesEnv(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

// downloadModeEnv normalizes S3_DOWNLOAD_MODE. Anything that is not "proxy"
// falls back to presigned direct downloads.
func downloadModeEnv(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), "proxy") {
		return "proxy"
	}
	return "presign"
}

func daysEnv(key string, fallback int) time.Duration {
	return time.Duration(intEnv(key, fallback)) * 24 * time.Hour
}

// overviewNamespace returns the namespace holding model overview ConfigMaps.
// It defaults to cubestack-system; an empty value falls back to the default.
func overviewNamespace(raw string) string {
	v := strings.TrimSpace(strings.TrimRight(raw, "/"))
	if v == "" {
		return "cubestack-system"
	}
	return v
}
