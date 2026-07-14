package controllers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"YourQL/pkg/environment"
	"YourQL/pkg/models"
	"YourQL/pkg/services"
	"YourQL/pkg/utils"

	"github.com/gin-gonic/gin"
)

type LoginInput struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type MagicCodeRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type MagicCodeLoginInput struct {
	Email string `json:"email" binding:"required,email"`
	Code  string `json:"code" binding:"required,min=6,max=6"`
}

// ensurePersonalWorkspace creates a personal workspace for a verified user if org features are enabled
func ensurePersonalWorkspace(userID uint, email string) {
	if environment.EnableOrgFeatures() != "true" {
		return
	}

	// Check if user is verified
	var isVerified bool
	err := models.DB.QueryRow("SELECT is_verified FROM users WHERE id = ?", userID).Scan(&isVerified)
	if err != nil || !isVerified {
		return
	}

	// Create personal workspace if it doesn't exist
	_, err = services.CreatePersonalWorkspace(userID, email)
	if err != nil {
		log.Printf("[ensurePersonalWorkspace] Failed to create personal workspace for user %d: %v", userID, err)
	}
}

type RegisterInput struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

var userService = services.NewUser()

func Login(c *gin.Context) {
	var input LoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	email := utils.SanitizeEmail(input.Email)
	password := utils.SanitizePassword(input.Password)

	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid email format"})
		return
	}

	ipAddress := utils.SanitizeIP(c.ClientIP())
	userAgent := utils.SanitizeUserAgent(c.GetHeader("User-Agent"))

	token, err := services.LogPasswordLogin(email, password, ipAddress, userAgent)
	if err != nil {
		errMsg := "Invalid email or password"
		if strings.Contains(err.Error(), "account is locked") {
			errMsg = err.Error()
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": errMsg})
		return
	}

	user, err := userService.GetUserByEmail(input.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// Ensure personal workspace exists for verified users
	ensurePersonalWorkspace(user.ID, user.Email)

	c.SetCookie("token", token, 3600*24, "/", "", false, false)

	c.JSON(http.StatusOK, gin.H{
		"message": "Login successful",
		"token":   token,
		"user": gin.H{
			"id":    user.ID,
			"email": user.Email,
		},
	})
}

func LoginForm(c *gin.Context) {
	email := utils.SanitizeEmail(c.PostForm("email"))
	password := utils.SanitizePassword(c.PostForm("password"))
	redirectTo := c.PostForm("redirect_to")

	if email == "" || password == "" {
		c.HTML(http.StatusBadRequest, "pages/login.tpl", gin.H{"error": "Email and password are required", "redirect": redirectTo})
		return
	}

	ipAddress := utils.SanitizeIP(c.ClientIP())
	userAgent := utils.SanitizeUserAgent(c.GetHeader("User-Agent"))

	token, err := services.LogPasswordLogin(email, password, ipAddress, userAgent)
	if err != nil {
		errMsg := "Invalid email or password"
		if strings.Contains(err.Error(), "account is locked") {
			errMsg = err.Error()
		}
		c.HTML(http.StatusUnauthorized, "pages/login.tpl", gin.H{"error": errMsg, "redirect": redirectTo})
		return
	}

	// Get user to ensure personal workspace
	user, err := userService.GetUserByEmail(email)
	if err == nil {
		ensurePersonalWorkspace(user.ID, user.Email)
	}

	c.SetCookie("token", token, 3600*24, "/", "", false, false)

	redirectURL := c.DefaultPostForm("redirect_to", "/account")
	c.Redirect(http.StatusSeeOther, redirectURL)
}

func Register(c *gin.Context) {
	var input RegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	email := utils.SanitizeEmail(input.Email)
	password := utils.SanitizePassword(input.Password)

	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid email format"})
		return
	}

	if passwordErr := utils.ValidatePasswordStrength(password); passwordErr != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": passwordErr})
		return
	}

	exists, err := userService.EmailExists(input.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}
	if exists {
		c.JSON(http.StatusConflict, gin.H{"error": "User with this email already exists"})
		return
	}

	confirmationToken, err := services.GenerateConfirmationToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate confirmation token"})
		return
	}

	userID, err := userService.CreateUserWithToken(input.Email, input.Password, confirmationToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create user"})
		return
	}

	if sendErr := services.SendConfirmationEmail(input.Email, confirmationToken); sendErr != nil {
		// Non-fatal: user can still log in
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "User created successfully.",
		"user": gin.H{
			"id":    userID,
			"email": input.Email,
		},
	})
}

func RegisterForm(c *gin.Context) {
	email := utils.SanitizeEmail(c.PostForm("email"))
	password := utils.SanitizePassword(c.PostForm("password"))

	if email == "" || password == "" {
		c.HTML(http.StatusBadRequest, "pages/signup.tpl", gin.H{"error": "Email and password are required"})
		return
	}

	if passwordErr := utils.ValidatePasswordStrength(password); passwordErr != "" {
		c.HTML(http.StatusBadRequest, "pages/signup.tpl", gin.H{"error": passwordErr})
		return
	}

	exists, err := userService.EmailExists(email)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "pages/signup.tpl", gin.H{"error": "Database error"})
		return
	}
	if exists {
		c.HTML(http.StatusConflict, "pages/signup.tpl", gin.H{"error": "User with this email already exists"})
		return
	}

	confirmationToken, err := services.GenerateConfirmationToken()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "pages/signup.tpl", gin.H{"error": "Could not generate confirmation token"})
		return
	}

	_, err = userService.CreateUserWithToken(email, password, confirmationToken)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "pages/signup.tpl", gin.H{"error": "Could not create user"})
		return
	}

	services.SendConfirmationEmail(email, confirmationToken)

	c.HTML(http.StatusOK, "pages/login.tpl", gin.H{"success": "Account created successfully! Please check your email to confirm your account."})
}

func ConfirmEmail(c *gin.Context) {
	token := utils.SanitizeToken(c.Query("token"))
	if token == "" {
		c.HTML(http.StatusBadRequest, "pages/home.tpl", gin.H{"error": "Invalid or missing confirmation token"})
		return
	}

	email, err := userService.GetUserByConfirmationToken(token)
	if err == sql.ErrNoRows {
		c.HTML(http.StatusBadRequest, "pages/home.tpl", gin.H{"error": "Invalid or expired confirmation token"})
		return
	}
	if err != nil {
		c.HTML(http.StatusInternalServerError, "pages/home.tpl", gin.H{"error": "Database error"})
		return
	}

	if err := userService.VerifyEmail(email); err != nil {
		c.HTML(http.StatusInternalServerError, "pages/home.tpl", gin.H{"error": "Could not verify email"})
		return
	}

	// Get the user to create their personal workspace
	user, err := userService.GetUserByEmail(email)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "pages/home.tpl", gin.H{"error": "Could not load user"})
		return
	}

	// Create personal workspace if org features are enabled
	var personalWorkspace *models.Workspace
	if environment.EnableOrgFeatures() == "true" {
		personalWorkspace, err = services.CreatePersonalWorkspace(user.ID, user.Email)
		if err != nil {
			// Non-fatal: user can still log in, workspace will be created on first login
			log.Printf("[ConfirmEmail] Failed to create personal workspace for user %d: %v", user.ID, err)
		}
	}

	// Build redirect URL
	var redirectURL string
	if personalWorkspace != nil {
		redirectURL = fmt.Sprintf("/app/%s", personalWorkspace.Slug)
	} else {
		redirectURL = "/app"
	}

	c.HTML(http.StatusOK, "pages/home.tpl", gin.H{
		"success": "Email confirmed successfully! You can now log in.",
		"redirect": redirectURL,
	})
}

func RequestMagicCode(c *gin.Context) {
	var input MagicCodeRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	email := utils.SanitizeEmail(input.Email)
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid email format"})
		return
	}

	exists, err := userService.EmailExists(input.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Email does not exist"})
		return
	}

	if err := services.CreateMagicCode(input.Email); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not send login code"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "A 6-digit login code has been sent to your email.",
	})
}

func RequestMagicCodeForm(c *gin.Context) {
	email := utils.SanitizeEmail(c.PostForm("email"))
	redirectTo := c.PostForm("redirect_to")

	if email == "" {
		c.HTML(http.StatusBadRequest, "pages/login.tpl", gin.H{"error": "Invalid email format", "redirect": redirectTo})
		return
	}

	exists, err := userService.EmailExists(email)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "pages/login.tpl", gin.H{"error": "Database error", "redirect": redirectTo})
		return
	}
	if !exists {
		c.HTML(http.StatusNotFound, "pages/login.tpl", gin.H{"error": "Email does not exist", "redirect": redirectTo})
		return
	}

	if err := services.CreateMagicCode(email); err != nil {
		c.HTML(http.StatusInternalServerError, "pages/login.tpl", gin.H{"error": "Could not send login code", "redirect": redirectTo})
		return
	}

	redirectURL := "/login?show_code_form=true&email=" + email
	if redirectTo != "" {
		redirectURL += "&redirect=" + redirectTo
	}
	c.Redirect(http.StatusSeeOther, redirectURL)
}

func LoginWithCode(c *gin.Context) {
	var input MagicCodeLoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	email := utils.SanitizeEmail(input.Email)
	code := utils.SanitizeCode(input.Code)

	if email == "" || code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid email or code format"})
		return
	}

	ipAddress := utils.SanitizeIP(c.ClientIP())
	userAgent := utils.SanitizeUserAgent(c.GetHeader("User-Agent"))

	token, err := services.LogMagicCodeLogin(input.Email, input.Code, ipAddress, userAgent)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired code"})
		return
	}

	user, err := userService.GetUserByEmail(input.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// Ensure personal workspace exists for verified users
	ensurePersonalWorkspace(user.ID, user.Email)

	c.SetCookie("token", token, 3600*24, "/", "", false, false)

	c.JSON(http.StatusOK, gin.H{
		"message": "Login successful",
		"token":   token,
		"user": gin.H{
			"id":    user.ID,
			"email": user.Email,
		},
	})
}

func LoginWithCodeForm(c *gin.Context) {
	email := utils.SanitizeEmail(c.PostForm("email"))
	code := utils.SanitizeCode(c.PostForm("code"))
	redirectTo := c.PostForm("redirect_to")

	if email == "" || code == "" {
		c.HTML(http.StatusBadRequest, "pages/login.tpl", gin.H{"error": "Invalid email or code format", "redirect": redirectTo})
		return
	}

	ipAddress := utils.SanitizeIP(c.ClientIP())
	userAgent := utils.SanitizeUserAgent(c.GetHeader("User-Agent"))

	token, err := services.LogMagicCodeLogin(email, code, ipAddress, userAgent)
	if err != nil {
		c.HTML(http.StatusUnauthorized, "pages/login.tpl", gin.H{"error": "Invalid or expired code", "redirect": redirectTo})
		return
	}

	// Get user to ensure personal workspace
	user, err := userService.GetUserByEmail(email)
	if err == nil {
		ensurePersonalWorkspace(user.ID, user.Email)
	}

	c.SetCookie("token", token, 3600*24, "/", "", false, false)

	redirectURL := "/account"
	if redirectTo != "" {
		redirectURL = redirectTo
	}
	c.Redirect(http.StatusSeeOther, redirectURL)
}

func ForgotPasswordForm(c *gin.Context) {
	c.HTML(http.StatusOK, "pages/forgot-password.tpl", nil)
}

func ForgotPassword(c *gin.Context) {
	email := utils.SanitizeEmail(c.PostForm("email"))

	if email == "" {
		c.HTML(http.StatusBadRequest, "pages/forgot-password.tpl", gin.H{"error": "Invalid email format"})
		return
	}

	exists, err := userService.EmailExists(email)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "pages/forgot-password.tpl", gin.H{"error": "Database error"})
		return
	}
	if !exists {
		c.HTML(http.StatusOK, "pages/forgot-password.tpl", gin.H{"success": "If an account exists with this email, a password reset link has been sent."})
		return
	}

	if err := services.CreatePasswordResetToken(email); err != nil {
		c.HTML(http.StatusInternalServerError, "pages/forgot-password.tpl", gin.H{"error": "Could not send password reset link"})
		return
	}

	c.HTML(http.StatusOK, "pages/forgot-password.tpl", gin.H{"success": "A password reset link has been sent to your email."})
}

func ResetPasswordForm(c *gin.Context) {
	token := utils.SanitizeToken(c.Query("token"))
	if token == "" {
		c.HTML(http.StatusBadRequest, "pages/reset-password.tpl", gin.H{"error": "Invalid or missing reset token"})
		return
	}

	email, err := services.ValidatePasswordResetToken(token)
	if err != nil {
		c.HTML(http.StatusBadRequest, "pages/reset-password.tpl", gin.H{"error": "Invalid or expired reset token"})
		return
	}

	c.Set("reset_email", email)
	c.Set("reset_token", token)
	c.HTML(http.StatusOK, "pages/reset-password.tpl", gin.H{"token": token})
}

func ResetPassword(c *gin.Context) {
	token := utils.SanitizeToken(c.PostForm("token"))
	newPassword := utils.SanitizePassword(c.PostForm("password"))
	confirmPassword := utils.SanitizePassword(c.PostForm("confirm_password"))

	if token == "" {
		c.HTML(http.StatusBadRequest, "pages/reset-password.tpl", gin.H{"error": "Invalid reset token"})
		return
	}
	if newPassword == "" {
		c.HTML(http.StatusBadRequest, "pages/reset-password.tpl", gin.H{"error": "Password is required"})
		return
	}
	if passwordErr := utils.ValidatePasswordStrength(newPassword); passwordErr != "" {
		c.HTML(http.StatusBadRequest, "pages/reset-password.tpl", gin.H{"error": passwordErr})
		return
	}
	if newPassword != confirmPassword {
		c.HTML(http.StatusBadRequest, "pages/reset-password.tpl", gin.H{"error": "Passwords do not match"})
		return
	}

	email, err := services.ValidatePasswordResetToken(token)
	if err != nil {
		c.HTML(http.StatusBadRequest, "pages/reset-password.tpl", gin.H{"error": "Invalid or expired reset token"})
		return
	}

	if err := services.ResetPassword(email, newPassword); err != nil {
		c.HTML(http.StatusInternalServerError, "pages/reset-password.tpl", gin.H{"error": "Could not reset password"})
		return
	}

	c.HTML(http.StatusOK, "pages/reset-password.tpl", gin.H{"success": "Password reset successful! You can now log in."})
}

func Logout(c *gin.Context) {
	userIDVal, _ := c.Get("user_id")
	email := c.GetString("email")

	var userIDInt uint
	if userIDFloat, ok := userIDVal.(float64); ok {
		userIDInt = uint(userIDFloat)
	} else if userIDStr, ok := userIDVal.(uint); ok {
		userIDInt = userIDStr
	}

	ipAddress := utils.SanitizeIP(c.ClientIP())
	userAgent := utils.SanitizeUserAgent(c.GetHeader("User-Agent"))
	if email != "" {
		services.LogLogout(userIDInt, email, ipAddress, userAgent)
	}

	c.SetCookie("token", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/login?loggedout=true")
}
