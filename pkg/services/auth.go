package services

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"YourQL/pkg/configuration"
	"YourQL/pkg/environment"
	"YourQL/pkg/models"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func GenerateJWT(userID uint, email string) (string, error) {
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"user_id": userID,
		"email":   email,
		"exp":     now.Add(time.Hour * 24).Unix(),
		"iat":     now.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(environment.Jwt_secret))
}

func GenerateJWTWithWorkspace(userID uint, email string, workspaceID uint) (string, error) {
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"user_id":              userID,
		"email":                email,
		"current_workspace_id": workspaceID,
		"exp":                  now.Add(time.Hour * 24).Unix(),
		"iat":                  now.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(environment.Jwt_secret))
}

func ValidateJWT(tokenString string) (*jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(environment.Jwt_secret), nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid or expired token")
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return &claims, nil
	}
	return nil, errors.New("invalid token claims")
}

func GenerateConfirmationToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func GeneratePasswordResetToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func GenerateMagicCode() (string, error) {
	max, _ := new(big.Int).SetString("999999", 10)
	num, err := rand.Int(rand.Reader, max.Add(max, big.NewInt(1)))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", num.Int64()), nil
}

func CreateMagicCode(email string) error {
	code, err := GenerateMagicCode()
	if err != nil {
		return fmt.Errorf("failed to generate magic code: %w", err)
	}

	expiresAt := time.Now().UTC().Add(10 * time.Minute)

	_, err = models.DB.Exec(
		"UPDATE users SET magic_code = ?, magic_code_expires_at = ? WHERE email = ?",
		code, expiresAt, email,
	)
	if err != nil {
		return fmt.Errorf("failed to save magic code: %w", err)
	}

	return SendMagicCodeEmail(email, code)
}

func ValidateMagicCode(email, code string) (bool, error) {
	var storedCode sql.NullString
	var expiresAt sql.NullTime

	err := models.DB.QueryRow(
		"SELECT magic_code, magic_code_expires_at FROM users WHERE email = ? LIMIT 1",
		email,
	).Scan(&storedCode, &expiresAt)

	if err == sql.ErrNoRows {
		return false, errors.New("user not found")
	}
	if err != nil {
		return false, fmt.Errorf("database error: %w", err)
	}

	if !storedCode.Valid || storedCode.String == "" {
		return false, errors.New("no magic code found")
	}
	if storedCode.String != code {
		return false, errors.New("invalid code")
	}
	if !expiresAt.Valid || expiresAt.Time.Before(time.Now()) {
		return false, errors.New("code has expired")
	}

	return true, nil
}

func ClearMagicCode(email string) error {
	_, err := models.DB.Exec(
		"UPDATE users SET magic_code = NULL, magic_code_expires_at = NULL WHERE email = ?",
		email,
	)
	return err
}

func CreatePasswordResetToken(email string) error {
	token, err := GeneratePasswordResetToken()
	if err != nil {
		return fmt.Errorf("failed to generate password reset token: %w", err)
	}

	expiresAt := time.Now().UTC().Add(1 * time.Hour)

	_, err = models.DB.Exec(
		"UPDATE users SET password_reset_token = ?, password_reset_expires_at = ? WHERE email = ?",
		token, expiresAt, email,
	)
	if err != nil {
		return fmt.Errorf("failed to save password reset token: %w", err)
	}

	return SendPasswordResetEmail(email, token)
}

func ValidatePasswordResetToken(token string) (string, error) {
	var email string
	var expiresAt sql.NullTime

	err := models.DB.QueryRow(
		"SELECT email, password_reset_expires_at FROM users WHERE password_reset_token = ? LIMIT 1",
		token,
	).Scan(&email, &expiresAt)

	if err == sql.ErrNoRows {
		return "", errors.New("invalid token")
	}
	if err != nil {
		return "", fmt.Errorf("database error: %w", err)
	}

	if !expiresAt.Valid || expiresAt.Time.Before(time.Now()) {
		return "", errors.New("token has expired")
	}

	return email, nil
}

func ClearPasswordResetToken(email string) error {
	_, err := models.DB.Exec(
		"UPDATE users SET password_reset_token = NULL, password_reset_expires_at = NULL WHERE email = ?",
		email,
	)
	return err
}

func ResetPassword(email, newPassword string) error {
	hashedPassword, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	_, err = models.DB.Exec(
		"UPDATE users SET password = ?, password_reset_token = NULL, password_reset_expires_at = NULL WHERE email = ?",
		hashedPassword, email,
	)
	return err
}

func LogLoginAttempt(userID uint, email string, success bool, method string, ipAddress string, userAgent string, failureReason string) error {
	return LogAuthEvent(userID, email, "login", success, method, ipAddress, userAgent, failureReason)
}

func LogLogout(userID uint, email, ipAddress, userAgent string) error {
	return LogAuthEvent(userID, email, "logout", true, "", ipAddress, userAgent, "")
}

func LogAuthEvent(userID uint, email, action string, success bool, method string, ipAddress string, userAgent string, failureReason string) error {
	_, err := models.DB.Exec(
		"INSERT INTO logins (user_id, email, action, success, method, ip_address, user_agent, failure_reason) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		userID, email, action, success, method, ipAddress, userAgent, failureReason,
	)
	return err
}

func LogPasswordLogin(email, password, ipAddress, userAgent string) (string, error) {
	if err := CheckAccountLockout(email); err != nil {
		return "", err
	}

	var userID uint
	err := models.DB.QueryRow("SELECT id FROM users WHERE email = ? LIMIT 1", email).Scan(&userID)

	if err == sql.ErrNoRows {
		LogLoginAttempt(0, email, false, "password", ipAddress, userAgent, "user not found")
		return "", errors.New("invalid email or password")
	}
	if err != nil {
		LogLoginAttempt(0, email, false, "password", ipAddress, userAgent, "database error")
		return "", fmt.Errorf("database error: %w", err)
	}

	var storedPassword string
	err = models.DB.QueryRow("SELECT password FROM users WHERE id = ? LIMIT 1", userID).Scan(&storedPassword)
	if err != nil {
		LogLoginAttempt(userID, email, false, "password", ipAddress, userAgent, "password lookup failed")
		return "", fmt.Errorf("database error: %w", err)
	}

	if !CheckPassword(password, storedPassword) {
		HandleFailedLogin(email, userID)
		LogLoginAttempt(userID, email, false, "password", ipAddress, userAgent, "invalid password")
		return "", errors.New("invalid email or password")
	}

	ResetFailedAttempts(email)
	LogLoginAttempt(userID, email, true, "password", ipAddress, userAgent, "")
	return GenerateJWT(userID, email)
}

func LogMagicCodeLogin(email, code, ipAddress, userAgent string) (string, error) {
	if err := CheckAccountLockout(email); err != nil {
		return "", err
	}

	var userID uint
	err := models.DB.QueryRow("SELECT id FROM users WHERE email = ? LIMIT 1", email).Scan(&userID)
	if err == sql.ErrNoRows {
		LogLoginAttempt(0, email, false, "magic_code", ipAddress, userAgent, "user not found")
		return "", errors.New("invalid or expired code")
	}

	valid, err := ValidateMagicCode(email, code)
	if !valid {
		failureReason := "invalid code"
		if err != nil {
			failureReason = err.Error()
		}
		LogLoginAttempt(userID, email, false, "magic_code", ipAddress, userAgent, failureReason)
		return "", errors.New("invalid or expired code")
	}

	ClearMagicCode(email)
	ResetFailedAttempts(email)
	LogLoginAttempt(userID, email, true, "magic_code", ipAddress, userAgent, "")
	return GenerateJWT(userID, email)
}

func CheckAccountLockout(email string) error {
	var lockedUntil sql.NullTime
	err := models.DB.QueryRow(
		"SELECT locked_until FROM users WHERE email = ? LIMIT 1",
		email,
	).Scan(&lockedUntil)

	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("database error checking lockout: %w", err)
	}

	now := time.Now().UTC()
	if lockedUntil.Valid && !lockedUntil.Time.Before(now) {
		remainingTime := time.Until(lockedUntil.Time).Minutes()
		return fmt.Errorf("account is locked. Please try again in %.0f minutes", remainingTime)
	}

	if lockedUntil.Valid && lockedUntil.Time.Before(now) {
		UnlockAccount(email)
	}

	return nil
}

func HandleFailedLogin(email string, userID uint) {
	var currentAttempts int
	err := models.DB.QueryRow(
		"SELECT COALESCE(failed_login_attempts, 0) FROM users WHERE email = ? LIMIT 1",
		email,
	).Scan(&currentAttempts)

	if err != nil {
		return
	}

	newAttempts := currentAttempts + 1

	_, err = models.DB.Exec(
		"UPDATE users SET failed_login_attempts = ?, last_failed_login_at = CURRENT_TIMESTAMP WHERE email = ?",
		newAttempts, email,
	)
	if err != nil {
		return
	}

	if newAttempts >= configuration.Config.Lockout.MaxAttempts {
		LockAccount(email, userID)
	}
}

func LockAccount(email string, userID uint) {
	lockUntil := time.Now().Add(time.Duration(configuration.Config.Lockout.DurationMinutes) * time.Minute)

	_, err := models.DB.Exec(
		"UPDATE users SET locked_until = ?, failed_login_attempts = ? WHERE email = ?",
		lockUntil, configuration.Config.Lockout.MaxAttempts, email,
	)
	if err != nil {
		return
	}

	go SendLockoutNotificationEmail(email, userID, lockUntil)
}

func UnlockAccount(email string) {
	_, err := models.DB.Exec(
		"UPDATE users SET locked_until = NULL WHERE email = ?",
		email,
	)
	if err != nil {
		return
	}
}

func ResetFailedAttempts(email string) {
	_, err := models.DB.Exec(
		"UPDATE users SET failed_login_attempts = 0, last_failed_login_at = NULL WHERE email = ?",
		email,
	)
	if err != nil {
		return
	}
}
