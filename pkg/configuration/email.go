package configuration

// EmailConfig holds all email template configurations
type EmailConfig struct {
	Confirmation  ConfirmationEmail  `json:"confirmation"`
	MagicCode     MagicCodeEmail     `json:"magic_code"`
	PasswordReset PasswordResetEmail `json:"password_reset"`
	Lockout       LockoutEmail       `json:"lockout"`
	Test          TestEmail          `json:"test"`
	TicketTransaction TicketTransactionEmail `json:"ticket_transaction"`
	ScannerInvite       ScannerInviteEmail       `json:"scanner_invite"`
	ScannerInviteNewUser ScannerInviteNewUserEmail `json:"scanner_invite_new_user"`
}

// ConfirmationEmail holds the email confirmation template configuration
type ConfirmationEmail struct {
	Subject string `json:"subject"`
	Body    string `json:"body"` // Use {{.Token}} and {{.URL}} as placeholders
}

// MagicCodeEmail holds the magic login code email template configuration
type MagicCodeEmail struct {
	Subject string `json:"subject"`
	Body    string `json:"body"` // Use {{.Code}} as placeholder
}

// PasswordResetEmail holds the password reset email template configuration
type PasswordResetEmail struct {
	Subject string `json:"subject"`
	Body    string `json:"body"` // Use {{.URL}} and {{.Token}} as placeholders
}

// LockoutEmail holds the account lockout notification email template configuration
type LockoutEmail struct {
	Subject string `json:"subject"`
	Body    string `json:"body"` // Use {{.Minutes}}, {{.URL}}, {{.ResetLink}} as placeholders
}

// TestEmail holds the test email template configuration
type TestEmail struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// TicketTransactionEmail holds the ticket transaction confirmation email template configuration
type TicketTransactionEmail struct {
	Subject string `json:"subject"`
	Body    string `json:"body"` // For enhanced emails, use SendEnhancedTicketTransactionEmail which includes full event details
}

// ScannerInviteEmail holds the scanner invitation email template for existing users
type ScannerInviteEmail struct {
	Subject string `json:"subject"`
	Body    string `json:"body"` // Use {{.EventTitle}}, {{.URL}}, {{.InvitedByName}} as placeholders
}

// ScannerInviteNewUserEmail holds the scanner invitation email template for new users
type ScannerInviteNewUserEmail struct {
	Subject string `json:"subject"`
	Body    string `json:"body"` // Use {{.EventTitle}}, {{.URL}}, {{.InvitedByName}}, {{.SignupURL}} as placeholders
}

// DefaultEmailConfig returns default email configurations with templates
func DefaultEmailConfig() EmailConfig {
	return EmailConfig{
		Confirmation: ConfirmationEmail{
			Subject: "Confirm Your Email Address",
			Body: `Hello,

Please click the following link to confirm your email address:
{{.URL}}

If you did not create an account, please ignore this email.`,
		},
		MagicCode: MagicCodeEmail{
			Subject: "Your Login Code",
			Body: `Hello,

Your 6-digit login code is: {{.Code}}

This code will expire in 10 minutes.

If you didn't request this code, please ignore this email.`,
		},
		PasswordReset: PasswordResetEmail{
			Subject: "Password Reset Request",
			Body: `Hello,

We received a request to reset your password. Click the link below to set a new password:
{{.URL}}

This link will expire in 1 hour.

If you did not request a password reset, please ignore this email and your password will remain unchanged.`,
		},
		Lockout: LockoutEmail{
			Subject: "Account Locked - Password Reset Required",
			Body: `Hello,

Your account has been temporarily locked due to multiple failed login attempts.

This is a security measure to protect your account from unauthorized access.

Your account will be automatically unlocked in {{.Minutes}} minutes.

If you need immediate access, you can reset your password using the link below:
{{.URL}}

This password reset link will expire in 1 hour.

If you believe this was a mistake or you did not attempt to log in, please ignore this email.`,
		},
		Test: TestEmail{
			Subject: "Test Email",
			Body: `This is a test email from Tix App.

If you received this, your email configuration is working correctly!`,
		},
		TicketTransaction: TicketTransactionEmail{
			Subject: "Your Ticket Transaction Confirmation",
			Body: `Hello,

Thank you for your ticket purchase!

Transaction ID: {{.TransactionID}}
Date: {{.TransactionCreatedAt}}
Total Amount: ${{.TotalAmount}}
Status: {{.TransactionStatus}}

--- YOUR TICKETS ---
{{range .EventDateTickets}}
  Event: {{.EventTitle}}
  Date/Time: {{.EventDateStart}} to {{.EventDateEnd}} ({{.EventDateTimezone}})
  Venue: {{.VenueName}}, {{.City}}

  Tickets:
  {{range .Tickets}}    - {{.TicketType}} x{{.Quantity}} @ ${{.Price}} each
      Ticket Code(s): {{.TicketCodes}}
      QR Code URL: {{.QRCodeURL}}
  {{end}}
{{end}}

View your transaction details and manage your tickets:
{{.URL}}

If you have any questions, please contact us.

Thank you,
Tix App Team`,
		},
		ScannerInvite: ScannerInviteEmail{
			Subject: "You've been invited to scan tickets for {{.EventTitle}}",
			Body: `Hello,

You've been invited by {{.InvitedByName}} to scan tickets for the event:

Event: {{.EventTitle}}

As a scanner, you'll be able to:
- Scan tickets at the event entrance
- Check attendees in using QR codes
- View real-time attendance data

To accept this invitation and start scanning, click the link below:
{{.URL}}

This invitation will expire in 7 days.

If you'd prefer not to receive platform invite emails in the future, you can opt out here:
{{.UnsubscribeURL}}

Note: If you opt out of platform invites, you won't be invited as a scanner or for similar roles in the future.

If you believe you received this invitation by mistake, please ignore this email.

Thank you,
Tix App Team`,
		},
		ScannerInviteNewUser: ScannerInviteNewUserEmail{
			Subject: "Join Tix App as a scanner for {{.EventTitle}}",
			Body: `Hello,

You've been invited by {{.InvitedByName}} to join Tix App as a scanner for the event:

Event: {{.EventTitle}}

As a scanner, you'll be able to:
- Scan tickets at the event entrance
- Check attendees in using QR codes
- View real-time attendance data

To accept this invitation, you'll need to create a Tix App account first.
Click the link below to sign up and accept the invitation:
{{.SignupURL}}

This invitation will expire in 7 days.

If you'd prefer not to receive platform invite emails in the future, you can opt out here:
{{.UnsubscribeURL}}

Note: If you opt out of platform invites, you won't be invited as a scanner or for similar roles in the future.

If you believe you received this invitation by mistake, please ignore this email.

Welcome to Tix App!
The Tix App Team`,
		},
	}
}

// EmailTemplates holds the global email template configuration
var EmailTemplates = DefaultEmailConfig()