export namespace engine {
	
	export class AgentLoopConfig {
	    tool_query_database_desc: string;
	    tool_query_database_sql_desc: string;
	    tool_query_database_is_exploration_desc: string;
	    tool_query_database_reasoning_desc: string;
	    tool_respond_to_user_desc: string;
	    tool_respond_to_user_text_desc: string;
	    tool_render_chart_desc: string;
	    tool_render_chart_config_desc: string;
	    instruction_1: string;
	    instruction_2: string;
	    instruction_2a: string;
	    instruction_2b: string;
	    instruction_2c: string;
	    instruction_3: string;
	    instruction_4: string;
	    instruction_5: string;
	    instruction_6: string;
	    safety_preamble: string;
	    safety_strict: string;
	    safety_moderate: string;
	    safety_relaxed: string;
	    safety_footer: string;
	    safety_oneshot: string;
	    charts_intro: string;
	    charts_format: string;
	    charts_timing: string;
	    persona_fallback: string;
	    response_parse_error: string;
	    response_exploration_exhausted: string;
	    response_safety_rejected: string;
	    response_oneshot_violation: string;
	    response_unknown_tool: string;
	    response_render_chart_no_pending: string;
	    response_render_chart_parse_error: string;
	    response_respond_parse_error: string;
	    response_loop_exhausted: string;
	    response_empty_truncated: string;
	    response_context_overflow: string;
	    response_summarization_requires_final_query: string;
	    response_query_error_requires_retry: string;
	    response_too_many_tools_per_round: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentLoopConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool_query_database_desc = source["tool_query_database_desc"];
	        this.tool_query_database_sql_desc = source["tool_query_database_sql_desc"];
	        this.tool_query_database_is_exploration_desc = source["tool_query_database_is_exploration_desc"];
	        this.tool_query_database_reasoning_desc = source["tool_query_database_reasoning_desc"];
	        this.tool_respond_to_user_desc = source["tool_respond_to_user_desc"];
	        this.tool_respond_to_user_text_desc = source["tool_respond_to_user_text_desc"];
	        this.tool_render_chart_desc = source["tool_render_chart_desc"];
	        this.tool_render_chart_config_desc = source["tool_render_chart_config_desc"];
	        this.instruction_1 = source["instruction_1"];
	        this.instruction_2 = source["instruction_2"];
	        this.instruction_2a = source["instruction_2a"];
	        this.instruction_2b = source["instruction_2b"];
	        this.instruction_2c = source["instruction_2c"];
	        this.instruction_3 = source["instruction_3"];
	        this.instruction_4 = source["instruction_4"];
	        this.instruction_5 = source["instruction_5"];
	        this.instruction_6 = source["instruction_6"];
	        this.safety_preamble = source["safety_preamble"];
	        this.safety_strict = source["safety_strict"];
	        this.safety_moderate = source["safety_moderate"];
	        this.safety_relaxed = source["safety_relaxed"];
	        this.safety_footer = source["safety_footer"];
	        this.safety_oneshot = source["safety_oneshot"];
	        this.charts_intro = source["charts_intro"];
	        this.charts_format = source["charts_format"];
	        this.charts_timing = source["charts_timing"];
	        this.persona_fallback = source["persona_fallback"];
	        this.response_parse_error = source["response_parse_error"];
	        this.response_exploration_exhausted = source["response_exploration_exhausted"];
	        this.response_safety_rejected = source["response_safety_rejected"];
	        this.response_oneshot_violation = source["response_oneshot_violation"];
	        this.response_unknown_tool = source["response_unknown_tool"];
	        this.response_render_chart_no_pending = source["response_render_chart_no_pending"];
	        this.response_render_chart_parse_error = source["response_render_chart_parse_error"];
	        this.response_respond_parse_error = source["response_respond_parse_error"];
	        this.response_loop_exhausted = source["response_loop_exhausted"];
	        this.response_empty_truncated = source["response_empty_truncated"];
	        this.response_context_overflow = source["response_context_overflow"];
	        this.response_summarization_requires_final_query = source["response_summarization_requires_final_query"];
	        this.response_query_error_requires_retry = source["response_query_error_requires_retry"];
	        this.response_too_many_tools_per_round = source["response_too_many_tools_per_round"];
	    }
	}

}

export namespace main {
	
	export class AgentLoopConfigField {
	    key: string;
	    label: string;
	    description: string;
	    section: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentLoopConfigField(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.section = source["section"];
	    }
	}
	export class AgentLoopConfigJSON {
	    config?: engine.AgentLoopConfig;
	    fields: AgentLoopConfigField[];
	
	    static createFrom(source: any = {}) {
	        return new AgentLoopConfigJSON(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.config = this.convertValues(source["config"], engine.AgentLoopConfig);
	        this.fields = this.convertValues(source["fields"], AgentLoopConfigField);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DataSourceSetting {
	    id: number;
	    name: string;
	    type: string;
	    host?: string;
	    port?: number;
	    database?: string;
	    username?: string;
	    sslMode?: string;
	    is_default: boolean;
	    is_active: boolean;
	    exploration_allowed: boolean;
	    max_exploration_rounds: number;
	    exploration_safety: string;
	    config?: string;
	    extra?: string;
	    file_path?: string;
	    file_type?: string;
	
	    static createFrom(source: any = {}) {
	        return new DataSourceSetting(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.database = source["database"];
	        this.username = source["username"];
	        this.sslMode = source["sslMode"];
	        this.is_default = source["is_default"];
	        this.is_active = source["is_active"];
	        this.exploration_allowed = source["exploration_allowed"];
	        this.max_exploration_rounds = source["max_exploration_rounds"];
	        this.exploration_safety = source["exploration_safety"];
	        this.config = source["config"];
	        this.extra = source["extra"];
	        this.file_path = source["file_path"];
	        this.file_type = source["file_type"];
	    }
	}
	export class GeneralSettings {
	    app_name: string;
	    app_version: string;
	    default_llm_provider: string;
	    theme: string;
	    accent: string;
	    scale: string;
	    language: string;
	
	    static createFrom(source: any = {}) {
	        return new GeneralSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.app_name = source["app_name"];
	        this.app_version = source["app_version"];
	        this.default_llm_provider = source["default_llm_provider"];
	        this.theme = source["theme"];
	        this.accent = source["accent"];
	        this.scale = source["scale"];
	        this.language = source["language"];
	    }
	}
	export class LLMProviderSetting {
	    id: number;
	    name: string;
	    provider: string;
	    model?: string;
	    baseURL?: string;
	    is_default: boolean;
	    is_active: boolean;
	    maxTokens: number;
	    modelMaxTokens: number;
	    contextWindow: number;
	
	    static createFrom(source: any = {}) {
	        return new LLMProviderSetting(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.baseURL = source["baseURL"];
	        this.is_default = source["is_default"];
	        this.is_active = source["is_active"];
	        this.maxTokens = source["maxTokens"];
	        this.modelMaxTokens = source["modelMaxTokens"];
	        this.contextWindow = source["contextWindow"];
	    }
	}
	export class QueryResult {
	    columns: string[];
	    rows: any[][];
	    total_rows: number;
	
	    static createFrom(source: any = {}) {
	        return new QueryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.columns = source["columns"];
	        this.rows = source["rows"];
	        this.total_rows = source["total_rows"];
	    }
	}
	export class SchemaColumnPreview {
	    name: string;
	    data_type: string;
	    is_primary_key: boolean;
	    is_nullable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SchemaColumnPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.data_type = source["data_type"];
	        this.is_primary_key = source["is_primary_key"];
	        this.is_nullable = source["is_nullable"];
	    }
	}
	export class SchemaTablePreview {
	    name: string;
	    row_count: number;
	    columns: SchemaColumnPreview[];
	    indexes: number;
	    foreign_keys: number;
	
	    static createFrom(source: any = {}) {
	        return new SchemaTablePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.row_count = source["row_count"];
	        this.columns = this.convertValues(source["columns"], SchemaColumnPreview);
	        this.indexes = source["indexes"];
	        this.foreign_keys = source["foreign_keys"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SchemaPreview {
	    connection_name: string;
	    total_tables: number;
	    tables: SchemaTablePreview[];
	
	    static createFrom(source: any = {}) {
	        return new SchemaPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connection_name = source["connection_name"];
	        this.total_tables = source["total_tables"];
	        this.tables = this.convertValues(source["tables"], SchemaTablePreview);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace models {
	
	export class Conversation {
	    id: number;
	    title?: string;
	    llm_provider_id?: number;
	    data_source_id?: number;
	    status: string;
	    max_messages: number;
	    max_context_messages: number;
	    pinned: boolean;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    // Go type: time
	    deleted_at?: any;
	    tech_details: boolean;
	    context_details: boolean;
	    summarize: boolean;
	    viz_enabled: boolean;
	    streaming_enabled: boolean;
	    tags?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Conversation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.llm_provider_id = source["llm_provider_id"];
	        this.data_source_id = source["data_source_id"];
	        this.status = source["status"];
	        this.max_messages = source["max_messages"];
	        this.max_context_messages = source["max_context_messages"];
	        this.pinned = source["pinned"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.deleted_at = this.convertValues(source["deleted_at"], null);
	        this.tech_details = source["tech_details"];
	        this.context_details = source["context_details"];
	        this.summarize = source["summarize"];
	        this.viz_enabled = source["viz_enabled"];
	        this.streaming_enabled = source["streaming_enabled"];
	        this.tags = source["tags"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ConversationMessage {
	    id: number;
	    conversation_id: number;
	    role: string;
	    content: string;
	    llm_content?: string;
	    sql_results?: string;
	    metadata?: string;
	    tool_transcript?: string;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new ConversationMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.conversation_id = source["conversation_id"];
	        this.role = source["role"];
	        this.content = source["content"];
	        this.llm_content = source["llm_content"];
	        this.sql_results = source["sql_results"];
	        this.metadata = source["metadata"];
	        this.tool_transcript = source["tool_transcript"];
	        this.created_at = this.convertValues(source["created_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiscussionDefaults {
	    llm_provider_id?: number;
	    data_source_id?: number;
	    max_context_messages?: number;
	    max_messages?: number;
	    summarize?: boolean;
	    viz_enabled?: boolean;
	    tech_details?: boolean;
	    context_details?: boolean;
	    streaming_enabled?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DiscussionDefaults(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.llm_provider_id = source["llm_provider_id"];
	        this.data_source_id = source["data_source_id"];
	        this.max_context_messages = source["max_context_messages"];
	        this.max_messages = source["max_messages"];
	        this.summarize = source["summarize"];
	        this.viz_enabled = source["viz_enabled"];
	        this.tech_details = source["tech_details"];
	        this.context_details = source["context_details"];
	        this.streaming_enabled = source["streaming_enabled"];
	    }
	}
	export class Skill {
	    id: number;
	    name: string;
	    markdown_content: string;
	    is_active: boolean;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Skill(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.markdown_content = source["markdown_content"];
	        this.is_active = source["is_active"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace services {
	
	export class ActiveDatabaseInfo {
	    path: string;
	    is_default: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ActiveDatabaseInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.is_default = source["is_default"];
	    }
	}
	export class DBTypeInfo {
	    type: string;
	    display_name: string;
	    default_port: number;
	
	    static createFrom(source: any = {}) {
	        return new DBTypeInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.display_name = source["display_name"];
	        this.default_port = source["default_port"];
	    }
	}
	export class UpdateInfo {
	    current_version: string;
	    latest_version: string;
	    update_available: boolean;
	    release_notes: string;
	    download_url: string;
	    asset_checksum: string;
	    published_at: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current_version = source["current_version"];
	        this.latest_version = source["latest_version"];
	        this.update_available = source["update_available"];
	        this.release_notes = source["release_notes"];
	        this.download_url = source["download_url"];
	        this.asset_checksum = source["asset_checksum"];
	        this.published_at = source["published_at"];
	    }
	}

}

