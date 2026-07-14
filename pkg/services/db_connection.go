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

// CreateDBConnection creates a new database connection for a workspace.
func CreateDBConnection(workspaceID, createdBy uint, name, dbType, host string, port int, database, username, password, sslMode string, configJSON string) (*models.DBConnection, error) {
	isMember, err := IsWorkspaceMember(workspaceID, createdBy)
	if err != nil {
		return nil, fmt.Errorf("failed to check workspace membership: %w", err)
	}
	if !isMember {
		return nil, errors.New("user is not a member of this workspace")
	}

	hasPermission, err := CheckPermission(workspaceID, createdBy, "can_manage_db")
	if err != nil {
		return nil, err
	}
	if !hasPermission {
		return nil, errors.New("insufficient permissions to manage database connections")
	}

	now := time.Now().UTC()
	isActive := true

	// Handle empty config as NULL for JSON column
	var configArg interface{}
	if configJSON == "" {
		configArg = nil
	} else {
		configArg = configJSON
	}

	result, err := models.DB.Exec(
		"INSERT INTO db_connections (workspace_id, name, type, host, port, `database`, username, password, ssl_mode, is_default, is_active, config, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		workspaceID, name, dbType, host, port, database, username, password, sslMode, false, isActive, configArg, createdBy, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create database connection: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection ID: %w", err)
	}

	conn := &models.DBConnection{
		ID:          uint(id),
		WorkspaceID: workspaceID,
		Name:        name,
		Type:        dbType,
		IsActive:    isActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	return conn, nil
}

// GetDBConnectionByID retrieves a database connection by ID.
func GetDBConnectionByID(id uint) (*models.DBConnection, error) {
	var c models.DBConnection
	var hostNull, databaseNull, usernameNull, passwordNull, sslModeNull sql.NullString
	var configNull []byte
	var portNull, createdByNull sql.NullInt64
	err := models.DB.QueryRow(
		"SELECT id, workspace_id, name, type, host, port, `database`, username, password, ssl_mode, is_default, is_active, config, created_by, created_at, updated_at FROM db_connections WHERE id = ? LIMIT 1",
		id,
	).Scan(
		&c.ID, &c.WorkspaceID, &c.Name, &c.Type, &hostNull, &portNull,
		&databaseNull, &usernameNull, &passwordNull, &sslModeNull,
		&c.IsDefault, &c.IsActive, &configNull, &createdByNull, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, errors.New("database connection not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}
	if createdByNull.Valid {
		cb := uint(createdByNull.Int64)
		c.CreatedBy = &cb
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
	return &c, nil
}

// ListDBConnectionsByWorkspace lists all database connections for a workspace.
func ListDBConnectionsByWorkspace(workspaceID uint) ([]*models.DBConnection, error) {
	rows, err := models.DB.Query(
		"SELECT id, workspace_id, name, type, host, port, `database`, username, password, ssl_mode, is_default, is_active, config, created_by, created_at, updated_at FROM db_connections WHERE workspace_id = ? ORDER BY is_default DESC, created_at DESC",
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list database connections: %w", err)
	}
	defer rows.Close()

	var connections []*models.DBConnection
	for rows.Next() {
		var c models.DBConnection
		var hostNull, databaseNull, usernameNull, passwordNull, sslModeNull sql.NullString
		var configNull []byte
		var portNull, createdByNull sql.NullInt64
		err := rows.Scan(
			&c.ID, &c.WorkspaceID, &c.Name, &c.Type, &hostNull, &portNull,
			&databaseNull, &usernameNull, &passwordNull, &sslModeNull,
			&c.IsDefault, &c.IsActive, &configNull, &createdByNull, &c.CreatedAt, &c.UpdatedAt,
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
		if createdByNull.Valid {
			cb := uint(createdByNull.Int64)
			c.CreatedBy = &cb
		}
		connections = append(connections, &c)
	}
	return connections, nil
}

// UpdateDBConnection updates a database connection's configuration.
func UpdateDBConnection(id uint, name *string, host *string, port *int, database *string, username *string, password *string, sslMode *string, configJSON *string) (*models.DBConnection, error) {
	c, err := GetDBConnectionByID(id)
	if err != nil {
		return nil, err
	}

	var createdBy uint
	if c.CreatedBy != nil {
		createdBy = *c.CreatedBy
	}
	isMember, err := IsWorkspaceMember(c.WorkspaceID, createdBy)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, errors.New("user is not a member of this workspace")
	}

	hasPermission, err := CheckPermission(c.WorkspaceID, createdBy, "can_manage_db")
	if err != nil {
		return nil, err
	}
	if !hasPermission {
		return nil, errors.New("insufficient permissions to update database connection")
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
		updates = append(updates, "`database` = ?")
		args = append(args, *database)
	}
	if username != nil {
		updates = append(updates, "username = ?")
		args = append(args, *username)
	}
	if password != nil {
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

	if len(updates) == 0 {
		return c, nil
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE db_connections SET %s, updated_at = CURRENT_TIMESTAMP WHERE id = ?", strings.Join(updates, ", "))
	_, err = models.DB.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to update database connection: %w", err)
	}

	return GetDBConnectionByID(id)
}

// DeleteDBConnection deletes a database connection.
func DeleteDBConnection(id uint) error {
	c, err := GetDBConnectionByID(id)
	if err != nil {
		return err
	}

	if c.IsDefault {
		return errors.New("cannot delete default database connection; set another as default first")
	}

	var count int
	err = models.DB.QueryRow(
		"SELECT COUNT(*) FROM conversations WHERE db_connection_id = ? AND status = 'active'",
		id,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check conversation references: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("cannot delete: %d active conversations reference this connection", count)
	}

	_, err = models.DB.Exec("DELETE FROM db_connections WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete database connection: %w", err)
	}
	return nil
}

// SetDefaultDBConnection sets a database connection as the default for a workspace.
func SetDefaultDBConnection(workspaceID, connectionID uint) error {
	var exists bool
	err := models.DB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM db_connections WHERE id = ? AND workspace_id = ?)",
		connectionID, workspaceID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to verify connection: %w", err)
	}
	if !exists {
		return errors.New("connection not found in workspace")
	}

	_, err = models.DB.Exec(
		"UPDATE db_connections SET is_default = 0 WHERE workspace_id = ?",
		workspaceID,
	)
	if err != nil {
		return fmt.Errorf("failed to unset previous defaults: %w", err)
	}

	_, err = models.DB.Exec(
		"UPDATE db_connections SET is_default = 1 WHERE id = ?",
		connectionID,
	)
	if err != nil {
		return fmt.Errorf("failed to set default: %w", err)
	}

	// Update workspace default_db_connection field
	_, err = models.DB.Exec(
		"UPDATE workspaces SET default_db_connection = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		connectionID, workspaceID,
	)
	if err != nil {
		return fmt.Errorf("failed to update workspace default: %w", err)
	}

	return nil
}

// GetDefaultDBConnection retrieves the default database connection for a workspace.
func GetDefaultDBConnection(workspaceID uint) (*models.DBConnection, error) {
	var c models.DBConnection
	var hostNull, databaseNull, usernameNull, passwordNull, sslModeNull sql.NullString
	var configNull []byte
	var portNull, createdByNull sql.NullInt64
	err := models.DB.QueryRow(
		"SELECT id, workspace_id, name, type, host, port, `database`, username, password, ssl_mode, is_default, is_active, config, created_by, created_at, updated_at FROM db_connections WHERE workspace_id = ? AND is_default = 1 LIMIT 1",
		workspaceID,
	).Scan(
		&c.ID, &c.WorkspaceID, &c.Name, &c.Type, &hostNull, &portNull,
		&databaseNull, &usernameNull, &passwordNull, &sslModeNull,
		&c.IsDefault, &c.IsActive, &configNull, &createdByNull, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get default database connection: %w", err)
	}
	log.Printf("[db_connection] Found default DB connection ID %d for workspace %d", c.ID, workspaceID)
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
	if createdByNull.Valid {
		cb := uint(createdByNull.Int64)
		c.CreatedBy = &cb
	}
	if len(configNull) > 0 {
		s := string(configNull)
		c.Config = &s
	}
	return &c, nil
}
