// Package projectconfig handles declarative project configuration manifests
// (volcano-config.yaml). The CLI is a thin client: it parses the manifest,
// interpolates ${ENV} references, and uploads it to the server, which owns
// validation and reconciliation.
package projectconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// ManifestVersion is the only currently supported manifest schema version.
	ManifestVersion = 1

	manifestDir        = "volcano"
	nestedManifestPath = "volcano/volcano-config.yaml"
	rootManifestPath   = "volcano-config.yaml"
)

// Manifest is the on-disk shape of volcano-config.yaml. Field names mirror the
// server's canonical YAML rendering and the JSON tags match the ProjectConfig
// schema. The typed struct drives local decoding and validation; the apply
// request body is produced from the interpolated manifest as a generic shape
// (see uploadBody) rather than by marshaling this struct, so that omitted
// fields stay omitted and the server's required-field validation applies. A
// non-pointer field such as VariableManifest.Value would otherwise serialize an
// omitted value as its zero value ("") and, because empty variable values are
// valid, silently clear the variable instead of being rejected.
type Manifest struct {
	Version   int                 `yaml:"version" json:"version"`
	Project   *ProjectManifest    `yaml:"project,omitempty" json:"project,omitempty"`
	Databases *[]DatabaseManifest `yaml:"databases,omitempty" json:"databases,omitempty"`
	Variables *[]VariableManifest `yaml:"variables,omitempty" json:"variables,omitempty"`
	Buckets   *[]BucketManifest   `yaml:"buckets,omitempty" json:"buckets,omitempty"`
	Realtime  *RealtimeManifest   `yaml:"realtime,omitempty" json:"realtime,omitempty"`
	Auth      *AuthManifest       `yaml:"auth,omitempty" json:"auth,omitempty"`
	Functions *[]FunctionManifest `yaml:"functions,omitempty" json:"functions,omitempty"`
	Frontends *[]FrontendManifest `yaml:"frontends,omitempty" json:"frontends,omitempty"`

	// upload is the interpolated manifest decoded into a generic shape; it is the
	// source of the apply request body (see uploadBody). Populated by Parse.
	upload map[string]any
}

// ProjectManifest declares project-level settings.
type ProjectManifest struct {
	Name            *string   `yaml:"name,omitempty" json:"name,omitempty"`
	AllRegions      *bool     `yaml:"all_regions,omitempty" json:"all_regions,omitempty"`
	SelectedRegions *[]string `yaml:"selected_regions,omitempty" json:"selected_regions,omitempty"`
}

// DatabaseManifest asserts identity properties of an existing database.
type DatabaseManifest struct {
	Name         string  `yaml:"name" json:"name"`
	Region       string  `yaml:"region" json:"region"`
	PgVersion    string  `yaml:"pg_version" json:"pg_version"`
	DatabaseType *string `yaml:"database_type,omitempty" json:"database_type,omitempty"`
}

// VariableManifest declares one project variable.
type VariableManifest struct {
	Name  string `yaml:"name" json:"name"`
	Value string `yaml:"value" json:"value"`
}

// BucketManifest declares settings and policies for an existing bucket.
type BucketManifest struct {
	Name             string            `yaml:"name" json:"name"`
	FileSizeLimit    *int64            `yaml:"file_size_limit,omitempty" json:"file_size_limit,omitempty"`
	AllowedMimeTypes *[]string         `yaml:"allowed_mime_types,omitempty" json:"allowed_mime_types,omitempty"`
	Policies         *[]PolicyManifest `yaml:"policies,omitempty" json:"policies,omitempty"`
}

// PolicyManifest declares one storage policy attached to a bucket.
type PolicyManifest struct {
	Name       string `yaml:"name" json:"name"`
	Operation  string `yaml:"operation" json:"operation"`
	Definition string `yaml:"definition" json:"definition"`
}

// RealtimeManifest declares realtime feature flags.
type RealtimeManifest struct {
	Enabled                *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	BroadcastEnabled       *bool `yaml:"broadcast_enabled,omitempty" json:"broadcast_enabled,omitempty"`
	PresenceEnabled        *bool `yaml:"presence_enabled,omitempty" json:"presence_enabled,omitempty"`
	PostgresChangesEnabled *bool `yaml:"postgres_changes_enabled,omitempty" json:"postgres_changes_enabled,omitempty"`
}

// AuthManifest groups authentication settings like the dashboard tabs.
type AuthManifest struct {
	Tokens            *AuthTokensManifest            `yaml:"tokens,omitempty" json:"tokens,omitempty"`
	Sessions          *AuthSessionsManifest          `yaml:"sessions,omitempty" json:"sessions,omitempty"`
	Signup            *AuthSignupManifest            `yaml:"signup,omitempty" json:"signup,omitempty"`
	RateLimits        *AuthRateLimitsManifest        `yaml:"rate_limits,omitempty" json:"rate_limits,omitempty"`
	Password          *AuthPasswordManifest          `yaml:"password,omitempty" json:"password,omitempty"`
	PasswordReset     *AuthPasswordResetManifest     `yaml:"password_reset,omitempty" json:"password_reset,omitempty"`
	EmailVerification *AuthEmailVerificationManifest `yaml:"email_verification,omitempty" json:"email_verification,omitempty"`
	Cors              *AuthCORSManifest              `yaml:"cors,omitempty" json:"cors,omitempty"`
	Providers         *AuthProvidersManifest         `yaml:"providers,omitempty" json:"providers,omitempty"`
	Email             *AuthEmailManifest             `yaml:"email,omitempty" json:"email,omitempty"`
	ManagedPages      *AuthManagedPagesManifest      `yaml:"managed_pages,omitempty" json:"managed_pages,omitempty"`
}

// AuthTokensManifest declares token lifetimes in seconds.
type AuthTokensManifest struct {
	AccessTokenLifetime       *int `yaml:"access_token_lifetime,omitempty" json:"access_token_lifetime,omitempty"`
	RefreshTokenLifetime      *int `yaml:"refresh_token_lifetime,omitempty" json:"refresh_token_lifetime,omitempty"`
	RefreshTokenReuseInterval *int `yaml:"refresh_token_reuse_interval,omitempty" json:"refresh_token_reuse_interval,omitempty"`
	PlatformTokenTTL          *int `yaml:"platform_token_ttl,omitempty" json:"platform_token_ttl,omitempty"`
}

// AuthSessionsManifest declares session lifetime limits.
type AuthSessionsManifest struct {
	InactivityTimeout  *int `yaml:"inactivity_timeout,omitempty" json:"inactivity_timeout,omitempty"`
	MaxSessionDuration *int `yaml:"max_session_duration,omitempty" json:"max_session_duration,omitempty"`
}

// AuthSignupManifest declares signup and access control switches.
type AuthSignupManifest struct {
	EnableSignup           *bool `yaml:"enable_signup,omitempty" json:"enable_signup,omitempty"`
	EnableAnonymousSignins *bool `yaml:"enable_anonymous_signins,omitempty" json:"enable_anonymous_signins,omitempty"`
	// AllowedEmailDomains replaces the stored allowlist; an empty list removes
	// the restriction. AllowedEmailDomainsMode is disabled, signup, or
	// signup_and_signin.
	AllowedEmailDomains     *[]string `yaml:"allowed_email_domains,omitempty" json:"allowed_email_domains,omitempty"`
	AllowedEmailDomainsMode *string   `yaml:"allowed_email_domains_mode,omitempty" json:"allowed_email_domains_mode,omitempty"`
}

// AuthRateLimitsManifest declares per-hour auth rate limits.
type AuthRateLimitsManifest struct {
	Signup        *int `yaml:"signup,omitempty" json:"signup,omitempty"`
	Signin        *int `yaml:"signin,omitempty" json:"signin,omitempty"`
	TokenRefresh  *int `yaml:"token_refresh,omitempty" json:"token_refresh,omitempty"`
	PasswordReset *int `yaml:"password_reset,omitempty" json:"password_reset,omitempty"`
}

// AuthPasswordManifest declares password strength requirements.
type AuthPasswordManifest struct {
	MinLength           *int  `yaml:"min_length,omitempty" json:"min_length,omitempty"`
	RequireUppercase    *bool `yaml:"require_uppercase,omitempty" json:"require_uppercase,omitempty"`
	RequireLowercase    *bool `yaml:"require_lowercase,omitempty" json:"require_lowercase,omitempty"`
	RequireNumbers      *bool `yaml:"require_numbers,omitempty" json:"require_numbers,omitempty"`
	RequireSpecialChars *bool `yaml:"require_special_chars,omitempty" json:"require_special_chars,omitempty"`
}

// AuthPasswordResetManifest declares password reset behavior.
type AuthPasswordResetManifest struct {
	Allow      *bool `yaml:"allow,omitempty" json:"allow,omitempty"`
	Timeout    *int  `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	MaxHistory *int  `yaml:"max_history,omitempty" json:"max_history,omitempty"`
}

// AuthEmailVerificationManifest declares email confirmation behavior.
type AuthEmailVerificationManifest struct {
	RequireConfirmation *bool `yaml:"require_confirmation,omitempty" json:"require_confirmation,omitempty"`
	ConfirmationTimeout *int  `yaml:"confirmation_timeout,omitempty" json:"confirmation_timeout,omitempty"`
}

// AuthCORSManifest declares auth CORS settings.
type AuthCORSManifest struct {
	Enabled          *bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	AllowedOrigins   *[]string `yaml:"allowed_origins,omitempty" json:"allowed_origins,omitempty"`
	AllowCredentials *bool     `yaml:"allow_credentials,omitempty" json:"allow_credentials,omitempty"`
	MaxAge           *int      `yaml:"max_age,omitempty" json:"max_age,omitempty"`
}

// AuthProvidersManifest declares sign-in providers.
type AuthProvidersManifest struct {
	EmailPassword *AuthEmailPasswordManifest `yaml:"email_password,omitempty" json:"email_password,omitempty"`
	Oauth         *[]OAuthProviderManifest   `yaml:"oauth,omitempty" json:"oauth,omitempty"`
}

// AuthEmailPasswordManifest declares the email/password provider switch.
type AuthEmailPasswordManifest struct {
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// OAuthProviderManifest declares one OAuth provider configuration.
type OAuthProviderManifest struct {
	Provider     string    `yaml:"provider" json:"provider"`
	Enabled      *bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	ClientID     *string   `yaml:"client_id,omitempty" json:"client_id,omitempty"`
	ClientSecret *string   `yaml:"client_secret,omitempty" json:"client_secret,omitempty"`
	RedirectURL  *string   `yaml:"redirect_url,omitempty" json:"redirect_url,omitempty"`
	Scopes       *[]string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
}

// AuthEmailManifest declares transactional email settings.
type AuthEmailManifest struct {
	Enabled   *bool                   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	From      *AuthEmailFromManifest  `yaml:"from,omitempty" json:"from,omitempty"`
	SMTP      *AuthEmailSMTPManifest  `yaml:"smtp,omitempty" json:"smtp,omitempty"`
	Templates *EmailTemplatesManifest `yaml:"templates,omitempty" json:"templates,omitempty"`
}

// AuthEmailFromManifest declares the sender identity.
type AuthEmailFromManifest struct {
	Address *string `yaml:"address,omitempty" json:"address,omitempty"`
	Name    *string `yaml:"name,omitempty" json:"name,omitempty"`
}

// AuthEmailSMTPManifest declares SMTP delivery settings.
type AuthEmailSMTPManifest struct {
	Host     *string `yaml:"host,omitempty" json:"host,omitempty"`
	Port     *int    `yaml:"port,omitempty" json:"port,omitempty"`
	Username *string `yaml:"username,omitempty" json:"username,omitempty"`
	Password *string `yaml:"password,omitempty" json:"password,omitempty"`
	UseTLS   *bool   `yaml:"use_tls,omitempty" json:"use_tls,omitempty"`
}

// EmailTemplatesManifest declares email templates keyed by type.
type EmailTemplatesManifest struct {
	Confirmation    *EmailTemplateManifest `yaml:"confirmation,omitempty" json:"confirmation,omitempty"`
	PasswordReset   *EmailTemplateManifest `yaml:"password_reset,omitempty" json:"password_reset,omitempty"`
	PasswordChanged *EmailTemplateManifest `yaml:"password_changed,omitempty" json:"password_changed,omitempty"`
	Welcome         *EmailTemplateManifest `yaml:"welcome,omitempty" json:"welcome,omitempty"`
}

// EmailTemplateManifest declares one email template.
type EmailTemplateManifest struct {
	Subject  *string `yaml:"subject,omitempty" json:"subject,omitempty"`
	HTMLBody *string `yaml:"html_body,omitempty" json:"html_body,omitempty"`
	TextBody *string `yaml:"text_body,omitempty" json:"text_body,omitempty"`
}

// AuthManagedPagesManifest declares managed auth page settings.
type AuthManagedPagesManifest struct {
	Enabled   *bool                  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Redirects *AuthRedirectsManifest `yaml:"redirects,omitempty" json:"redirects,omitempty"`
	Pages     *HostedPagesManifest   `yaml:"pages,omitempty" json:"pages,omitempty"`
}

// AuthRedirectsManifest declares the redirect allowlist and defaults.
type AuthRedirectsManifest struct {
	Allowed            *[]string `yaml:"allowed,omitempty" json:"allowed,omitempty"`
	PostAuth           *string   `yaml:"post_auth,omitempty" json:"post_auth,omitempty"`
	PostLogout         *string   `yaml:"post_logout,omitempty" json:"post_logout,omitempty"`
	DeviceVerification *string   `yaml:"device_verification,omitempty" json:"device_verification,omitempty"`
}

// HostedPagesManifest declares hosted auth pages keyed by page type.
type HostedPagesManifest struct {
	Login         *HostedPageManifest `yaml:"login,omitempty" json:"login,omitempty"`
	ResetPassword *HostedPageManifest `yaml:"reset_password,omitempty" json:"reset_password,omitempty"`
}

// HostedPageManifest declares one hosted auth page.
type HostedPageManifest struct {
	HTML string  `yaml:"html" json:"html"`
	CSS  *string `yaml:"css,omitempty" json:"css,omitempty"`
}

// FunctionManifest declares configuration for one deployed function.
type FunctionManifest struct {
	Name       string               `yaml:"name" json:"name"`
	Public     *bool                `yaml:"public,omitempty" json:"public,omitempty"`
	Schedulers *[]SchedulerManifest `yaml:"schedulers,omitempty" json:"schedulers,omitempty"`
}

// SchedulerManifest declares one scheduler attached to a function. Regions is
// parsed only to reject manifests using the removed field with a clear error.
type SchedulerManifest struct {
	Name    string          `yaml:"name" json:"name"`
	Cron    string          `yaml:"cron" json:"cron"`
	Enabled *bool           `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Payload *map[string]any `yaml:"payload,omitempty" json:"payload,omitempty"`
	Regions *[]string       `yaml:"regions,omitempty" json:"-"`
}

// FrontendManifest declares configuration for one deployed frontend.
type FrontendManifest struct {
	Name         string                `yaml:"name" json:"name"`
	CustomDomain *CustomDomainManifest `yaml:"custom_domain,omitempty" json:"custom_domain,omitempty"`
}

// CustomDomainManifest declares a frontend custom domain with BYOC TLS.
type CustomDomainManifest struct {
	Domain string       `yaml:"domain" json:"domain"`
	TLS    *TLSManifest `yaml:"tls,omitempty" json:"tls,omitempty"`
}

// TLSManifest declares BYOC TLS material for a custom domain.
type TLSManifest struct {
	Mode                string  `yaml:"mode" json:"mode"`
	CertificatePEM      string  `yaml:"certificate_pem" json:"certificate_pem"`
	PrivateKeyPEM       string  `yaml:"private_key_pem" json:"private_key_pem"`
	CertificateChainPEM *string `yaml:"certificate_chain_pem,omitempty" json:"certificate_chain_pem,omitempty"`
}

// Load reads, interpolates, parses, and validates a manifest from disk. The
// returned path is the absolute path that was actually opened.
func Load(filePath string) (*Manifest, string, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, "", errors.New("file path is required")
	}

	resolved, err := filepath.Abs(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve file path: %w", err)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read configuration file %q: %w", resolved, err)
	}

	manifest, err := Parse(data, os.LookupEnv)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", resolved, err)
	}
	return manifest, resolved, nil
}

// Parse interpolates ${ENV} references through lookupEnv, decodes the manifest
// strictly (unknown fields are errors), and validates it.
func Parse(data []byte, lookupEnv func(string) (string, bool)) (*Manifest, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}
	if root.Kind == 0 {
		return nil, errors.New("manifest is empty")
	}

	if err := interpolateNode(&root, lookupEnv); err != nil {
		return nil, err
	}

	interpolated, err := yaml.Marshal(&root)
	if err != nil {
		return nil, fmt.Errorf("failed to re-encode manifest after interpolation: %w", err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(interpolated))
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}

	if err := manifest.Validate(); err != nil {
		return nil, err
	}

	// Capture the interpolated manifest as a generic shape for upload so omitted
	// fields stay absent (see uploadBody and the Manifest doc comment).
	if err := yaml.Unmarshal(interpolated, &manifest.upload); err != nil {
		return nil, fmt.Errorf("failed to prepare manifest for upload: %w", err)
	}
	return &manifest, nil
}

// uploadBody returns the apply request body: the interpolated manifest marshaled
// from its generic shape. Marshaling the generic shape rather than the typed
// struct preserves omitted fields as absent — a non-pointer field such as
// VariableManifest.Value would otherwise serialize an omitted value as "" and
// bypass the server's required-field validation, silently clearing the variable.
func (m *Manifest) uploadBody() ([]byte, error) {
	if m.upload == nil {
		return nil, errors.New("manifest was not parsed")
	}
	body, err := json.Marshal(m.upload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode manifest: %w", err)
	}
	return body, nil
}

// Validate performs the minimal local checks: the schema version and the
// removed scheduler regions field. All semantic validation is server-side.
func (m *Manifest) Validate() error {
	if m.Version != ManifestVersion {
		return fmt.Errorf("unsupported manifest version %d (expected %d)", m.Version, ManifestVersion)
	}
	if m.Functions == nil {
		return nil
	}
	for _, function := range *m.Functions {
		if function.Schedulers == nil {
			continue
		}
		for _, scheduler := range *function.Schedulers {
			if scheduler.Regions != nil {
				return fmt.Errorf("function %q scheduler %q: the schedulers %q field is no longer supported; scheduler placement is managed by the server — remove the field and re-run", function.Name, scheduler.Name, "regions")
			}
		}
	}
	return nil
}

// ResolveManifestPath returns the manifest path that config commands should
// use. An explicit path is used as-is (after existence check); otherwise the
// CLI looks for volcano/volcano-config.yaml then volcano-config.yaml in the
// working directory.
func ResolveManifestPath(fileArg string) (string, error) {
	if trimmed := strings.TrimSpace(fileArg); trimmed != "" {
		if !fileExists(trimmed) {
			return "", fmt.Errorf("specified file not found: %s", trimmed)
		}
		return trimmed, nil
	}

	candidates := []string{nestedManifestPath, rootManifestPath}
	found := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if fileExists(candidate) {
			found = append(found, candidate)
		}
	}

	switch len(found) {
	case 0:
		return "", errors.New("no volcano-config.yaml file found.\ncreate volcano/volcano-config.yaml or ./volcano-config.yaml, or use --file to specify a path")
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("found multiple volcano-config.yaml files: %s.\nplease keep only one volcano-config.yaml file, or specify explicitly with --file", strings.Join(found, ", "))
	}
}

// DefaultPullPath returns where `config pull` writes when no --file is given:
// an existing manifest location if one is found, else volcano/volcano-config.yaml
// when the volcano directory exists, else ./volcano-config.yaml.
func DefaultPullPath() string {
	for _, candidate := range []string{nestedManifestPath, rootManifestPath} {
		if fileExists(candidate) {
			return candidate
		}
	}
	if info, err := os.Stat(manifestDir); err == nil && info.IsDir() {
		return nestedManifestPath
	}
	return rootManifestPath
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}
