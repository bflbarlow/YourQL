package services

import (
	"YourQL/pkg/engine"
	"YourQL/pkg/models"
)

// This file contains the SQLite-backed implementations of the engine's
// port interfaces. They live in the services package (rather than a
// sub-package) so they can call the existing CRUD functions directly
// without creating an import cycle. The engine package itself never
// imports services — the adapters are what bridge the two directions.

// SqliteConversationStore implements engine.ConversationStore.
type SqliteConversationStore struct{}

func (s *SqliteConversationStore) GetByID(id uint) (*engine.ConversationMeta, error) {
	conv, err := GetConversationByID(id)
	if err != nil {
		return nil, err
	}
	return &engine.ConversationMeta{
		ID:                 conv.ID,
		LLMProviderID:      conv.LLMProviderID,
		DataSourceID:       conv.DataSourceID,
		MaxContextMessages: conv.MaxContextMessages,
		VizEnabled:         conv.VizEnabled,
		StreamingEnabled:   conv.StreamingEnabled,
		Summarize:          conv.Summarize,
	}, nil
}

func (s *SqliteConversationStore) GetMessages(conversationID uint) ([]*engine.ConversationMessageMeta, error) {
	msgs, err := GetConversationMessages(conversationID)
	if err != nil {
		return nil, err
	}
	result := make([]*engine.ConversationMessageMeta, 0, len(msgs))
	for _, m := range msgs {
		result = append(result, &engine.ConversationMessageMeta{
			ID:             m.ID,
			Role:           m.Role,
			Content:        m.Content,
			LLMContent:     m.LLMContent,
			SQLResults:     m.SQLResults,
			Metadata:       m.Metadata,
			ToolTranscript: m.ToolTranscript,
		})
	}
	return result, nil
}

func (s *SqliteConversationStore) CreateMessage(conversationID uint, msg engine.NewMessage) (*engine.ConversationMessageMeta, error) {
	m, err := CreateConversationMessage(conversationID, msg.Role, msg.Content, msg.LLMContent, msg.SQLResults, msg.Metadata)
	if err != nil {
		return nil, err
	}
	return &engine.ConversationMessageMeta{
		ID:             m.ID,
		Role:           m.Role,
		Content:        m.Content,
		LLMContent:     m.LLMContent,
		SQLResults:     m.SQLResults,
		Metadata:       m.Metadata,
		ToolTranscript: m.ToolTranscript,
	}, nil
}

// SqliteLLMProviderStore implements engine.LLMProviderStore.
type SqliteLLMProviderStore struct{}

func (s *SqliteLLMProviderStore) GetByID(id uint) (*engine.LLMProviderMeta, error) {
	p, err := GetLLMProviderByID(id)
	if err != nil || p == nil {
		return nil, err
	}
	return &engine.LLMProviderMeta{
		ID:             p.ID,
		Name:           p.Name,
		Provider:       p.Provider,
		APIKey:         strOrEmpty(p.APIKey),
		Model:          strOrEmpty(p.Model),
		BaseURL:        strOrEmpty(p.BaseURL),
		ContextWindow:  p.ContextWindow,
		MaxTokens:      p.MaxTokens,
		ModelMaxTokens: p.ModelMaxTokens,
	}, nil
}

func (s *SqliteLLMProviderStore) GetDefault() (*engine.LLMProviderMeta, error) {
	p, err := GetDefaultLLMProvider()
	if err != nil || p == nil {
		return nil, err
	}
	return &engine.LLMProviderMeta{
		ID:             p.ID,
		Name:           p.Name,
		Provider:       p.Provider,
		APIKey:         strOrEmpty(p.APIKey),
		Model:          strOrEmpty(p.Model),
		BaseURL:        strOrEmpty(p.BaseURL),
		ContextWindow:  p.ContextWindow,
		MaxTokens:      p.MaxTokens,
		ModelMaxTokens: p.ModelMaxTokens,
	}, nil
}

// SqliteDataSourceStore implements engine.DataSourceStore.
type SqliteDataSourceStore struct{}

func (s *SqliteDataSourceStore) GetByID(id uint) (*engine.DataSourceMeta, error) {
	ds, err := GetDataSourceByID(id)
	if err != nil || ds == nil {
		return nil, err
	}
	return dataSourceToEngineMeta(ds), nil
}

func (s *SqliteDataSourceStore) GetDefault() (*engine.DataSourceMeta, error) {
	ds, err := GetDefaultDataSource()
	if err != nil || ds == nil {
		return nil, err
	}
	return dataSourceToEngineMeta(ds), nil
}

func dataSourceToEngineMeta(ds *models.DataSource) *engine.DataSourceMeta {
	host := ""
	if ds.Host != nil {
		host = *ds.Host
	}
	database := ""
	if ds.Database != nil {
		database = *ds.Database
	}
	username := ""
	if ds.Username != nil {
		username = *ds.Username
	}
	password := ""
	if ds.Password != nil {
		password = *ds.Password
	}
	sslMode := ""
	if ds.SSLMode != nil {
		sslMode = *ds.SSLMode
	}
	port := 0
	if ds.Port != nil {
		port = *ds.Port
	}
	configJSON := ""
	if ds.Config != nil {
		configJSON = *ds.Config
	}
	extra := ""
	if ds.Extra != nil {
		extra = *ds.Extra
	}
	filePath := ""
	if ds.FilePath != nil {
		filePath = *ds.FilePath
	}
	fileType := ""
	if ds.FileType != nil {
		fileType = *ds.FileType
	}

	cfg, _ := ds.ParseConfig()
	maxRounds := 0
	maxTools := 0
	maxRetries := 0
	explSafety := ""
	if cfg != nil {
		maxRounds = cfg.MaxExplorationRounds
		maxTools = cfg.MaxToolsPerRound
		maxRetries = cfg.MaxFinalQueryRetries
		explSafety = cfg.ExplorationSafety
	}

	return &engine.DataSourceMeta{
		ID:                   ds.ID,
		Name:                 ds.Name,
		Type:                 ds.Type,
		Host:                 host,
		Port:                 port,
		Database:             database,
		Username:             username,
		Password:             password,
		SSLMode:              sslMode,
		ConfigJSON:           configJSON,
		Extra:                extra,
		FilePath:             filePath,
		FileType:             fileType,
		MaxExplorationRounds: maxRounds,
		ExplorationSafety:    explSafety,
		MaxFinalRetries:      maxRetries,
		MaxToolsPerRound:     maxTools,
	}
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// SqliteConfigProvider implements engine.ConfigurationProvider.
type SqliteConfigProvider struct{}

func (s *SqliteConfigProvider) GetEnabledSkillsContent(conversationID uint) (string, error) {
	return GetEnabledSkillsContent(conversationID)
}

func (s *SqliteConfigProvider) GetAgentLoopConfig() (*engine.AgentLoopConfig, error) {
	cfg, err := GetAgentLoopConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return &engine.AgentLoopConfig{}, nil
	}
	return cfg, nil
}

func (s *SqliteConfigProvider) GetTimeoutSeconds(key string, defaultSeconds int) int {
	return GetTimeoutSetting(key, defaultSeconds)
}

// SqliteOutputHandler implements engine.OutputHandler.
type SqliteOutputHandler struct{}

func (o *SqliteOutputHandler) CreateQueryRecord(conversationID uint, userMessage string, providerID *uint) (uint, error) {
	q, err := CreateQuery(&conversationID, userMessage, providerID, nil)
	if err != nil {
		return 0, err
	}
	return q.ID, nil
}

func (o *SqliteOutputHandler) EmitFinalResponse(conversationID uint, queryID uint, resp engine.FinalResponse) error {
	return EmitFinalResponseDB(conversationID, queryID, resp)
}

func (o *SqliteOutputHandler) EmitSQLWarning(conversationID uint, queryID uint, sql string, err error, isRetryable bool) error {
	return EmitSQLWarningDB(conversationID, queryID, sql, err, isRetryable)
}

func (o *SqliteOutputHandler) EmitClarification(conversationID uint, queryID uint, category string, message string) error {
	return EmitClarificationDB(conversationID, queryID, category, message)
}

func (o *SqliteOutputHandler) StoreTechDetail(conversationID uint, detail engine.TechDetail) error {
	StoreTechDetailDB(conversationID, detail)
	return nil
}

// SchemaIntrospectorAdapter implements engine.SchemaIntrospector.
type SchemaIntrospectorAdapter struct{}

func (s *SchemaIntrospectorAdapter) GetSchema(ds engine.DataSourceMeta) (*engine.DataSchema, error) {
	modelsDS := engineMetaToDataSource(ds)
	return GetDataSchema(modelsDS)
}

func engineMetaToDataSource(ds engine.DataSourceMeta) *models.DataSource {
	return &models.DataSource{
		Name:     ds.Name,
		Type:     ds.Type,
		Host:     strPtrNil(ds.Host),
		Port:     intPtrNil(ds.Port),
		Database: strPtrNil(ds.Database),
		Username: strPtrNil(ds.Username),
		Password: strPtrNil(ds.Password),
		SSLMode:  strPtrNil(ds.SSLMode),
		Config:   strPtrNil(ds.ConfigJSON),
		Extra:    strPtrNil(ds.Extra),
		FilePath: strPtrNil(ds.FilePath),
		FileType: strPtrNil(ds.FileType),
	}
}

// QueryExecutorAdapter implements engine.QueryExecutor by wrapping the
// existing executeSQLWithMode. It carries the data source connection info
// and the configured safety mode.
//
// SAFETY-CRITICAL: SafetyMode is required; never left at the Go zero value.
// Execute calls engine.ValidateExplorationQuery when isExploration==true in
// addition to the read-only invariant enforced inside executeSQLWithMode.
type QueryExecutorAdapter struct {
	DataSource *models.DataSource
	SafetyMode engine.ExplorationSafetyMode
}

func (q *QueryExecutorAdapter) Execute(sqlQuery string, isExploration bool) (*engine.QueryResult, error) {
	if isExploration {
		if err := engine.ValidateExplorationQuery(sqlQuery, q.SafetyMode); err != nil {
			return nil, err
		}
	}
	return ExecuteSQLWithMode(q.DataSource, sqlQuery, isExploration)
}

func strPtrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intPtrNil(i int) *int {
	if i == 0 {
		return nil
	}
	return &i
}
