package services

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"YourQL/pkg/models"
)

func CreateQuery(conversationID *uint, question string, llmProviderID, dataSourceID *uint) (*models.Query, error) {
	now := time.Now().UTC()
	result, err := models.DB.Exec(
		"INSERT INTO queries (conversation_id, question, original_query, llm_provider_id, data_source_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		conversationID, question, question, llmProviderID, dataSourceID, "pending", now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create query: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get query ID: %w", err)
	}

	return GetQueryByID(uint(id))
}

func GetQueryByID(id uint) (*models.Query, error) {
	var q models.Query
	var convID, llmID, dbConnID sql.NullInt64
	var genSQL, resultSummary, errMsg, costEstimate sql.NullString
	var execTime, tokensUsed sql.NullInt64
	err := models.DB.QueryRow(
		`SELECT id, conversation_id, question, generated_sql, data_source_id, llm_provider_id, status, result_summary, error_message, execution_time_ms, tokens_used, cost_estimate, created_at, updated_at FROM queries WHERE id = ?`,
		id,
	).Scan(
		&q.ID, &convID, &q.Question, &genSQL,
		&dbConnID, &llmID, &q.Status, &resultSummary, &errMsg,
		&execTime, &tokensUsed, &costEstimate, &q.CreatedAt, &q.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("query not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get query: %w", err)
	}
	if convID.Valid {
		cid := uint(convID.Int64)
		q.ConversationID = &cid
	}
	if llmID.Valid {
		lid := uint(llmID.Int64)
		q.LLMProviderID = &lid
	}
	if dbConnID.Valid {
		dbid := uint(dbConnID.Int64)
		q.DataSourceID = &dbid
	}
	if genSQL.Valid {
		s := genSQL.String
		q.GeneratedSQL = &s
	}
	if resultSummary.Valid {
		s := resultSummary.String
		q.ResultSummary = &s
	}
	if errMsg.Valid {
		s := errMsg.String
		q.ErrorMessage = &s
	}
	if execTime.Valid {
		e := int(execTime.Int64)
		q.ExecutionTimeMS = &e
	}
	if tokensUsed.Valid {
		t := int(tokensUsed.Int64)
		q.TokensUsed = &t
	}
	if costEstimate.Valid {
		s := costEstimate.String
		q.CostEstimate = &s
	}
	return &q, nil
}

func UpdateQueryStatus(id uint, status string, generatedSQL *string, resultSummary *string, errorMessage *string, executionTimeMS *int, tokensUsed *int, costEstimate *string) error {
	updates := []string{"status = ?", "updated_at = ?"}
	args := []interface{}{status, time.Now().UTC()}

	if generatedSQL != nil {
		updates = append(updates, "generated_sql = ?")
		args = append(args, *generatedSQL)
	}
	if resultSummary != nil {
		updates = append(updates, "result_summary = ?")
		args = append(args, *resultSummary)
	}
	if errorMessage != nil {
		updates = append(updates, "error_message = ?")
		args = append(args, *errorMessage)
	}
	if executionTimeMS != nil {
		updates = append(updates, "execution_time_ms = ?")
		args = append(args, *executionTimeMS)
	}
	if tokensUsed != nil {
		updates = append(updates, "tokens_used = ?")
		args = append(args, *tokensUsed)
	}
	if costEstimate != nil {
		updates = append(updates, "cost_estimate = ?")
		args = append(args, *costEstimate)
	}

	query := fmt.Sprintf("UPDATE queries SET %s WHERE id = ?", strings.Join(updates, ", "))
	args = append(args, id)
	_, err := models.DB.Exec(query, args...)
	return err
}
