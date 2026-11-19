package config

import (
	"os"
	"strings"
	"testing"
)

var envKeys = []string{
	"APP_ENV", "PORT", "BASE_PATH", "PUBLIC_ORIGIN", "SYSTEM_NAME", "TRUSTED_PROXIES",
	"SESSION_SECRET", "SESSION_COOKIE_NAME", "SESSION_COOKIE_SECURE",
	"SESSION_TTL_DAYS", "SESSION_TOUCH_INTERVAL_SECONDS",
	"REGISTER_ENABLED", "INITIAL_ADMIN_USERNAME", "INITIAL_ADMIN_PASSWORD", "INITIAL_ADMIN_EMAIL",
	"SQL_DSN", "SQLITE_PATH", "DB_MAX_OPEN_CONNS",
	"EMAIL_VERIFICATION_ENABLED", "EMAIL_DEBUG_MODE", "ALLOW_EMAIL_DEBUG_IN_PRODUCTION",
	"SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM", "SMTP_TLS_MODE",
	"SMTP_TIMEOUT_SECONDS",
	"VERIFICATION_CODE_TTL_SECONDS", "VERIFICATION_MAX_ATTEMPTS",
	"VERIFICATION_RESEND_COOLDOWN_SECONDS", "VERIFICATION_IP_HOURLY_LIMIT",
	"GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET",
	"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET",
	"OIDC_ISSUER_URL", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "OIDC_DISPLAY_NAME", "OIDC_SCOPES",
	"OAUTH_REDIRECT_BASE",
	"LOGIN_RATE_LIMIT_PER_MINUTE", "REGISTER_RATE_LIMIT_PER_HOUR",
	"MODEL_LIBRARY_ENABLED", "MODEL_LIBRARY_API_URL",
	"MODEL_LIBRARY_CACHE_TTL_SECONDS", "MODEL_LIBRARY_REQUEST_TIMEOUT_SECONDS",
	"MODEL_LIBRARY_OVERVIEW_NAMESPACE",
	"DATASET_LIBRARY_ENABLED", "DATASET_LIBRARY_API_URL",
	"DATASET_LIBRARY_CACHE_TTL_SECONDS", "DATASET_LIBRARY_REQUEST_TIMEOUT_SECONDS",
	"S3_ENABLED", "S3_ENDPOINT", "S3_REGION", "S3_PATH_STYLE",
	"S3_CREDENTIALS_FILE", "S3_CREDENTIALS_PROFILE",
	"S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "S3_SESSION_TOKEN",
	"S3_PRESIGN_TTL_SECONDS", "S3_LIST_CACHE_TTL_SECONDS", "S3_REQUEST_TIMEOUT_SECONDS",
	"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY",
	"AWS_SESSION_TOKEN", "AWS_PROFILE", "AWS_SHARED_CREDENTIALS_FILE",
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range envKeys {
		t.Setenv(key, "")
	}
}

func TestLoadDevelopmentDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AppEnv != "development" || cfg.SessionSecret != DefaultDevSessionSecret {
		t.Fatalf("unexpected development config: %+v", cfg)
	}
	if !cfg.EmailVerificationEnabled || !cfg.EmailDebugMode {
		t.Fatalf("development defaults should enable verification and debug mail")
	}
	if cfg.PublicOrigin != "http://localhost:5173" {
		t.Fatalf("public origin = %q", cfg.PublicOrigin)
	}
	if cfg.OAuthRedirectBase != cfg.PublicOrigin {
		t.Fatalf("redirect base = %q", cfg.OAuthRedirectBase)
	}
	if cfg.APIPrefix != "/medrouter" {
		t.Fatalf("api prefix = %q", cfg.APIPrefix)
	}
	if cfg.APIPath() != "/medrouter/api" {
		t.Fatalf("api path = %q", cfg.APIPath())
	}
	if got := cfg.CallbackURL("github"); got != "http://localhost:5173/medrouter/api/oauth/github/callback" {
		t.Fatalf("callback = %q", got)
	}
	if got := cfg.FrontendPath("/dashboard"); got != "/dashboard" {
		t.Fatalf("frontend path = %q", got)
	}
}

func TestDatasetLibraryConfig(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.DatasetLibraryEnabled || cfg.DatasetLibraryAPIURL != "" {
		t.Fatalf("dataset defaults = %+v", cfg)
	}
	if cfg.DatasetLibraryCacheTTL.Seconds() != 30 || cfg.DatasetLibraryTimeout.Seconds() != 5 {
		t.Fatalf("dataset durations = %s/%s", cfg.DatasetLibraryCacheTTL, cfg.DatasetLibraryTimeout)
	}

	t.Setenv("DATASET_LIBRARY_ENABLED", "false")
	t.Setenv("DATASET_LIBRARY_API_URL", "http://127.0.0.1:8001/")
	t.Setenv("DATASET_LIBRARY_CACHE_TTL_SECONDS", "45")
	t.Setenv("DATASET_LIBRARY_REQUEST_TIMEOUT_SECONDS", "8")

	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatasetLibraryEnabled {
		t.Fatal("DATASET_LIBRARY_ENABLED=false was ignored")
	}
	if cfg.DatasetLibraryAPIURL != "http://127.0.0.1:8001" {
		t.Fatalf("dataset API URL = %q", cfg.DatasetLibraryAPIURL)
	}
	if cfg.DatasetLibraryCacheTTL.Seconds() != 45 || cfg.DatasetLibraryTimeout.Seconds() != 8 {
		t.Fatalf("dataset durations = %s/%s", cfg.DatasetLibraryCacheTTL, cfg.DatasetLibraryTimeout)
	}
}

func TestS3Config(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.S3Enabled || cfg.S3Region != "us-east-1" || cfg.S3PathStyle {
		t.Fatalf("S3 defaults = %+v", cfg)
	}
	if cfg.S3PresignTTL.Seconds() != 600 || cfg.S3ListCacheTTL.Seconds() != 20 || cfg.S3Timeout.Seconds() != 5 {
		t.Fatalf("S3 durations = %s/%s/%s", cfg.S3PresignTTL, cfg.S3ListCacheTTL, cfg.S3Timeout)
	}

	t.Setenv("S3_ENABLED", "true")
	t.Setenv("S3_ENDPOINT", "http://10.66.2.1:30880/")
	t.Setenv("S3_REGION", "cn-east-1")
	t.Setenv("S3_PATH_STYLE", "false")
	t.Setenv("S3_ACCESS_KEY_ID", "key")
	t.Setenv("S3_SECRET_ACCESS_KEY", "secret")
	t.Setenv("S3_SESSION_TOKEN", "token")
	t.Setenv("S3_PRESIGN_TTL_SECONDS", "900")
	t.Setenv("S3_LIST_CACHE_TTL_SECONDS", "15")
	t.Setenv("S3_REQUEST_TIMEOUT_SECONDS", "7")

	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.S3Enabled || cfg.S3Endpoint != "http://10.66.2.1:30880" || cfg.S3Region != "cn-east-1" || cfg.S3PathStyle {
		t.Fatalf("S3 config = %+v", cfg)
	}
	if cfg.S3PresignTTL.Seconds() != 900 || cfg.S3ListCacheTTL.Seconds() != 15 || cfg.S3Timeout.Seconds() != 7 {
		t.Fatalf("S3 durations = %s/%s/%s", cfg.S3PresignTTL, cfg.S3ListCacheTTL, cfg.S3Timeout)
	}

	credentials, err := cfg.LoadS3Credentials()
	if err != nil {
		t.Fatalf("LoadS3Credentials: %v", err)
	}
	if credentials.AccessKeyID != "key" || credentials.SecretAccessKey != "secret" || credentials.SessionToken != "token" {
		t.Fatalf("credentials = %+v", credentials)
	}
}

func TestS3AWSNativeConfig(t *testing.T) {
	clearEnv(t)

	// With no S3_ENDPOINT the client uses the standard AWS endpoint, so the
	// conventional AWS environment variables are accepted as fallbacks.
	t.Setenv("S3_ENABLED", "true")
	t.Setenv("AWS_REGION", "ap-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "aws-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")
	t.Setenv("AWS_SESSION_TOKEN", "aws-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.S3Endpoint != "" || cfg.S3Region != "ap-east-1" || cfg.S3PathStyle {
		t.Fatalf("AWS config = %+v", cfg)
	}
	credentials, err := cfg.LoadS3Credentials()
	if err != nil {
		t.Fatalf("LoadS3Credentials: %v", err)
	}
	if credentials.AccessKeyID != "aws-key" || credentials.SecretAccessKey != "aws-secret" || credentials.SessionToken != "aws-token" {
		t.Fatalf("credentials = %+v", credentials)
	}

	// S3_* takes precedence over AWS_*.
	t.Setenv("S3_REGION", "eu-west-1")
	t.Setenv("S3_ACCESS_KEY_ID", "s3-key")
	t.Setenv("S3_SECRET_ACCESS_KEY", "s3-secret")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.S3Region != "eu-west-1" || cfg.S3AccessKeyID != "s3-key" {
		t.Fatalf("S3_* should win: %+v", cfg)
	}
}

func TestLoadS3CredentialsFile(t *testing.T) {
	clearEnv(t)

	dir := t.TempDir()
	filename := dir + "/credentials"
	content := `# AWS shared credentials
[default]
aws_access_key_id = default-key
aws_secret_access_key = default-secret

[platform]
aws_access_key_id = platform-key
aws_secret_access_key = platform-secret
aws_session_token = platform-token
`
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	t.Setenv("S3_CREDENTIALS_FILE", filename)
	t.Setenv("S3_CREDENTIALS_PROFILE", "platform")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	credentials, err := cfg.LoadS3Credentials()
	if err != nil {
		t.Fatalf("LoadS3Credentials: %v", err)
	}
	if credentials.AccessKeyID != "platform-key" || credentials.SecretAccessKey != "platform-secret" || credentials.SessionToken != "platform-token" {
		t.Fatalf("credentials = %+v", credentials)
	}

	t.Setenv("S3_CREDENTIALS_PROFILE", "missing")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := cfg.LoadS3Credentials(); err == nil {
		t.Fatal("a missing profile should fail")
	}

	// Anonymous access when neither env nor a file is configured.
	clearEnv(t)
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if credentials, err := cfg.LoadS3Credentials(); err != nil || credentials.AccessKeyID != "" {
		t.Fatalf("anonymous credentials = %+v err=%v", credentials, err)
	}
}

func TestLoadBasePath(t *testing.T) {
	clearEnv(t)
	t.Setenv("BASE_PATH", "/medrouter/")
	t.Setenv("PUBLIC_ORIGIN", "https://example.com/")
	t.Setenv("OAUTH_REDIRECT_BASE", "https://auth.example.com/prefix/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BasePath != "/medrouter" {
		t.Fatalf("base path = %q", cfg.BasePath)
	}
	if cfg.PublicOrigin != "https://example.com" {
		t.Fatalf("public origin = %q", cfg.PublicOrigin)
	}
	if got := cfg.FrontendPath("sign-in"); got != "/medrouter/sign-in" {
		t.Fatalf("frontend path = %q", got)
	}
	if got := cfg.FrontendPath(""); got != "/medrouter/" {
		t.Fatalf("frontend root = %q", got)
	}
	if got := cfg.CallbackURL("google"); got != "https://auth.example.com/prefix/api/oauth/google/callback" {
		t.Fatalf("callback = %q", got)
	}
}

func TestLoadAPIPrefix(t *testing.T) {
	clearEnv(t)
	t.Setenv("API_PREFIX", "/medrouter/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIPrefix != "/medrouter" {
		t.Fatalf("api prefix = %q", cfg.APIPrefix)
	}
	if cfg.APIPath() != "/medrouter/api" {
		t.Fatalf("api path = %q", cfg.APIPath())
	}
	if got := cfg.CallbackURL("google"); got != "http://localhost:5173/medrouter/api/oauth/google/callback" {
		t.Fatalf("callback = %q", got)
	}

	// "/" puts the API directly under /api; the default is /medrouter even
	// when the site itself is served from another prefix.
	t.Setenv("API_PREFIX", "/")
	t.Setenv("BASE_PATH", "/site")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIPrefix != "" || cfg.APIPath() != "/api" {
		t.Fatalf("root api prefix = %q path = %q", cfg.APIPrefix, cfg.APIPath())
	}

	t.Setenv("API_PREFIX", "")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIPath() != "/medrouter/api" {
		t.Fatalf("default api path = %q", cfg.APIPath())
	}

	for _, bad := range []string{"/med/../api", "/med//api", "/med/./api"} {
		t.Setenv("API_PREFIX", bad)
		if _, err := Load(); err == nil {
			t.Fatalf("API_PREFIX=%q should be rejected", bad)
		}
	}
}

func TestProductionValidation(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"

	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "missing secret",
			env:     map[string]string{"APP_ENV": "production"},
			wantErr: "SESSION_SECRET is required",
		},
		{
			name:    "short secret",
			env:     map[string]string{"APP_ENV": "production", "SESSION_SECRET": "short"},
			wantErr: "at least 32 characters",
		},
		{
			name: "debug email in production",
			env: map[string]string{
				"APP_ENV": "production", "SESSION_SECRET": secret,
				"EMAIL_DEBUG_MODE": "true",
			},
			wantErr: "EMAIL_DEBUG_MODE must be false",
		},
		{
			name: "missing smtp host",
			env: map[string]string{
				"APP_ENV": "production", "SESSION_SECRET": secret,
				"EMAIL_DEBUG_MODE": "false", "EMAIL_VERIFICATION_ENABLED": "true",
			},
			wantErr: "SMTP_HOST is required",
		},
		{
			name: "valid with smtp",
			env: map[string]string{
				"APP_ENV": "production", "SESSION_SECRET": secret,
				"EMAIL_DEBUG_MODE": "false", "EMAIL_VERIFICATION_ENABLED": "true",
				"SMTP_HOST": "smtp.example.com",
			},
		},
		{
			name: "debug explicitly allowed",
			env: map[string]string{
				"APP_ENV": "production", "SESSION_SECRET": secret,
				"EMAIL_DEBUG_MODE": "true", "ALLOW_EMAIL_DEBUG_IN_PRODUCTION": "true",
			},
		},
		{
			name: "verification disabled needs no smtp",
			env: map[string]string{
				"APP_ENV": "production", "SESSION_SECRET": secret,
				"EMAIL_DEBUG_MODE": "false", "EMAIL_VERIFICATION_ENABLED": "false",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			cfg, err := Load()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				if !cfg.IsProduction() {
					t.Fatalf("IsProduction() = false")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Load error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
