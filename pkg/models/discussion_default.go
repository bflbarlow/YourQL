package models

// DiscussionDefaults holds all user-configurable default settings
// applied when creating a new discussion. Nil pointers mean "no
// default configured — use the system default for that field."
type DiscussionDefaults struct {
	LLMProviderID      *uint `json:"llm_provider_id,omitempty"`
	DataSourceID       *uint `json:"data_source_id,omitempty"`
	MaxContextMessages *int  `json:"max_context_messages,omitempty"`
	MaxMessages        *int  `json:"max_messages,omitempty"`
	Summarize          *bool `json:"summarize,omitempty"`
	VizEnabled         *bool `json:"viz_enabled,omitempty"`
	TechDetails        *bool `json:"tech_details,omitempty"`
	ContextDetails     *bool `json:"context_details,omitempty"`
	StreamingEnabled   *bool `json:"streaming_enabled,omitempty"`
}
