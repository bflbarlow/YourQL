package models

// AppSetting represents a key-value application setting persisted in the local database.
type AppSetting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
