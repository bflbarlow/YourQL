package configuration

// SQLQueryConfig holds SQL query execution defaults (the only config used at
// runtime in the single-user desktop app).
type SQLQueryConfig struct {
	DefaultLimit            int `json:"default_limit" yaml:"default_limit"`
	ExplorationDefaultLimit int `json:"exploration_default_limit" yaml:"exploration_default_limit"`
	QueryLengthThreshold    int `json:"query_length_threshold" yaml:"query_length_threshold"`
}

// AppConfig holds application configuration.
type AppConfig struct {
	SQLQuery SQLQueryConfig `json:"sql_query" yaml:"sql_query"`
}

func DefaultConfig() *AppConfig {
	return &AppConfig{
		SQLQuery: SQLQueryConfig{
			DefaultLimit:            1000,
			ExplorationDefaultLimit: 100,
			QueryLengthThreshold:    200,
		},
	}
}

var Config = DefaultConfig()