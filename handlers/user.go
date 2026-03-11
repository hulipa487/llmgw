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

// CreateAPIKeyRequest represents API key creation request
type CreateAPIKeyRequest struct {
	Name string `json:"name" binding:"max=100"`
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
	mtfUser, _ := middleware.GetMTFPassUser(c)

	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	// Get request count this month
	var monthRequests int64
	models.DB.Model(&models.UsageLog{}).
		Where("user_id = ? AND created_at >= ?", user.ID, monthStart).
		Count(&monthRequests)

	// Get tokens by model this month
	type ModelTokens struct {
		ModelName    string `json:"model_name"`
		InputTokens  int64  `json:"input_tokens"`
		OutputTokens int64  `json:"output_tokens"`
	}
	var modelTokens []ModelTokens
	models.DB.Table("usage_logs").
		Select("model_name, COALESCE(SUM(input_tokens), 0) as input_tokens, COALESCE(SUM(output_tokens), 0) as output_tokens").
		Where("user_id = ? AND created_at >= ?", user.ID, monthStart).
		Group("model_name").
		Scan(&modelTokens)

	c.JSON(http.StatusOK, gin.H{
		"credits": gin.H{
			"remaining": mtfUser.Credits,
			"unlimited": mtfUser.HasUnlimitedCredits(),
		},
		"usage": gin.H{
			"requests_this_month": monthRequests,
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

// GetCurrentUser returns the current logged-in user info
func GetCurrentUser(c *gin.Context) {
	user, exists := middleware.GetCurrentUser(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	mtfUser, _ := middleware.GetMTFPassUser(c)

	c.JSON(http.StatusOK, gin.H{
		"id":       user.ID,
		"username": user.Username,
		"role":     user.Role,
		"credits":  mtfUser.Credits,
	})
}

// Logout calls MTFPass logout API and clears the auth cookie
func Logout(c *gin.Context) {
	// Get the current token to logout from MTFPass
	jwtToken, err := c.Cookie("mtf_auth")
	if err == nil && jwtToken != "" {
		// Call MTFPass logout API (best effort)
		middleware.MTFPassClient.Logout(jwtToken)
	}

	// Clear the mtf_auth cookie with correct domain
	c.SetCookie("mtf_auth", "", -1, "/", "mtf.edu.ci", true, true)

	// Redirect to MTFPass login with origin
	origin := "https://" + c.Request.Host
	c.Redirect(http.StatusFound, middleware.MTFPassClient.BaseURL+"/auth/login?origin="+origin)
}

// CheckAuth checks if user is authenticated and returns user info
// This endpoint is used by nginx auth_request
func CheckAuth(c *gin.Context) {
	jwtToken, err := c.Cookie("mtf_auth")
	if err != nil || jwtToken == "" {
		// Clear any existing cookie and return 401
		c.SetCookie("mtf_auth", "", -1, "/", "mtf.edu.ci", true, true)
		c.Status(http.StatusUnauthorized)
		return
	}

	// Validate with MTFPass
	mtfUser, err := middleware.MTFPassClient.ValidateToken(jwtToken)
	if err != nil {
		// Clear invalid cookie and return 401
		c.SetCookie("mtf_auth", "", -1, "/", "mtf.edu.ci", true, true)
		c.Status(http.StatusUnauthorized)
		return
	}

	// Set headers for nginx to use with auth_request_set
	c.Header("X-Auth-User-Id", fmt.Sprintf("%d", mtfUser.UID))
	c.Header("X-Auth-Role", mtfUser.Role)
	c.Status(http.StatusOK)
}
