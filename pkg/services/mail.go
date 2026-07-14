package services

import (
	"crypto/tls"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
	"time"
	"YourQL/pkg/configuration"
	"YourQL/pkg/environment"
	"YourQL/pkg/models"
)

// appURL builds a full URL for the application using the configured domain.
// On production (APP_ENV != development) it uses https://; on development it uses http://.
func appURL(path string) string {
	prefix := "http://"
	if environment.App_env != "development" {
		prefix = "https://"
	}
	return prefix + environment.App_domain + path
}

// getSMTPAuth returns an smtp.Auth for the configured SMTP credentials.
// Returns nil when no credentials are configured (e.g. local MailHog).
func getSMTPAuth() smtp.Auth {
	if environment.Smtp_username != "" && environment.Smtp_password != "" {
		return smtp.PlainAuth("", environment.Smtp_username, environment.Smtp_password, environment.Smtp_host)
	}
	return nil
}

// sendMail sends an email with optional STARTTLS support.
func sendMail(addr string, from string, to []string, msg []byte) error {
	auth := getSMTPAuth()

	if strings.EqualFold(environment.Smtp_encryption, "STARTTLS") {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to connect to SMTP server: %w", err)
		}

		// Pass the SMTP host as the hostname for EHLO — required for AWS SES
		client, err := smtp.NewClient(conn, environment.Smtp_host)
		if err != nil {
			conn.Close()
			return fmt.Errorf("failed to create SMTP client: %w", err)
		}

		if err := client.StartTLS(&tls.Config{ServerName: environment.Smtp_host}); err != nil {
			client.Close()
			conn.Close()
			return fmt.Errorf("failed to start TLS: %w", err)
		}

		if auth != nil {
			if err := client.Auth(auth); err != nil {
				client.Close()
				conn.Close()
				return fmt.Errorf("SMTP auth failed after TLS: %w", err)
			}
		}

		if err := client.Mail(from); err != nil {
			client.Close()
			conn.Close()
			return fmt.Errorf("failed to set sender: %w", err)
		}

		for _, recipient := range to {
			if err := client.Rcpt(recipient); err != nil {
				client.Close()
				conn.Close()
				return fmt.Errorf("failed to set recipient %s: %w", recipient, err)
			}
		}

		writer, err := client.Data()
		if err != nil {
			client.Close()
			conn.Close()
			return fmt.Errorf("failed to open data writer: %w", err)
		}

		if _, err := writer.Write(msg); err != nil {
			writer.Close()
			client.Close()
			conn.Close()
			return fmt.Errorf("failed to write message: %w", err)
		}

		if err := writer.Close(); err != nil {
			client.Close()
			conn.Close()
			return fmt.Errorf("failed to close data writer: %w", err)
		}

		client.Close()
		conn.Close()
		return nil
	}

	return smtp.SendMail(addr, auth, from, to, msg)
}

// LogEmail records an email in the emails table
func LogEmail(recipient, subject, emailType, status string, errorMessage string) error {
	var errMsg sql.NullString
	if errorMessage != "" {
		errMsg.String = errorMessage
		errMsg.Valid = true
	}

	_, err := models.DB.Exec(
		"INSERT INTO emails (recipient, subject, email_type, status, error_message) VALUES (?, ?, ?, ?, ?)",
		recipient, subject, emailType, status, errMsg,
	)
	return err
}

// SendConfirmationEmail sends an email with a confirmation link
func SendConfirmationEmail(to, token string) error {
	host := environment.Smtp_host
	port := environment.Smtp_port
	from := environment.From_email

	addr := fmt.Sprintf("%s:%s", host, port)

	confirmationLink := appURL("/confirm-email?token=" + token)

	// Get template from configuration and replace placeholders
	bodyTemplate := configuration.EmailTemplates.Confirmation.Body
	body := strings.ReplaceAll(bodyTemplate, "{{.URL}}", confirmationLink)
	body = strings.ReplaceAll(body, "{{.Token}}", token)

	// Add footer for essential emails
	body += "\n\n---\nYou're receiving this email because it's related to your account. Essential account emails cannot be opted out. [Manage your email preferences]" + appURL("/email-preferences?email=" + to)

	msg := []byte("To: " + to + "\r\n" +
		"From: " + from + "\r\n" +
		"Subject: " + configuration.EmailTemplates.Confirmation.Subject + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" +
		body + "\r\n")

	err := sendMail(addr, from, []string{to}, msg)
	if err != nil {
		LogEmail(to, configuration.EmailTemplates.Confirmation.Subject, "confirmation", "failed", err.Error())
	} else {
		LogEmail(to, configuration.EmailTemplates.Confirmation.Subject, "confirmation", "sent", "")
	}
	return err
}

// SendPasswordResetEmail sends an email with a password reset link
func SendPasswordResetEmail(to, token string) error {
	host := environment.Smtp_host
	port := environment.Smtp_port
	from := environment.From_email

	addr := fmt.Sprintf("%s:%s", host, port)

	resetLink := appURL("/reset-password?token=" + token)

	// Get template from configuration and replace placeholders
	bodyTemplate := configuration.EmailTemplates.PasswordReset.Body
	body := strings.ReplaceAll(bodyTemplate, "{{.URL}}", resetLink)
	body = strings.ReplaceAll(body, "{{.Token}}", token)

	// Add footer for essential emails
	body += "\n\n---\nYou're receiving this email because it's related to your account. Essential account emails cannot be opted out. [Manage your email preferences]" + appURL("/email-preferences?email=" + to)

	msg := []byte("To: " + to + "\r\n" +
		"From: " + from + "\r\n" +
		"Subject: " + configuration.EmailTemplates.PasswordReset.Subject + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" +
		body + "\r\n")

	err := sendMail(addr, from, []string{to}, msg)
	if err != nil {
		LogEmail(to, configuration.EmailTemplates.PasswordReset.Subject, "password_reset", "failed", err.Error())
	} else {
		LogEmail(to, configuration.EmailTemplates.PasswordReset.Subject, "password_reset", "sent", "")
	}
	return err
}

// SendMagicCodeEmail sends an email with a 6-digit login code
func SendMagicCodeEmail(to, code string) error {
	host := environment.Smtp_host
	port := environment.Smtp_port
	from := environment.From_email

	addr := fmt.Sprintf("%s:%s", host, port)

	// Get template from configuration and replace placeholders
	bodyTemplate := configuration.EmailTemplates.MagicCode.Body
	body := strings.ReplaceAll(bodyTemplate, "{{.Code}}", code)

	// Add footer for essential emails
	body += "\n\n---\nYou're receiving this email because it's related to your account. Essential account emails cannot be opted out. [Manage your email preferences]" + appURL("/email-preferences?email=" + to)

	msg := []byte("To: " + to + "\r\n" +
		"From: " + from + "\r\n" +
		"Subject: " + configuration.EmailTemplates.MagicCode.Subject + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" +
		body + "\r\n")

	err := sendMail(addr, from, []string{to}, msg)
	if err != nil {
		LogEmail(to, configuration.EmailTemplates.MagicCode.Subject, "magic_code", "failed", err.Error())
	} else {
		LogEmail(to, configuration.EmailTemplates.MagicCode.Subject, "magic_code", "sent", "")
	}
	return err
}

// SendLockoutNotificationEmail sends an email when account is locked out with password reset link
func SendLockoutNotificationEmail(to string, userID uint, lockUntil time.Time) error {
	host := environment.Smtp_host
	port := environment.Smtp_port
	from := environment.From_email

	addr := fmt.Sprintf("%s:%s", host, port)

	// Generate a password reset token for the user
	token, err := GeneratePasswordResetToken()
	if err != nil {
		log.Printf("Failed to generate password reset token for %s: %v", to, err)
		LogEmail(to, configuration.EmailTemplates.Lockout.Subject, "lockout_notification", "failed", fmt.Sprintf("Failed to generate token: %v", err))
		return fmt.Errorf("failed to generate password reset token: %w", err)
	}

	// Store the token in the database
	expiresAt := time.Now().Add(1 * time.Hour)
	_, err = models.DB.Exec(
		"UPDATE users SET password_reset_token = ?, password_reset_expires_at = ? WHERE id = ?",
		token, expiresAt, userID,
	)
	if err != nil {
		log.Printf("Failed to store password reset token for %s: %v", to, err)
		LogEmail(to, configuration.EmailTemplates.Lockout.Subject, "lockout_notification", "failed", fmt.Sprintf("Failed to store token: %v", err))
		return fmt.Errorf("failed to store password reset token: %w", err)
	}

	resetLink := appURL("/reset-password?token=" + token)
	remainingMinutes := int(time.Until(lockUntil).Minutes())

	// Get template from configuration and replace placeholders
	bodyTemplate := configuration.EmailTemplates.Lockout.Body
	body := strings.ReplaceAll(bodyTemplate, "{{.Minutes}}", fmt.Sprintf("%d", remainingMinutes))
	body = strings.ReplaceAll(body, "{{.URL}}", resetLink)
	body = strings.ReplaceAll(body, "{{.ResetLink}}", resetLink)

	msg := []byte("To: " + to + "\r\n" +
		"From: " + from + "\r\n" +
		"Subject: " + configuration.EmailTemplates.Lockout.Subject + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" +
		body + "\r\n")

	err = sendMail(addr, from, []string{to}, msg)
	if err != nil {
		log.Printf("Failed to send lockout notification email to %s: %v", to, err)
		LogEmail(to, configuration.EmailTemplates.Lockout.Subject, "lockout_notification", "failed", err.Error())
		return fmt.Errorf("failed to send lockout notification email: %w", err)
	}
	log.Printf("Lockout notification email sent successfully to %s", to)
	LogEmail(to, configuration.EmailTemplates.Lockout.Subject, "lockout_notification", "sent", "")
	return nil
}

// ShouldSendMarketingEmail stub - always returns true
func ShouldSendMarketingEmail(email string) bool {
	return true
}

// ShouldSendInviteEmail stub - always returns true
func ShouldSendInviteEmail(email string) bool {
	return true
}