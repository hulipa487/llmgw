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
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// UserRegisterRequest represents registration request
type UserRegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	Password string `json:"password" binding:"required,min=6"`
	Email    string `json:"email" binding:"required,email"`
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
	if err := models.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
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
		"id":       user.ID,
		"username": user.Username,
		"email":    user.Email,
	}})
}

// UserRegister handles user registration
func UserRegister(c *gin.Context) {
	var req UserRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if username exists
	var existingUser models.User
	if models.DB.Where("username = ?", req.Username).First(&existingUser).Error == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Username already exists"})
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	user := models.User{
		Username:     req.Username,
		PasswordHash: string(hashedPassword),
		Email:        req.Email,
	}

	if err := models.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "User created successfully", "user": gin.H{
		"id":       user.ID,
		"username": user.Username,
		"email":    user.Email,
	}})
}

// UserLogout handles user logout
func UserLogout(c *gin.Context) {
	c.SetCookie("session", "", -1, "/", "", false, true)
	c.SetCookie("userID", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

// AdminLogin handles admin login
func AdminLogin(c *gin.Context) {
	var req UserLoginRequest
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
	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

// CreateAPIKey creates a new API key for the user
func CreateAPIKey(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)

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

	// Get date range from query
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	query := models.DB.Model(&models.UsageLog{}).Where("user_id = ?", user.ID)

	if startDate != "" {
		if t, err := time.Parse("2006-01-02", startDate); err == nil {
			query = query.Where("created_at >= ?", t)
		}
	}
	if endDate != "" {
		if t, err := time.Parse("2006-01-02", endDate); err == nil {
			query = query.Where("created_at <= ?", t.Add(24*time.Hour))
		}
	}

	// Get total stats
	var totalInput, totalOutput int64
	var totalCost float64
	query.Select("COALESCE(SUM(input_tokens), 0)").Scan(&totalInput)
	query.Select("COALESCE(SUM(output_tokens), 0)").Scan(&totalOutput)
	query.Select("COALESCE(SUM(cost_usd), 0)").Scan(&totalCost)

	// Get usage by model
	type ModelUsage struct {
		ModelName    string
		InputTokens  int64
		OutputTokens int64
		CostUSD      float64
		RequestCount int64
	}
	var modelUsage []ModelUsage
	query.Select("model_name, SUM(input_tokens) as input_tokens, SUM(output_tokens) as output_tokens, SUM(cost_usd) as cost_usd, COUNT(*) as request_count").
		Group("model_name").
		Scan(&modelUsage)

	// Get daily usage
	type DailyUsage struct {
		Date         string
		InputTokens  int64
		OutputTokens int64
		CostUSD      float64
		RequestCount int64
	}
	var dailyUsage []DailyUsage
	query.Select("DATE(created_at) as date, SUM(input_tokens) as input_tokens, SUM(output_tokens) as output_tokens, SUM(cost_usd) as cost_usd, COUNT(*) as request_count").
		Group("DATE(created_at)").
		Order("date desc").
		Limit(30).
		Scan(&dailyUsage)

	c.JSON(http.StatusOK, gin.H{
		"totals": gin.H{
			"input_tokens":  totalInput,
			"output_tokens": totalOutput,
			"cost_usd":      totalCost,
		},
		"by_model": modelUsage,
		"daily":    dailyUsage,
	})
}

// GetUserBilling gets billing information
func GetUserBilling(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)

	// Get current month's usage
	now := time.Now()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	var monthlyCost float64
	models.DB.Model(&models.UsageLog{}).
		Where("user_id = ? AND created_at >= ?", user.ID, startOfMonth).
		Select("COALESCE(SUM(cost_usd), 0)").
		Scan(&monthlyCost)

	// Get all-time stats
	var totalCost float64
	var totalRequests int64
	models.DB.Model(&models.UsageLog{}).
		Where("user_id = ?", user.ID).
		Select("COALESCE(SUM(cost_usd), 0)").
		Scan(&totalCost)
	models.DB.Model(&models.UsageLog{}).
		Where("user_id = ?", user.ID).
		Count(&totalRequests)

	c.JSON(http.StatusOK, gin.H{
		"current_month": gin.H{
			"cost":   monthlyCost,
			"period": now.Format("January 2006"),
		},
		"all_time": gin.H{
			"cost":     totalCost,
			"requests": totalRequests,
		},
	})
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