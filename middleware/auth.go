package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"llmgw/auth"
	"llmgw/models"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type contextKey string

const UserKey contextKey = "user"
const APIKeyKey contextKey = "apiKey"
const MTFPassUserKey contextKey = "mtfpass_user"

// MTFPassClient is the global MTFPass client
var MTFPassClient *auth.MTFPassClient

// InitMTFPassClient initializes the MTFPass client
func InitMTFPassClient(baseURL string) {
	MTFPassClient = auth.NewMTFPassClient(baseURL)
}

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

// RequireAuth validates MTFPass JWT cookie and loads user
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get mtf_auth cookie
		jwtToken, err := c.Cookie("mtf_auth")
		if err != nil || jwtToken == "" {
			// Clear any invalid cookie and redirect to MTFPass login
			c.SetCookie("mtf_auth", "", -1, "/", "mtf.edu.ci", true, true)
			origin := "https://" + c.Request.Host
			c.Redirect(http.StatusFound, MTFPassClient.BaseURL+"/auth/login?origin="+origin)
			c.Abort()
			return
		}

		// Validate with MTFPass
		mtfUser, err := MTFPassClient.ValidateToken(jwtToken)
		if err != nil {
			// Clear invalid cookie and redirect to MTFPass login
			c.SetCookie("mtf_auth", "", -1, "/", "mtf.edu.ci", true, true)
			origin := "https://" + c.Request.Host
			c.Redirect(http.StatusFound, MTFPassClient.BaseURL+"/auth/login?origin="+origin)
			c.Abort()
			return
		}

		// Get or create local user
		var user models.User
		result := models.DB.FirstOrCreate(&user, models.User{ID: mtfUser.UID})
		if result.Error != nil {
			c.SetCookie("mtf_auth", "", -1, "/", "mtf.edu.ci", true, true)
			origin := "https://" + c.Request.Host
			c.Redirect(http.StatusFound, MTFPassClient.BaseURL+"/auth/login?origin="+origin)
			c.Abort()
			return
		}

		// Update user info if new or changed
		if user.Username != mtfUser.Username || user.Role != mtfUser.Role {
			user.Username = mtfUser.Username
			user.Role = mtfUser.Role
			models.DB.Save(&user)
		}

		// Store both local user and MTFPass user in context
		c.Set("user", user)
		c.Set("mtfpass_user", mtfUser)
		c.Next()
	}
}

// RequireAdminAuth validates MTFPass JWT cookie and checks for admin role
func RequireAdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get mtf_auth cookie
		jwtToken, err := c.Cookie("mtf_auth")
		if err != nil || jwtToken == "" {
			// Clear any invalid cookie and redirect to MTFPass login
			c.SetCookie("mtf_auth", "", -1, "/", "mtf.edu.ci", true, true)
			origin := "https://" + c.Request.Host
			c.Redirect(http.StatusFound, MTFPassClient.BaseURL+"/auth/login?origin="+origin)
			c.Abort()
			return
		}

		// Validate with MTFPass
		mtfUser, err := MTFPassClient.ValidateToken(jwtToken)
		if err != nil {
			// Clear invalid cookie and redirect to MTFPass login
			c.SetCookie("mtf_auth", "", -1, "/", "mtf.edu.ci", true, true)
			origin := "https://" + c.Request.Host
			c.Redirect(http.StatusFound, MTFPassClient.BaseURL+"/auth/login?origin="+origin)
			c.Abort()
			return
		}

		// Check admin role
		if !mtfUser.IsAdmin() {
			c.JSON(http.StatusForbidden, gin.H{"error": "Admin access required"})
			c.Abort()
			return
		}

		// Get or create local user
		var user models.User
		result := models.DB.FirstOrCreate(&user, models.User{ID: mtfUser.UID})
		if result.Error != nil {
			c.SetCookie("mtf_auth", "", -1, "/", "mtf.edu.ci", true, true)
			origin := "https://" + c.Request.Host
			c.Redirect(http.StatusFound, MTFPassClient.BaseURL+"/auth/login?origin="+origin)
			c.Abort()
			return
		}

		// Update user info if changed
		if user.Username != mtfUser.Username || user.Role != mtfUser.Role {
			user.Username = mtfUser.Username
			user.Role = mtfUser.Role
			models.DB.Save(&user)
		}

		c.Set("user", user)
		c.Set("mtfpass_user", mtfUser)
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

// GetMTFPassUser returns the MTFPass user info from context
func GetMTFPassUser(c *gin.Context) (*auth.MTFPassUser, bool) {
	mtfUser, exists := c.Get("mtfpass_user")
	if !exists {
		return nil, false
	}
	u, ok := mtfUser.(*auth.MTFPassUser)
	if !ok {
		return nil, false
	}
	return u, true
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
