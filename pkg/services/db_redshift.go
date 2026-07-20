package services

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"

	"YourQL/pkg/models"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func init() {
	RegisterDriver(&RedshiftDriver{})
}

// RedshiftDriver implements DBDriver for Amazon Redshift.
// Redshift is based on PostgreSQL and uses the pgx driver.
type RedshiftDriver struct{}

func (d *RedshiftDriver) TypeKey() string      { return "redshift" }
func (d *RedshiftDriver) OpenDriver() string   { return "pgx" }
func (d *RedshiftDriver) DisplayName() string  { return "Redshift" }
func (d *RedshiftDriver) DefaultPort() int     { return 5439 }
func (d *RedshiftDriver) SQLDialectHint() string {
	return "Redshift — PostgreSQL-compatible dialect; DISTKEY/SORTKEY table options, COPY/UNLOAD commands, no SERIAL type (use IDENTITY), no indexes (use SORTKEY), VACUUM/ANALYZE"
}

// RedshiftExtra holds Redshift-specific connection parameters.
type RedshiftExtra struct {
	SSLMode    string `json:"sslmode,omitempty"`    // disable, require, verify-ca, verify-full
	SearchPath string `json:"search_path,omitempty"`
}

func parseRedshiftExtra(conn *models.DBConnection) RedshiftExtra {
	if conn.Extra == nil || *conn.Extra == "" {
		return RedshiftExtra{SSLMode: "require"}
	}
	var extra RedshiftExtra
	if err := json.Unmarshal([]byte(*conn.Extra), &extra); err != nil {
		return RedshiftExtra{SSLMode: "require"}
	}
	return extra
}

func (d *RedshiftDriver) BuildDSN(conn *models.DBConnection) (string, error) {
	if conn.Database == nil {
		return "", fmt.Errorf("database name is required")
	}

	host := "localhost"
	if conn.Host != nil && *conn.Host != "" {
		host = *conn.Host
	}

	port := 5439
	if conn.Port != nil {
		port = *conn.Port
	}

	username := ""
	if conn.Username != nil {
		username = *conn.Username
	}

	password := ""
	if conn.Password != nil {
		password = *conn.Password
	}

	extra := parseRedshiftExtra(conn)

	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(username, password),
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   *conn.Database,
	}

	q := u.Query()
	sslMode := extra.SSLMode
	if sslMode == "" {
		sslMode = "require"
	}
	q.Set("sslmode", sslMode)
	if extra.SearchPath != "" {
		q.Set("search_path", extra.SearchPath)
	}

	u.RawQuery = q.Encode()

	log.Printf("[redshift] Built DSN for database %s, user %s, host %s:%d", *conn.Database, username, host, port)
	return u.String(), nil
}

func (d *RedshiftDriver) GetSchema(conn *models.DBConnection) (*DatabaseSchema, error) {
	// Reuse the PostgreSQL driver's GetSchema — identical INFORMATION_SCHEMA.
	pgDriver := &PostgresDriver{}
	return pgDriver.GetSchema(conn)
}