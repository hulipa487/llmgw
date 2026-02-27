package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"llmgw/middleware"
	"llmgw/models"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// hashKey creates a SHA256 hash of an API key
func hashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

// generateAPIKey generates a random API key with a prefix
func generateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "llmgw_" + hex.EncodeToString(bytes), nil
}

// generateRandomPassword generates a random password
func generateRandomPassword() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// UserLoginRequest represents login request
type UserLoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// UserRegisterRequest represents registration request
type UserRegisterRequest struct {
	Email      string `json:"email" binding:"required,email"`
	Password   string `json:"password" binding:"required,min=6"`
	InviteCode string `json:"invite_code" binding:"required"`
}

// CreateAPIKeyRequest represents API key creation request
type CreateAPIKeyRequest struct {
	Name string `json:"name" binding:"max=100"`
}

// UserLogin handles user login
func UserLogin(c *gin.Context) {
	var req UserLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var user models.User
	if err := models.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Set session
	c.SetCookie("session", fmt.Sprintf("%d", user.ID), 3600*24*7, "/", "", false, true)
	c.SetCookie("userID", fmt.Sprintf("%d", user.ID), 3600*24*7, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{"message": "Login successful", "user": gin.H{
		"id":    user.ID,
		"email": user.Email,
	}})
}

// UserRegister handles user registration
func UserRegister(c *gin.Context) {
	var req UserRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check invite code
	var inviteCode models.InviteCode
	if err := models.DB.Where("code = ? AND used_by IS NULL", req.InviteCode).First(&inviteCode).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or already used invite code"})
		return
	}

	// Check if email exists
	var existingUser models.User
	if models.DB.Where("email = ?", req.Email).First(&existingUser).Error == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Email already registered"})
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// Start transaction
	tx := models.DB.Begin()

	user := models.User{
		Email:        req.Email,
		PasswordHash: string(hashedPassword),
	}

	if err := tx.Create(&user).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	// Mark invite code as used
	now := time.Now()
	inviteCode.UsedBy = &user.ID
	inviteCode.UsedAt = &now
	if err := tx.Save(&inviteCode).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update invite code"})
		return
	}

	tx.Commit()

	c.JSON(http.StatusCreated, gin.H{"message": "User created successfully", "user": gin.H{
		"id":    user.ID,
		"email": user.Email,
	}})
}

// UserLogout handles user logout
func UserLogout(c *gin.Context) {
	c.SetCookie("session", "", -1, "/", "", false, true)
	c.SetCookie("userID", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/login")
}

// AdminLogin handles admin login
func AdminLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var admin models.Admin
	if err := models.DB.Where("username = ?", req.Username).First(&admin).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Set admin session
	c.SetCookie("adminSession", fmt.Sprintf("%d", admin.ID), 3600*24*7, "/", "", false, true)
	c.SetCookie("adminID", fmt.Sprintf("%d", admin.ID), 3600*24*7, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{"message": "Login successful", "admin": gin.H{
		"id":       admin.ID,
		"username": admin.Username,
	}})
}

// AdminLogout handles admin logout
func AdminLogout(c *gin.Context) {
	c.SetCookie("adminSession", "", -1, "/", "", false, true)
	c.SetCookie("adminID", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/admin/login")
}

// CreateAPIKey creates a new API key for the user
func CreateAPIKey(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)

	// Check API key limit
	var activeKeyCount int64
	models.DB.Model(&models.APIKey{}).Where("user_id = ? AND is_active = ?", user.ID, true).Count(&activeKeyCount)
	if activeKeyCount >= models.MaxAPIKeysPerUser {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Maximum of %d active API keys allowed", models.MaxAPIKeysPerUser)})
		return
	}

	var req CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Name is optional
		req.Name = "Default"
	}

	// Generate API key
	key, err := generateAPIKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate API key"})
		return
	}

	keyHash := hashKey(key)
	keyPrefix := key[:8] // First 8 chars for display

	apiKey := models.APIKey{
		KeyHash:   keyHash,
		UserID:    user.ID,
		Name:      req.Name,
		KeyPrefix: keyPrefix,
		IsActive:  true,
	}

	if err := models.DB.Create(&apiKey).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create API key"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":         apiKey.ID,
		"name":       apiKey.Name,
		"key":        key, // Only shown once!
		"key_prefix": keyPrefix,
		"created_at": apiKey.CreatedAt,
	})
}

// ListAPIKeys lists all API keys for the user
func ListAPIKeys(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)

	var keys []models.APIKey
	if err := models.DB.Where("user_id = ?", user.ID).Order("created_at desc").Find(&keys).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch API keys"})
		return
	}

	var result []gin.H
	for _, k := range keys {
		result = append(result, gin.H{
			"id":           k.ID,
			"name":         k.Name,
			"key_prefix":   k.KeyPrefix,
			"is_active":    k.IsActive,
			"created_at":   k.CreatedAt,
			"last_used_at": k.LastUsedAt,
		})
	}

	c.JSON(http.StatusOK, result)
}

// DeleteAPIKey deletes (revokes) an API key
func DeleteAPIKey(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	keyID := c.Param("id")

	result := models.DB.Where("id = ? AND user_id = ?", keyID, user.ID).Delete(&models.APIKey{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete API key"})
		return
	}

	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "API key deleted"})
}

// GetUserUsage gets usage statistics for the user
func GetUserUsage(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)

	now := time.Now()
	windowStart := now.Add(-6 * time.Hour)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	// Get request counts
	var windowRequests, monthRequests int64
	models.DB.Model(&models.UsageLog{}).
		Where("user_id = ? AND created_at >= ?", user.ID, windowStart).
		Count(&windowRequests)
	models.DB.Model(&models.UsageLog{}).
		Where("user_id = ? AND created_at >= ?", user.ID, monthStart).
		Count(&monthRequests)

	// Get tokens by model this month
	type ModelTokens struct {
		ModelName    string
		InputTokens  int64
		OutputTokens int64
	}
	var modelTokens []ModelTokens
	models.DB.Model(&models.UsageLog{}).
		Select("model_name, SUM(input_tokens) as input_tokens, SUM(output_tokens) as output_tokens").
		Where("user_id = ? AND created_at >= ?", user.ID, monthStart).
		Group("model_name").
		Scan(&modelTokens)

	c.JSON(http.StatusOK, gin.H{
		"rate_limits": gin.H{
			"window": gin.H{
				"limit":     models.RateLimitPerWindow,
				"used":      windowRequests,
				"remaining": max(0, models.RateLimitPerWindow-int(windowRequests)),
				"percent":   float64(windowRequests) / float64(models.RateLimitPerWindow) * 100,
			},
			"month": gin.H{
				"limit":     models.RateLimitPerMonth,
				"used":      monthRequests,
				"remaining": max(0, models.RateLimitPerMonth-int(monthRequests)),
				"percent":   float64(monthRequests) / float64(models.RateLimitPerMonth) * 100,
			},
		},
		"tokens_by_model": modelTokens,
	})
}

// GetEnabledModels lists all enabled models for the user
func GetEnabledModels(c *gin.Context) {
	var modelList []models.Model
	if err := models.DB.Where("is_enabled = ?", true).Order("name").Find(&modelList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch models"})
		return
	}

	var result []gin.H
	for _, m := range modelList {
		result = append(result, gin.H{
			"name": m.Name,
		})
	}

	c.JSON(http.StatusOK, result)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// SessionMiddleware extracts session from cookie
func SessionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check for user session
		userIDStr, err := c.Cookie("userID")
		if err == nil && userIDStr != "" {
			var userID uint
			if _, err := fmt.Sscanf(userIDStr, "%d", &userID); err == nil {
				c.Set("userID", userID)
			}
		}

		// Check for admin session
		adminIDStr, err := c.Cookie("adminID")
		if err == nil && adminIDStr != "" {
			var adminID uint
			if _, err := fmt.Sscanf(adminIDStr, "%d", &adminID); err == nil {
				c.Set("adminID", adminID)
			}
		}

		c.Next()
	}
}

// CheckRateLimit checks if user has exceeded rate limit
func CheckRateLimit(userID uint) (bool, int, int) {
	now := time.Now()
	windowStart := now.Add(-6 * time.Hour)

	var windowCount int64
	models.DB.Model(&models.UsageLog{}).
		Where("user_id = ? AND created_at >= ?", userID, windowStart).
		Count(&windowCount)

	if windowCount >= models.RateLimitPerWindow {
		return false, int(windowCount), models.RateLimitPerWindow
	}

	return true, int(windowCount), models.RateLimitPerWindow
}

// CheckMonthlyLimit checks if user has exceeded monthly limit
func CheckMonthlyLimit(userID uint) (bool, int, int) {
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	var monthCount int64
	models.DB.Model(&models.UsageLog{}).
		Where("user_id = ? AND created_at >= ?", userID, monthStart).
		Count(&monthCount)

	if monthCount >= models.RateLimitPerMonth {
		return false, int(monthCount), models.RateLimitPerMonth
	}

	return true, int(monthCount), models.RateLimitPerMonth
}