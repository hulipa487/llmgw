package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"llmgw/config"
	"llmgw/handlers"
	"llmgw/middleware"
	"llmgw/models"
	"log"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	// Parse command line flags
	configPath := flag.String("c", "/etc/llmgw.conf", "path to config file")
	flag.Parse()

	// Disable Gin debug mode
	gin.SetMode(gin.ReleaseMode)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config from %s: %v", *configPath, err)
	}

	// Check for required DB config
	if cfg.DB == "" {
		log.Fatal("db config required in config file")
	}

	// Initialize database
	if err := models.InitDatabase(cfg.DB); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Create default admin if not exists
	var adminCount int64
	models.DB.Model(&models.Admin{}).Count(&adminCount)
	if adminCount == 0 {
		password, err := generateRandomPassword()
		if err != nil {
			log.Fatalf("Failed to generate admin password: %v", err)
		}
		hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		admin := models.Admin{
			Username:     "admin",
			PasswordHash: string(hashedPassword),
		}
		if err := models.DB.Create(&admin).Error; err != nil {
			log.Fatalf("Failed to create admin: %v", err)
		}
		fmt.Printf("\n========================================\n")
		fmt.Printf("  ADMIN CREDENTIALS (SAVE THIS!)\n")
		fmt.Printf("  Username: admin\n")
		fmt.Printf("  Password: %s\n", password)
		fmt.Printf("========================================\n\n")
	}

	// Setup Gin
	r := gin.New()
	r.Use(gin.Recovery())

	// Custom logger that only logs errors
	r.Use(func(c *gin.Context) {
		c.Next()
		if len(c.Errors) > 0 {
			log.Printf("Error: %v", c.Errors)
		}
	})

	// Session middleware
	r.Use(handlers.SessionMiddleware())

	// API routes - User Auth (public)
	apiUserAuth := r.Group("/api/user")
	{
		apiUserAuth.POST("/login", handlers.UserLogin)
		apiUserAuth.POST("/register", handlers.UserRegister)
		apiUserAuth.GET("/logout", handlers.UserLogout)
	}

	// API routes - Admin Auth (public)
	apiAdminAuth := r.Group("/api/admin")
	{
		apiAdminAuth.POST("/login", handlers.AdminLogin)
		apiAdminAuth.GET("/logout", handlers.AdminLogout)
	}

	// Protected API routes - User
	apiUser := r.Group("/api/user")
	apiUser.Use(middleware.RequireAuth())
	{
		apiUser.GET("/keys", handlers.ListAPIKeys)
		apiUser.POST("/keys", handlers.CreateAPIKey)
		apiUser.DELETE("/keys/:id", handlers.DeleteAPIKey)
		apiUser.GET("/usage", handlers.GetUserUsage)
		apiUser.GET("/models", handlers.GetEnabledModels)
	}

	// Protected API routes - Admin
	apiAdmin := r.Group("/api/admin")
	apiAdmin.Use(middleware.RequireAdminAuth())
	{
		apiAdmin.GET("/models", handlers.ListModels)
		apiAdmin.POST("/models", handlers.CreateModel)
		apiAdmin.PUT("/models/:id", handlers.UpdateModel)
		apiAdmin.DELETE("/models/:id", handlers.DeleteModel)
		apiAdmin.GET("/upstreams", handlers.ListUpstreams)
		apiAdmin.POST("/upstreams", handlers.CreateUpstream)
		apiAdmin.PUT("/upstreams/:id", handlers.UpdateUpstream)
		apiAdmin.DELETE("/upstreams/:id", handlers.DeleteUpstream)
		apiAdmin.GET("/users", handlers.ListUsers)
		apiAdmin.GET("/usage", handlers.GetAdminUsage)
		apiAdmin.GET("/invites", handlers.ListInviteCodes)
		apiAdmin.POST("/invites", handlers.CreateInviteCode)
		apiAdmin.POST("/invites/batch", handlers.CreateMultipleInviteCodes)
		apiAdmin.DELETE("/invites/:id", handlers.DeleteInviteCode)
	}

	// OpenAI-compatible API routes (under /openai)
	openaiAPI := r.Group("/openai")
	openaiAPI.Use(middleware.RequireAPIKey())
	{
		openaiAPI.POST("/chat/completions", handlers.OpenAIChatCompletions)
		openaiAPI.GET("/models", handlers.OpenAIListModels)
		openaiAPI.GET("/models/:model", handlers.OpenAIGetModel)
	}

	// Anthropic-compatible API routes (under /anthropic/v1)
	anthropicAPI := r.Group("/anthropic/v1")
	anthropicAPI.Use(middleware.RequireAPIKey())
	{
		anthropicAPI.POST("/messages", handlers.AnthropicMessages)
		anthropicAPI.GET("/models", handlers.AnthropicListModels)
	}

	// Start server
	host := cfg.Host
	port := cfg.Port

	addr := fmt.Sprintf("%s:%d", host, port)
	log.Printf("Starting server on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func generateRandomPassword() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}