package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"llmgw/models"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type contextKey string

const UserKey contextKey = "user"
const APIKeyKey contextKey = "apiKey"

func hashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

// RequireAPIKey validates the API key from Authorization header or x-api-key header
func RequireAPIKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		var key string

		// Try Authorization header first (Bearer token)
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			if strings.HasPrefix(authHeader, "Bearer ") {
				key = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		// Try x-api-key header (Anthropic style)
		if key == "" {
			key = c.GetHeader("x-api-key")
		}

		if key == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "API key required"})
			c.Abort()
			return
		}

		keyHash := hashKey(key)

		var apiKey models.APIKey
		if err := models.DB.Where("key_hash = ? AND is_active = ?", keyHash, true).
			Preload("User").First(&apiKey).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
			c.Abort()
			return
		}

		// Update last used
		models.DB.Model(&apiKey).Update("last_used_at", models.DB.NowFunc())

		c.Set("user", apiKey.User)
		c.Set("apiKey", apiKey)
		c.Next()
	}
}

// RequireAuth validates session-based authentication for web panels
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("userID")
		if !exists {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		var user models.User
		if err := models.DB.First(&user, userID).Error; err != nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		c.Set("user", user)
		c.Next()
	}
}

// RequireAdminAuth validates admin session
func RequireAdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		adminID, exists := c.Get("adminID")
		if !exists {
			c.Redirect(http.StatusFound, "/admin/login")
			c.Abort()
			return
		}

		var admin models.Admin
		if err := models.DB.First(&admin, adminID).Error; err != nil {
			c.Redirect(http.StatusFound, "/admin/login")
			c.Abort()
			return
		}

		c.Set("admin", admin)
		c.Next()
	}
}

// GetCurrentUser returns the current user from context
func GetCurrentUser(c *gin.Context) (*models.User, bool) {
	user, exists := c.Get("user")
	if !exists {
		return nil, false
	}
	u, ok := user.(models.User)
	if !ok {
		return nil, false
	}
	return &u, true
}

// GetCurrentAdmin returns the current admin from context
func GetCurrentAdmin(c *gin.Context) (*models.Admin, bool) {
	admin, exists := c.Get("admin")
	if !exists {
		return nil, false
	}
	a, ok := admin.(models.Admin)
	if !ok {
		return nil, false
	}
	return &a, true
}

// GetCurrentAPIKey returns the current API key from context
func GetCurrentAPIKey(c *gin.Context) (*models.APIKey, bool) {
	apiKey, exists := c.Get("apiKey")
	if !exists {
		return nil, false
	}
	k, ok := apiKey.(models.APIKey)
	if !ok {
		return nil, false
	}
	return &k, true
}