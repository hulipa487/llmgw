package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"llmgw/config"
	"llmgw/handlers"
	"llmgw/middleware"
	"llmgw/models"
	"log"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	// Disable Gin debug mode
	gin.SetMode(gin.ReleaseMode)

	// Load configuration
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Printf("Note: config.json not found, using defaults")
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

	// Load templates
	tmpl := template.Must(template.ParseGlob("templates/*.html"))
	r.SetHTMLTemplate(tmpl)

	// Serve static files
	r.Static("/static", "./static")

	// Session middleware
	r.Use(handlers.SessionMiddleware())

	// Web routes - Auth
	r.GET("/login", func(c *gin.Context) {
		c.HTML(200, "login.html", nil)
	})
	r.GET("/register", func(c *gin.Context) {
		c.HTML(200, "register.html", nil)
	})

	// API routes - User Auth
	apiAuth := r.Group("/api/user")
	{
		apiAuth.POST("/login", handlers.UserLogin)
		apiAuth.POST("/register", handlers.UserRegister)
		apiAuth.GET("/logout", handlers.UserLogout)
		apiAuth.POST("/logout", handlers.UserLogout)
	}

	// API routes - Admin Auth
	apiAdminAuth := r.Group("/api/admin")
	{
		apiAdminAuth.POST("/login", handlers.AdminLogin)
		apiAdminAuth.GET("/logout", handlers.AdminLogout)
		apiAdminAuth.POST("/logout", handlers.AdminLogout)
	}

	// Protected web routes - User
	userRoutes := r.Group("/user")
	userRoutes.Use(middleware.RequireAuth())
	{
		userRoutes.GET("/dashboard", func(c *gin.Context) {
			user, _ := middleware.GetCurrentUser(c)
			c.HTML(200, "user_dashboard.html", gin.H{"User": user})
		})
		userRoutes.GET("/keys", func(c *gin.Context) {
			user, _ := middleware.GetCurrentUser(c)
			c.HTML(200, "user_keys.html", gin.H{"User": user})
		})
		userRoutes.GET("/billing", func(c *gin.Context) {
			user, _ := middleware.GetCurrentUser(c)
			c.HTML(200, "user_billing.html", gin.H{"User": user})
		})
	}

	// Protected API routes - User
	apiUser := r.Group("/api/user")
	apiUser.Use(middleware.RequireAuth())
	{
		apiUser.GET("/keys", handlers.ListAPIKeys)
		apiUser.POST("/keys", handlers.CreateAPIKey)
		apiUser.DELETE("/keys/:id", handlers.DeleteAPIKey)
		apiUser.GET("/usage", handlers.GetUserUsage)
		apiUser.GET("/billing", handlers.GetUserBilling)
	}

	// Admin login page
	r.GET("/admin/login", func(c *gin.Context) {
		c.HTML(200, "admin_login.html", nil)
	})

	// Admin web routes
	adminRoutes := r.Group("/admin")
	adminRoutes.Use(middleware.RequireAdminAuth())
	{
		adminRoutes.GET("/dashboard", func(c *gin.Context) {
			admin, _ := middleware.GetCurrentAdmin(c)
			c.HTML(200, "admin_dashboard.html", gin.H{"Admin": admin})
		})
		adminRoutes.GET("/models", func(c *gin.Context) {
			admin, _ := middleware.GetCurrentAdmin(c)
			c.HTML(200, "admin_models.html", gin.H{"Admin": admin})
		})
		adminRoutes.GET("/upstreams", func(c *gin.Context) {
			admin, _ := middleware.GetCurrentAdmin(c)
			c.HTML(200, "admin_upstreams.html", gin.H{"Admin": admin})
		})
		adminRoutes.GET("/users", func(c *gin.Context) {
			admin, _ := middleware.GetCurrentAdmin(c)
			c.HTML(200, "admin_users.html", gin.H{"Admin": admin})
		})
	}

	// Admin API routes
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

	// OpenAI-compatible API routes (under /openai)
	openaiAPI := r.Group("/openai")
	openaiAPI.Use(middleware.RequireAPIKey())
	{
		openaiAPI.POST("/chat/completions", handlers.OpenAIChatCompletions)
		openaiAPI.GET("/models", handlers.OpenAIListModels)
		openaiAPI.GET("/models/:model", handlers.OpenAIGetModel)
	}

	// Anthropic-compatible API routes (under /anthropic)
	anthropicAPI := r.Group("/anthropic/v1")
	anthropicAPI.Use(middleware.RequireAPIKey())
	{
		anthropicAPI.POST("/messages", handlers.AnthropicMessages)
		anthropicAPI.GET("/models", handlers.AnthropicListModels)
	}

	// Home route
	r.GET("/", func(c *gin.Context) {
		_, loggedIn := c.Get("userID")
		if loggedIn {
			c.Redirect(302, "/user/dashboard")
			return
		}
		_, adminLoggedIn := c.Get("adminID")
		if adminLoggedIn {
			c.Redirect(302, "/admin/dashboard")
			return
		}
		c.Redirect(302, "/login")
	})

	// Start server
	host := cfg.Host
	port := cfg.Port

	addr := fmt.Sprintf("%s:%d", host, port)
	log.Printf("Starting server on %s", addr)
	log.Printf("Database: %s", cfg.DB)
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