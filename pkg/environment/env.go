package environment

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

var Jwt_secret string
var Db_conn_str string
var Db_host string
var Db_port string
var Db_user string
var Db_password string
var Db_hostport string
var Db_database string
var App_port string
var Smtp_host string
var Smtp_port string
var Smtp_username string
var Smtp_password string
var Smtp_encryption string
var From_email string

// Application Settings
var App_domain string
var App_env string

// Stripe Configuration
var Stripe_publishable_key string
var Stripe_secret_key string
var Stripe_webhook_secret string
var Stripe_webhook_snapshot_secret string
var Stripe_webhook_thin_secret string
var Stripe_success_url string
var Stripe_cancel_url string
var Stripe_currency string

// Workspace Configuration
var Workspace_max_workspaces_per_user string
var Workspace_default_role string
var Workspace_encryption_key string

// Organization Features Feature Flag
var Enable_org_features_value string

// LLM Configuration
var Llm_provider string
var Llm_model string
var Llm_timeout string
var Llm_rate_limit string
var Llm_max_tokens string
var Llm_master_key string

func init() {
	if err := godotenv.Load(".env"); err != nil {
		log.Println("warning: could not load .env file:", err)
	}
	Jwt_secret = os.Getenv("JWT_SECRET")
	if Jwt_secret == "" {
		log.Println("warning: JWT_SECRET not set in environment")
	}
	Db_conn_str = os.Getenv("DB_CONN_STR")
	if Db_conn_str == "" {
		log.Println("warning: DB_CONN_STR not set in environment")
	}
	Db_host = os.Getenv("DB_HOST")
	if Db_host == "" {
		log.Println("warning: DB_HOST not set in environment")
	}
	Db_port = os.Getenv("DB_PORT")
	if Db_port == "" {
		log.Println("warning: DB_PORT not set in environment")
	}
	Db_user = os.Getenv("DB_USER")
	if Db_user == "" {
		log.Println("warning: DB_USER not set in environment")
	}
	Db_password = os.Getenv("DB_PASSWORD")
	if Db_password == "" {
		log.Println("warning: DB_PASSWORD not set in environment")
	}
	Db_hostport = os.Getenv("DB_HOSTPORT")
	if Db_hostport == "" {
		log.Println("warning: DB_HOSTPORT not set in environment")
	}
	Db_database = os.Getenv("DB_DATABASE")
	if Db_database == "" {
		log.Println("warning: DB_DATABASE not set in environment")
	}
	App_port = os.Getenv("APP_PORT")
	if App_port == "" {
		log.Println("warning: APP_PORT not set in environment")
	}
	Smtp_host = os.Getenv("SMTP_HOST")
	if Smtp_host == "" {
		log.Println("warning: SMTP_HOST not set in environment")
	}
	Smtp_port = os.Getenv("SMTP_PORT")
	if Smtp_port == "" {
		log.Println("warning: SMTP_PORT not set in environment")
	}
	Smtp_username = os.Getenv("SMTP_USERNAME")
	Smtp_password = os.Getenv("SMTP_PASSWORD")
	Smtp_encryption = os.Getenv("SMTP_ENCRYPTION")
	if Smtp_encryption == "" {
		log.Println("warning: SMTP_ENCRYPTION not set in environment")
	}
	From_email = os.Getenv("FROM_EMAIL")
	if From_email == "" {
		log.Println("warning: FROM_EMAIL not set in environment")
	}
	
	App_domain = os.Getenv("APP_DOMAIN")
	if App_domain == "" {
		App_domain = "localhost"
		log.Println("info: APP_DOMAIN not set, defaulting to 'localhost'")
	}
	
	App_env = os.Getenv("APP_ENV")
	if App_env == "" {
		App_env = "development"
		log.Println("info: APP_ENV not set, defaulting to 'development'")
	}
	
	// Stripe Configuration
	Stripe_publishable_key = os.Getenv("STRIPE_PUBLISHABLE_KEY")
	if Stripe_publishable_key == "" {
		log.Println("warning: STRIPE_PUBLISHABLE_KEY not set in environment")
	}
	Stripe_secret_key = os.Getenv("STRIPE_SECRET_KEY")
	if Stripe_secret_key == "" {
		log.Println("warning: STRIPE_SECRET_KEY not set in environment")
	}
	Stripe_webhook_secret = os.Getenv("STRIPE_WEBHOOK_SECRET")
	if Stripe_webhook_secret == "" {
		log.Println("warning: STRIPE_WEBHOOK_SECRET not set in environment")
	}
	Stripe_webhook_snapshot_secret = os.Getenv("STRIPE_WEBHOOK_SNAPSHOT_SECRET")
	Stripe_webhook_thin_secret = os.Getenv("STRIPE_WEBHOOK_THIN_SECRET")
	Stripe_success_url = os.Getenv("STRIPE_SUCCESS_URL")
	if Stripe_success_url == "" {
		log.Println("warning: STRIPE_SUCCESS_URL not set in environment")
	}
	Stripe_cancel_url = os.Getenv("STRIPE_CANCEL_URL")
	if Stripe_cancel_url == "" {
		log.Println("warning: STRIPE_CANCEL_URL not set in environment")
	}
	Stripe_currency = os.Getenv("STRIPE_CURRENCY")
	if Stripe_currency == "" {
		Stripe_currency = "usd"
		log.Println("info: STRIPE_CURRENCY not set, defaulting to 'usd'")
	}

	// Workspace Configuration
	Workspace_max_workspaces_per_user = os.Getenv("WORKSPACE_MAX_WORKSPACES_PER_USER")
	Workspace_default_role = os.Getenv("WORKSPACE_DEFAULT_ROLE")
	Workspace_encryption_key = os.Getenv("WORKSPACE_ENCRYPTION_KEY")
	if Workspace_encryption_key == "" {
		log.Println("warning: WORKSPACE_ENCRYPTION_KEY not set in environment")
	}

	// LLM Configuration
	Llm_provider = os.Getenv("LLM_PROVIDER")
	Llm_model = os.Getenv("LLM_MODEL")
	Llm_timeout = os.Getenv("LLM_TIMEOUT")
	Llm_rate_limit = os.Getenv("LLM_RATE_LIMIT")
	Llm_max_tokens = os.Getenv("LLM_MAX_TOKENS")
	Llm_master_key = os.Getenv("LLM_MASTER_KEY")
	if Llm_master_key == "" {
		log.Println("warning: LLM_MASTER_KEY not set in environment")
	}

	// Organization Features Feature Flag
	Enable_org_features_value = os.Getenv("ENABLE_ORG_FEATURES")
	if Enable_org_features_value == "" {
		Enable_org_features_value = "false"
	}
}

// EnableOrgFeatures returns the ENABLE_ORG_FEATURES env var (default "false").
func EnableOrgFeatures() string {
	return Enable_org_features_value
}