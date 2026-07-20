package services

import (
	"fmt"
	"log"

	"YourQL/pkg/models"

	_ "github.com/go-sql-driver/mysql"
)

func init() {
	RegisterDriver(&MariaDBDriver{})
}

// MariaDBDriver implements DBDriver for MariaDB.
// Reuses the MySQL wire protocol driver since MariaDB is protocol-compatible.
type MariaDBDriver struct{}

func (d *MariaDBDriver) TypeKey() string      { return "mariadb" }
func (d *MariaDBDriver) OpenDriver() string   { return "mysql" }
func (d *MariaDBDriver) DisplayName() string  { return "MariaDB" }
func (d *MariaDBDriver) DefaultPort() int     { return 3306 }
func (d *MariaDBDriver) SQLDialectHint() string {
	return "MariaDB — backtick `identifier` quoting, LIMIT, RETURNING clause on INSERT/UPDATE/DELETE, sequences via CREATE SEQUENCE, ENGINE=Aria/ColumnStore options"
}

func (d *MariaDBDriver) BuildDSN(conn *models.DataSource) (string, error) {
	if conn.Database == nil {
		return "", fmt.Errorf("database name is required")
	}
	database := *conn.Database

	host := "localhost"
	if conn.Host != nil && *conn.Host != "" {
		host = *conn.Host
	}

	port := 3306
	if conn.Port != nil {
		port = *conn.Port
	}

	username := "root"
	if conn.Username != nil {
		username = *conn.Username
	}

	password := ""
	if conn.Password != nil {
		password = *conn.Password
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true", username, password, host, port, database)

	log.Printf("[mariadb] Built DSN for database %s, user %s, host %s:%d", database, username, host, port)
	return dsn, nil
}

func (d *MariaDBDriver) GetSchema(conn *models.DataSource) (*DataSchema, error) {
	if conn.Database == nil {
		return nil, fmt.Errorf("database name is required")
	}

	// Reuse the MySQL driver's GetSchema logic since MariaDB has the same INFORMATION_SCHEMA.
	mysqlDriver := &MySQLDriver{}
	return mysqlDriver.GetSchema(conn)
}