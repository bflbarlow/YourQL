package configuration

// AppConfig holds all application-wide configuration settings.
// This centralizes configuration management for the Tix App.
type AppConfig struct {
	Lockout  LockoutConfig  `json:"lockout" yaml:"lockout"`
	Email    EmailConfig    `json:"email" yaml:"email"`
	Password PasswordConfig `json:"password" yaml:"password"`
	Workspace WorkspaceConfig `json:"workspace" yaml:"workspace"`
	LLM       LLMConfig     `json:"llm" yaml:"llm"`
	SQLQuery  SQLQueryConfig `json:"sql_query" yaml:"sql_query"`
}

// LockoutConfig holds account lockout security settings.
// These values control how the application responds to failed login attempts.
type LockoutConfig struct {
	// MaxAttempts is the number of consecutive failed login attempts
	// before an account gets temporarily locked out.
	// Default: 5
	MaxAttempts int `json:"max_attempts" yaml:"max_attempts"`

	// DurationMinutes is how long (in minutes) an account stays locked out
	// after exceeding MaxAttempts. After this duration, the account
	// automatically unlocks on the next login attempt.
	// Default: 15
	DurationMinutes int `json:"duration_minutes" yaml:"duration_minutes"`
}

// PasswordConfig holds password validation settings.
// These values control what makes a valid password in the application.
type PasswordConfig struct {
	// MinLength is the minimum number of characters required for a password.
	// Default: 8
	MinLength int `json:"min_length" yaml:"min_length"`

	// RequireUppercase indicates whether passwords must contain at least one uppercase letter.
	// Default: true
	RequireUppercase bool `json:"require_uppercase" yaml:"require_uppercase"`

	// RequireLowercase indicates whether passwords must contain at least one lowercase letter.
	// Default: true
	RequireLowercase bool `json:"require_lowercase" yaml:"require_lowercase"`

	// RequireDigit indicates whether passwords must contain at least one digit.
	// Default: true
	RequireDigit bool `json:"require_digit" yaml:"require_digit"`

	// RequireSpecial indicates whether passwords must contain at least one special character.
	// Default: true
	RequireSpecial bool `json:"require_special" yaml:"require_special"`
}

// DefaultConfig returns the default application configuration.
// Call this to get a fresh config with sensible defaults.
func DefaultConfig() *AppConfig {
	return &AppConfig{
		Lockout: LockoutConfig{
			MaxAttempts:     7,
			DurationMinutes: 15,
		},
		Email: DefaultEmailConfig(),
		Password: PasswordConfig{
			MinLength:        8,
			RequireUppercase: true,
			RequireLowercase: true,
			RequireDigit:     true,
			RequireSpecial:   true,
		},
		Workspace: WorkspaceConfig{
			MaxWorkspacesPerUser: 10,
			DefaultRole:          "member",
			UUIDNamespace:        "data_app",
		},
		LLM: LLMConfig{
			DefaultProvider: "openai",
			DefaultModel:    "gpt-4-turbo",
			TimeoutSeconds:  30,
			RateLimitPerMin: 30,
			MaxTokens:       4096,
		},
		SQLQuery: SQLQueryConfig{
			DefaultLimit:            1000,
			ExplorationDefaultLimit: 100,
			QueryLengthThreshold:    200,
		},
	}
}

// WorkspaceConfig holds workspace-related configuration settings.
type WorkspaceConfig struct {
	// MaxWorkspacesPerUser is the maximum number of workspaces a single user can create.
	MaxWorkspacesPerUser int `json:"max_workspaces_per_user" yaml:"max_workspaces_per_user"`

	// DefaultRole is the default role assigned when a user joins a workspace.
	DefaultRole string `json:"default_role" yaml:"default_role"`

	// UUIDNamespace is the UUID namespace (v4) used for generating workspace UUIDs.
	UUIDNamespace string `json:"uuid_namespace" yaml:"uuid_namespace"`
}

// LLMConfig holds language model configuration defaults.
type LLMConfig struct {
	// DefaultProvider is the default LLM provider (openai, anthropic, ollama, local).
	DefaultProvider string `json:"default_provider" yaml:"default_provider"`

	// DefaultModel is the default model to use.
	DefaultModel string `json:"default_model" yaml:"default_model"`

	// TimeoutSeconds is the default timeout for LLM API calls in seconds.
	TimeoutSeconds int `json:"timeout_seconds" yaml:"timeout_seconds"`

	// RateLimitPerMin is the maximum number of requests per minute per workspace.
	RateLimitPerMin int `json:"rate_limit_per_min" yaml:"rate_limit_per_min"`

	// MaxTokens is the default maximum tokens for LLM responses.
	MaxTokens int `json:"max_tokens" yaml:"max_tokens"`
}

// SQLQueryConfig holds SQL query execution configuration defaults.
type SQLQueryConfig struct {
	// DefaultLimit is the maximum number of rows returned for queries without a LIMIT clause.
	// Set to 0 to disable (no limit applied). Default: 1000.
	DefaultLimit int `json:"default_limit" yaml:"default_limit"`

	// ExplorationDefaultLimit is the maximum rows returned during exploration mode.
	// Set to 0 to disable (no limit applied). Default: 100.
	ExplorationDefaultLimit int `json:"exploration_default_limit" yaml:"exploration_default_limit"`

	// QueryLengthThreshold triggers the default limit only when the LLM's SQL
	// query length exceeds this value (in characters). Short queries (e.g.
	// "SELECT COUNT(*) FROM users") are assumed intentional and left unbounded.
	// Set to 0 to always apply the limit regardless of query length. Default: 200.
	QueryLengthThreshold int `json:"query_length_threshold" yaml:"query_length_threshold"`
}

// Config is the global application configuration instance.
// Modify this at startup to customize behavior, or load from external config file.
var Config = DefaultConfig()