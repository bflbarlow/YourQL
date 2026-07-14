package services

import (
	"database/sql"
	"time"
	"YourQL/pkg/models"
)

type User struct{}

func NewUser() *User {
	return &User{}
}

func (u *User) GetUserByEmail(email string) (*models.User, error) {
	var user models.User
	var firstNameNull, lastNameNull, nameNull sql.NullString
	var isVerifiedNull sql.NullBool
	var isAdminInt int
	err := models.DB.QueryRow(
		"SELECT id, email, password, first_name, last_name, name, is_verified, is_admin, created_at, updated_at FROM users WHERE email = ? LIMIT 1",
		email,
	).Scan(
		&user.ID, &user.Email, &user.Password,
		&firstNameNull, &lastNameNull, &nameNull,
		&isVerifiedNull,
		&isAdminInt,
		&user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if firstNameNull.Valid {
		user.FirstName = firstNameNull.String
	}
	if lastNameNull.Valid {
		user.LastName = lastNameNull.String
	}
	if nameNull.Valid {
		user.Name = nameNull.String
	}
	if isVerifiedNull.Valid {
		user.IsVerified = isVerifiedNull.Bool
	} else {
		user.IsVerified = false
	}
	user.IsAdmin = isAdminInt == 1
	return &user, nil
}

func (u *User) GetUserByID(id uint) (*models.User, error) {
	var user models.User
	var firstNameNull, lastNameNull, nameNull sql.NullString
	var isVerifiedNull sql.NullBool
	var isAdminInt int
	err := models.DB.QueryRow(
		"SELECT id, email, password, first_name, last_name, name, is_verified, is_admin, created_at, updated_at FROM users WHERE id = ? LIMIT 1",
		id,
	).Scan(
		&user.ID, &user.Email, &user.Password,
		&firstNameNull, &lastNameNull, &nameNull,
		&isVerifiedNull,
		&isAdminInt,
		&user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if firstNameNull.Valid {
		user.FirstName = firstNameNull.String
	}
	if lastNameNull.Valid {
		user.LastName = lastNameNull.String
	}
	if nameNull.Valid {
		user.Name = nameNull.String
	}
	if isVerifiedNull.Valid {
		user.IsVerified = isVerifiedNull.Bool
	} else {
		user.IsVerified = false
	}
	user.IsAdmin = isAdminInt == 1
	return &user, nil
}

func (u *User) EmailExists(email string) (bool, error) {
	var existingEmail string
	err := models.DB.QueryRow("SELECT email FROM users WHERE email = ? LIMIT 1", email).Scan(&existingEmail)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (u *User) CreateUser(email, password string) (int64, error) {
	hashedPassword, err := HashPassword(password)
	if err != nil {
		return 0, err
	}

	now := time.Now().UTC()
	result, err := models.DB.Exec(
		"INSERT INTO users (email, password, is_verified, created_at, updated_at) VALUES (?, ?, 0, ?, ?)",
		email, hashedPassword, now, now,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (u *User) CreateUserWithToken(email, password, token string) (int64, error) {
	hashedPassword, err := HashPassword(password)
	if err != nil {
		return 0, err
	}

	now := time.Now().UTC()
	result, err := models.DB.Exec(
		"INSERT INTO users (email, password, confirm_token, is_verified, created_at, updated_at) VALUES (?, ?, ?, 0, ?, ?)",
		email, hashedPassword, token, now, now,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (u *User) GetUserByConfirmationToken(token string) (string, error) {
	var email string
	err := models.DB.QueryRow(
		"SELECT email FROM users WHERE confirm_token = ? AND (is_verified IS NULL OR is_verified = 0) LIMIT 1",
		token,
	).Scan(&email)
	if err != nil {
		return "", err
	}
	return email, nil
}

func (u *User) VerifyEmail(email string) error {
	_, err := models.DB.Exec("UPDATE users SET is_verified = true, confirm_token = NULL WHERE email = ?", email)
	return err
}

func (u *User) UpdateUserNames(email, firstName, lastName string) error {
	_, err := models.DB.Exec(
		"UPDATE users SET first_name = ?, last_name = ? WHERE email = ?",
		firstName, lastName, email,
	)
	return err
}

func (u *User) ResendConfirmationEmail(email string) error {
	token, err := GenerateConfirmationToken()
	if err != nil {
		return err
	}

	_, err = models.DB.Exec("UPDATE users SET confirm_token = ? WHERE email = ?", token, email)
	if err != nil {
		return err
	}
	return SendConfirmationEmail(email, token)
}
