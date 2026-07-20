export namespace main {
	
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
	    }
	}
	export class GeneralSettings {
	    app_name: string;
	    app_version: string;
	    default_llm_provider: string;
	    theme: string;
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

}

export namespace services {
	
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

}

