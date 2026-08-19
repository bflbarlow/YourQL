package models

import "YourQL/pkg/engine"

// AgentLoopConfig holds all user-configurable text for the agentic loop's
// system prompt and tool definitions. The canonical definition now lives in
// pkg/engine; this is a type alias so existing call sites in services/
// (and the database-backed config layer) continue to compile unchanged.
type AgentLoopConfig = engine.AgentLoopConfig