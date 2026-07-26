package services

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"YourQL/pkg/models"
)

func CreateDataSource(name, dbType, host string, port int, database, username, password, sslMode string, configJSON, extraJSON string, filePath, fileType string) (*models.DataSource, error) {
	now := time.Now().UTC()
	isActive := true

	var configArg, extraArg interface{}
	if configJSON == "" {
		configArg = nil
	} else {
		configArg = configJSON
	}
	if extraJSON == "" {
		extraArg = nil
	} else {
		extraArg = extraJSON
	}

	result, err := models.DB.Exec(
		"INSERT INTO data_sources (name, type, host, port, database_name, username, password, ssl_mode, is_default, is_active, config, extra, file_path, file_type, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		name, dbType, host, port, database, username, password, sslMode, false, isActive, configArg, extraArg, filePath, fileType, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create database connection: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection ID: %w", err)
	}

	conn := &models.DataSource{
		ID:       uint(id),
		Name:     name,
		Type:     dbType,
		IsActive: isActive,
		CreatedAt: now,
		UpdatedAt: now,
	}

	return conn, nil
}

func GetDataSourceByID(id uint) (*models.DataSource, error) {
	var c models.DataSource
	var hostNull, databaseNull, usernameNull, passwordNull, sslModeNull sql.NullString
	var configNull, extraNull, filePathNull, fileTypeNull, authConfigNull []byte
	var portNull sql.NullInt64
	err := models.DB.QueryRow(
		"SELECT id, name, type, host, port, database_name, username, password, ssl_mode, is_default, is_active, config, extra, file_path, file_type, auth_config, created_at, updated_at FROM data_sources WHERE id = ? LIMIT 1",
		id,
	).Scan(
		&c.ID, &c.Name, &c.Type, &hostNull, &portNull,
		&databaseNull, &usernameNull, &passwordNull, &sslModeNull,
		&c.IsDefault, &c.IsActive, &configNull, &extraNull, &filePathNull, &fileTypeNull, &authConfigNull, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, errors.New("database connection not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}
	if hostNull.Valid {
		c.Host = &hostNull.String
	}
	if portNull.Valid {
		p := int(portNull.Int64)
		c.Port = &p
	}
	if databaseNull.Valid {
		c.Database = &databaseNull.String
	}
	if usernameNull.Valid {
		c.Username = &usernameNull.String
	}
	if passwordNull.Valid {
		c.Password = &passwordNull.String
	}
	if sslModeNull.Valid {
		c.SSLMode = &sslModeNull.String
	}
	if len(configNull) > 0 {
		s := string(configNull)
		c.Config = &s
	}
	if len(extraNull) > 0 {
		s := string(extraNull)
		c.Extra = &s
	}
	if len(filePathNull) > 0 {
		s := string(filePathNull)
		c.FilePath = &s
	}
	if len(fileTypeNull) > 0 {
		s := string(fileTypeNull)
		c.FileType = &s
	}
	if len(filePathNull) > 0 {
		s := string(filePathNull)
		c.FilePath = &s
	}
	if len(fileTypeNull) > 0 {
		s := string(fileTypeNull)
		c.FileType = &s
	}
	if len(authConfigNull) > 0 {
		s := string(authConfigNull)
		c.AuthConfig = &s
	}
	return &c, nil
}

func ListDataSourcesByWorkspace() ([]*models.DataSource, error) {
	rows, err := models.DB.Query(
		"SELECT id, name, type, host, port, database_name, username, password, ssl_mode, is_default, is_active, config, extra, file_path, file_type, created_at, updated_at FROM data_sources ORDER BY is_default DESC, created_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list database connections: %w", err)
	}
	defer rows.Close()

	var connections []*models.DataSource
	for rows.Next() {
		var c models.DataSource
		var hostNull, databaseNull, usernameNull, passwordNull, sslModeNull sql.NullString
		var configNull, extraNull, filePathNull, fileTypeNull []byte
		var portNull sql.NullInt64
		err := rows.Scan(
			&c.ID, &c.Name, &c.Type, &hostNull, &portNull,
			&databaseNull, &usernameNull, &passwordNull, &sslModeNull,
			&c.IsDefault, &c.IsActive, &configNull, &extraNull, &filePathNull, &fileTypeNull, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			continue
		}
		if hostNull.Valid {
			c.Host = &hostNull.String
		}
		if portNull.Valid {
			p := int(portNull.Int64)
			c.Port = &p
		}
		if databaseNull.Valid {
			c.Database = &databaseNull.String
		}
		if usernameNull.Valid {
			c.Username = &usernameNull.String
		}
		if passwordNull.Valid {
			c.Password = &passwordNull.String
		}
		if sslModeNull.Valid {
			c.SSLMode = &sslModeNull.String
		}
		if len(configNull) > 0 {
			s := string(configNull)
			c.Config = &s
		}
		if len(extraNull) > 0 {
			s := string(extraNull)
			c.Extra = &s
		}
		if len(filePathNull) > 0 {
			s := string(filePathNull)
			c.FilePath = &s
		}
		if len(fileTypeNull) > 0 {
			s := string(fileTypeNull)
			c.FileType = &s
		}
		connections = append(connections, &c)
	}
	return connections, nil
}

func UpdateDataSource(id uint, name *string, host *string, port *int, database *string, username *string, password *string, sslMode *string, configJSON *string, extraJSON *string, filePath, fileType *string) (*models.DataSource, error) {
	c, err := GetDataSourceByID(id)
	if err != nil {
		return nil, err
	}

	updates := make([]string, 0)
	args := []interface{}{}

	if name != nil {
		updates = append(updates, "name = ?")
		args = append(args, *name)
	}
	if host != nil {
		updates = append(updates, "host = ?")
		args = append(args, *host)
	}
	if port != nil {
		updates = append(updates, "port = ?")
		args = append(args, *port)
	}
	if database != nil {
		updates = append(updates, "database_name = ?")
		args = append(args, *database)
	}
	if username != nil {
		updates = append(updates, "username = ?")
		args = append(args, *username)
	}
	if password != nil && *password != "" {
		updates = append(updates, "password = ?")
		args = append(args, *password)
	}
	if sslMode != nil {
		updates = append(updates, "ssl_mode = ?")
		args = append(args, *sslMode)
	}
	if configJSON != nil {
		updates = append(updates, "config = ?")
		if *configJSON == "" {
			args = append(args, nil)
		} else {
			args = append(args, *configJSON)
		}
	}
	if extraJSON != nil {
		updates = append(updates, "extra = ?")
		if *extraJSON == "" {
			args = append(args, nil)
		} else {
			args = append(args, *extraJSON)
		}
	}

	if filePath != nil {
		updates = append(updates, "file_path = ?")
		args = append(args, *filePath)
	}
	if fileType != nil {
		updates = append(updates, "file_type = ?")
		args = append(args, *fileType)
	}

	if len(updates) == 0 {
		return c, nil
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE data_sources SET %s, updated_at = CURRENT_TIMESTAMP WHERE id = ?", strings.Join(updates, ", "))
	_, err = models.DB.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to update database connection: %w", err)
	}

	return GetDataSourceByID(id)
}

func DeleteDataSource(id uint) error {
	c, err := GetDataSourceByID(id)
	if err != nil {
		return err
	}

	if c.IsDefault {
		return errors.New("cannot delete default database connection; set another as default first")
	}

	var count int
	err = models.DB.QueryRow(
		"SELECT COUNT(*) FROM conversations WHERE data_source_id = ? AND status = 'active'",
		id,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check conversation references: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("cannot delete: %d active conversations reference this connection", count)
	}

	_, err = models.DB.Exec("DELETE FROM data_sources WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete database connection: %w", err)
	}
	return nil
}

func SetDefaultDataSource(connectionID uint) error {
	var exists bool
	err := models.DB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM data_sources WHERE id = ?)",
		connectionID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to verify connection: %w", err)
	}
	if !exists {
		return errors.New("connection not found")
	}

	_, err = models.DB.Exec("UPDATE data_sources SET is_default = 0")
	if err != nil {
		return fmt.Errorf("failed to unset previous defaults: %w", err)
	}

	_, err = models.DB.Exec("UPDATE data_sources SET is_default = 1 WHERE id = ?", connectionID)
	if err != nil {
		return fmt.Errorf("failed to set default: %w", err)
	}

	return nil
}

func GetDefaultDataSource() (*models.DataSource, error) {
	var c models.DataSource
	var hostNull, databaseNull, usernameNull, passwordNull, sslModeNull sql.NullString
	var configNull, extraNull, filePathNull, fileTypeNull []byte
	var portNull sql.NullInt64
	err := models.DB.QueryRow(
		"SELECT id, name, type, host, port, database_name, username, password, ssl_mode, is_default, is_active, config, extra, file_path, file_type, created_at, updated_at FROM data_sources WHERE is_default = 1 LIMIT 1",
	).Scan(
		&c.ID, &c.Name, &c.Type, &hostNull, &portNull,
		&databaseNull, &usernameNull, &passwordNull, &sslModeNull,
		&c.IsDefault, &c.IsActive, &configNull, &extraNull, &filePathNull, &fileTypeNull, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get default database connection: %w", err)
	}
	log.Printf("[data_source] Found default data source ID %d", c.ID)
	if hostNull.Valid {
		c.Host = &hostNull.String
	}
	if portNull.Valid {
		p := int(portNull.Int64)
		c.Port = &p
	}
	if databaseNull.Valid {
		c.Database = &databaseNull.String
	}
	if usernameNull.Valid {
		c.Username = &usernameNull.String
	}
	if passwordNull.Valid {
		c.Password = &passwordNull.String
	}
	if sslModeNull.Valid {
		c.SSLMode = &sslModeNull.String
	}
	if len(configNull) > 0 {
		s := string(configNull)
		c.Config = &s
	}
	if len(extraNull) > 0 {
		s := string(extraNull)
		c.Extra = &s
	}
	if len(filePathNull) > 0 {
		s := string(filePathNull)
		c.FilePath = &s
	}
	if len(fileTypeNull) > 0 {
		s := string(fileTypeNull)
		c.FileType = &s
	}
	return &c, nil
}
