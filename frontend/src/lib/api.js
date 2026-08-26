// YourQL — single import point for Wails bindings + shared conversation
// creation honoring Discussion Defaults (Two Modes Principle, A0 rule 4).

export * from "../../wailsjs/go/main/App.js";

import {
  CreateConversation,
  GetDiscussionDefaults,
  ListLLMProviders,
  ListDataSources,
  GetAppSetting,
} from "../../wailsjs/go/main/App.js";

/**
 * Create a new conversation pre-filled from the user's saved defaults.
 * Fallback chain: Discussion Defaults → app-wide default provider/source →
 * nulls. Never throws for missing defaults; only propagates backend errors.
 */
export async function createDefaultConversation(title = "") {
  let providerId = null;
  let sourceId = null;

  try {
    const d = await GetDiscussionDefaults();
    if (d) {
      providerId = d.llm_provider_id ?? null;
      sourceId = d.data_source_id ?? null;
    }
  } catch {
    /* defaults are a convenience, not a requirement */
  }

  // Fall back to app-wide defaults where per-discussion defaults are unset.
  if (!providerId) {
    try {
      const providers = (await ListLLMProviders()) || [];
      const def = providers.find((p) => p.is_default);
      providerId = def?.id ?? null;
    } catch {
      /* ignore */
    }
  }
  if (!sourceId) {
    try {
      const sources = (await ListDataSources()) || [];
      const def = sources.find((s) => s.is_default);
      sourceId = def?.id ?? null;
    } catch {
      /* ignore */
    }
  }

  return CreateConversation(title, providerId, sourceId);
}

// Re-export commonly used settings keys so string literals live in one place.
export const APP_SETTING_KEYS = {
  minimalist: "minimalist_mode",
  agentLoopAdvanced: "agent_loop_advanced_enabled",
  dbSwitcher: "db_switcher_enabled",
  pipelineTimeout: "pipeline_timeout_seconds",
  summarizationTimeout: "summarization_timeout_seconds",
};

export { GetAppSetting };
