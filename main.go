package main

import (
	"flag"
	"fmt"
	"llmgw/config"
	"llmgw/handlers"
	"llmgw/middleware"
	"llmgw/models"
	"log"

	"github.com/gin-gonic/gin"
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

	// Check for required MTFPass URL
	if cfg.MTFPassURL == "" {
		log.Fatal("mtfpass_url config required in config file")
	}

	// Initialize database
	if err := models.InitDatabase(cfg.DB); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Initialize MTFPass client
	middleware.InitMTFPassClient(cfg.MTFPassURL)

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

	// Public routes
	r.GET("/api/auth/check", handlers.CheckAuth)
	r.GET("/api/auth/logout", handlers.Logout)

	// Protected API routes - User
	apiUser := r.Group("/api/user")
	apiUser.Use(middleware.RequireAuth())
	{
		apiUser.GET("/me", handlers.GetCurrentUser)
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
	}

	// OpenAI-compatible API routes
	openaiAPI := r.Group("/openai")
	openaiAPI.Use(middleware.RequireAPIKey())
	{
		openaiAPI.POST("/chat/completions", handlers.OpenAIChatCompletions)
		openaiAPI.GET("/models", handlers.OpenAIListModels)
		openaiAPI.GET("/models/:model", handlers.OpenAIGetModel)
	}

	// OpenAI top-level models endpoint (for compatibility)
	r.GET("/models", middleware.RequireAPIKey(), handlers.OpenAIListModels)

	// OpenAI to Anthropic converter routes
	// Accepts Anthropic format, converts to OpenAI format for upstream
	anthropicAPI := r.Group("/anthropic")
	anthropicAPI.Use(middleware.RequireAPIKey())
	{
		anthropicAPI.POST("/messages", handlers.AnthropicToOpenAI)
		anthropicAPI.GET("/models", handlers.AnthropicListModels)
	}

	// Anthropic top-level models endpoint (for compatibility)
	r.GET("/anthropic/models", middleware.RequireAPIKey(), handlers.AnthropicListModels)

	// Start server
	host := cfg.Host
	port := cfg.Port

	addr := fmt.Sprintf("%s:%d", host, port)
	log.Printf("Starting server on %s", addr)
	log.Printf("MTFPass URL: %s", cfg.MTFPassURL)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
