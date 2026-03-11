package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"llmgw/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ModelUpstreamResponse represents upstream association in API response
type ModelUpstreamResponse struct {
	ID               uint   `json:"id"`
	Name             string `json:"name"`
	UpstreamID       string `json:"upstream_id"`
	UpstreamModelName string `json:"upstream_model_name"`
}

// ModelResponse represents model in API response
type ModelResponse struct {
	ID              uint                  `json:"id"`
	Name            string                `json:"name"`
	PriceInputPerM  float64               `json:"price_input_per_m"`
	PriceOutputPerM float64               `json:"price_output_per_m"`
	IsEnabled       bool                  `json:"is_enabled"`
	CreatedAt       string                `json:"created_at"`
	Upstreams       []ModelUpstreamResponse `json:"upstreams"`
}

// ListModels lists all models (admin)
func ListModels(c *gin.Context) {
	var modelList []models.Model
	if err := models.DB.Order("name").Find(&modelList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch models"})
		return
	}

	var result []ModelResponse
	for _, m := range modelList {
		// Get upstream associations with junction table data
		var upstreamAssocs []models.ModelUpstream
		models.DB.Where("model_id = ?", m.ID).Find(&upstreamAssocs)

		var upstreams []ModelUpstreamResponse
		for _, ua := range upstreamAssocs {
			var upstream models.UpstreamConfig
			if err := models.DB.First(&upstream, ua.UpstreamConfigID).Error; err == nil {
				upstreams = append(upstreams, ModelUpstreamResponse{
					ID:                upstream.ID,
					Name:              upstream.Name,
					UpstreamID:        upstream.UpstreamID,
					UpstreamModelName: ua.UpstreamModelName,
				})
			}
		}

		result = append(result, ModelResponse{
			ID:              m.ID,
			Name:            m.Name,
			PriceInputPerM:  m.PriceInputPerM,
			PriceOutputPerM: m.PriceOutputPerM,
			IsEnabled:       m.IsEnabled,
			CreatedAt:       m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			Upstreams:       upstreams,
		})
	}

	c.JSON(http.StatusOK, result)
}

// CreateModelRequest represents model creation request
type CreateModelRequest struct {
	Name            string               `json:"name" binding:"required"`
	PriceInputPerM  float64              `json:"price_input_per_m"`
	PriceOutputPerM float64              `json:"price_output_per_m"`
	IsEnabled       bool                 `json:"is_enabled"`
	Upstreams       []ModelUpstreamInput `json:"upstreams"`
}

// ModelUpstreamInput represents upstream association for model
type ModelUpstreamInput struct {
	UpstreamID        uint   `json:"upstream_id"`
	UpstreamModelName string `json:"upstream_model_name" binding:"required"`
}

// CreateModel creates a new model
func CreateModel(c *gin.Context) {
	var req CreateModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Start transaction
	tx := models.DB.Begin()

	model := models.Model{
		Name:            req.Name,
		PriceInputPerM:  req.PriceInputPerM,
		PriceOutputPerM: req.PriceOutputPerM,
		IsEnabled:       req.IsEnabled,
	}

	if err := tx.Create(&model).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create model"})
		return
	}

	// Add upstream associations
	for _, us := range req.Upstreams {
		modelUpstream := models.ModelUpstream{
			ModelID:           model.ID,
			UpstreamConfigID:  us.UpstreamID,
			UpstreamModelName: us.UpstreamModelName,
		}
		if err := tx.Create(&modelUpstream).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create model-upstream association"})
			return
		}
	}

	tx.Commit()

	// Return the created model with upstreams
	var upstreamAssocs []models.ModelUpstream
	models.DB.Where("model_id = ?", model.ID).Find(&upstreamAssocs)

	var upstreams []ModelUpstreamResponse
	for _, ua := range upstreamAssocs {
		var upstream models.UpstreamConfig
		if err := models.DB.First(&upstream, ua.UpstreamConfigID).Error; err == nil {
			upstreams = append(upstreams, ModelUpstreamResponse{
				ID:                upstream.ID,
				Name:              upstream.Name,
				UpstreamID:        upstream.UpstreamID,
				UpstreamModelName: ua.UpstreamModelName,
			})
		}
	}

	c.JSON(http.StatusCreated, ModelResponse{
		ID:              model.ID,
		Name:            model.Name,
		PriceInputPerM:  model.PriceInputPerM,
		PriceOutputPerM: model.PriceOutputPerM,
		IsEnabled:       model.IsEnabled,
		CreatedAt:       model.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Upstreams:       upstreams,
	})
}

// UpdateModelRequest represents model update request
type UpdateModelRequest struct {
	Name            *string               `json:"name"`
	PriceInputPerM  *float64              `json:"price_input_per_m"`
	PriceOutputPerM *float64              `json:"price_output_per_m"`
	IsEnabled       *bool                 `json:"is_enabled"`
	Upstreams       *[]ModelUpstreamInput `json:"upstreams"`
}

// UpdateModel updates a model
func UpdateModel(c *gin.Context) {
	modelID := c.Param("id")

	var model models.Model
	if err := models.DB.First(&model, modelID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Model not found"})
		return
	}

	var req UpdateModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tx := models.DB.Begin()

	updates := make(map[string]interface{})
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.PriceInputPerM != nil {
		updates["price_input_per_m"] = *req.PriceInputPerM
	}
	if req.PriceOutputPerM != nil {
		updates["price_output_per_m"] = *req.PriceOutputPerM
	}
	if req.IsEnabled != nil {
		updates["is_enabled"] = *req.IsEnabled
	}

	if len(updates) > 0 {
		if err := tx.Model(&model).Updates(updates).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update model"})
			return
		}
	}

	// Update upstream associations if provided
	if req.Upstreams != nil {
		// Delete existing associations
		if err := tx.Where("model_id = ?", model.ID).Delete(&models.ModelUpstream{}).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update upstream associations"})
			return
		}

		// Add new associations
		for _, us := range *req.Upstreams {
			modelUpstream := models.ModelUpstream{
				ModelID:           model.ID,
				UpstreamConfigID:  us.UpstreamID,
				UpstreamModelName: us.UpstreamModelName,
			}
			if err := tx.Create(&modelUpstream).Error; err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create model-upstream association"})
				return
			}
		}
	}

	tx.Commit()

	// Return updated model with upstreams
	var upstreamAssocs []models.ModelUpstream
	models.DB.Where("model_id = ?", model.ID).Find(&upstreamAssocs)

	var upstreams []ModelUpstreamResponse
	for _, ua := range upstreamAssocs {
		var upstream models.UpstreamConfig
		if err := models.DB.First(&upstream, ua.UpstreamConfigID).Error; err == nil {
			upstreams = append(upstreams, ModelUpstreamResponse{
				ID:                upstream.ID,
				Name:              upstream.Name,
				UpstreamID:        upstream.UpstreamID,
				UpstreamModelName: ua.UpstreamModelName,
			})
		}
	}

	c.JSON(http.StatusOK, ModelResponse{
		ID:              model.ID,
		Name:            model.Name,
		PriceInputPerM:  model.PriceInputPerM,
		PriceOutputPerM: model.PriceOutputPerM,
		IsEnabled:       model.IsEnabled,
		CreatedAt:       model.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Upstreams:       upstreams,
	})
}

// DeleteModel deletes a model
func DeleteModel(c *gin.Context) {
	modelID := c.Param("id")

	tx := models.DB.Begin()

	// Delete upstream associations first
	if err := tx.Where("model_id = ?", modelID).Delete(&models.ModelUpstream{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete model associations"})
		return
	}

	result := tx.Delete(&models.Model{}, modelID)
	if result.Error != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete model"})
		return
	}

	if result.RowsAffected == 0 {
		tx.Rollback()
		c.JSON(http.StatusNotFound, gin.H{"error": "Model not found"})
		return
	}

	tx.Commit()
	c.JSON(http.StatusOK, gin.H{"message": "Model deleted"})
}

// UpstreamResponse represents upstream in API response (without key)
type UpstreamResponse struct {
	ID         uint   `json:"id"`
	UpstreamID string `json:"upstream_id"`
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	APIPath    string `json:"api_path"`
	HasKey     bool   `json:"has_key"`
}

// ListUpstreams lists all upstreams
func ListUpstreams(c *gin.Context) {
	var upstreams []models.UpstreamConfig
	if err := models.DB.Order("name").Find(&upstreams).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch upstreams"})
		return
	}

	var result []UpstreamResponse
	for _, u := range upstreams {
		result = append(result, UpstreamResponse{
			ID:         u.ID,
			UpstreamID: u.UpstreamID,
			Name:       u.Name,
			BaseURL:    u.BaseURL,
			APIPath:    u.APIPath,
			HasKey:     u.Key != "",
		})
	}

	c.JSON(http.StatusOK, result)
}

// CreateUpstreamRequest represents upstream creation request
type CreateUpstreamRequest struct {
	UpstreamID string `json:"upstream_id" binding:"required"`
	Name       string `json:"name" binding:"required"`
	BaseURL    string `json:"base_url" binding:"required"`
	APIPath    string `json:"api_path"`
	Key        string `json:"key" binding:"required"`
}

// CreateUpstream creates a new upstream
func CreateUpstream(c *gin.Context) {
	var req CreateUpstreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Set default API path
	if req.APIPath == "" {
		req.APIPath = "/v1"
	}

	// Check if upstream_id already exists
	var existing models.UpstreamConfig
	if models.DB.Where("upstream_id = ?", req.UpstreamID).First(&existing).Error == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Upstream ID already exists"})
		return
	}

	upstream := models.UpstreamConfig{
		UpstreamID: req.UpstreamID,
		Name:       req.Name,
		BaseURL:    req.BaseURL,
		APIPath:    req.APIPath,
		Key:        req.Key,
	}

	if err := models.DB.Create(&upstream).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create upstream"})
		return
	}

	c.JSON(http.StatusCreated, upstream)
}

// UpdateUpstreamRequest represents upstream update request
type UpdateUpstreamRequest struct {
	UpstreamID *string `json:"upstream_id"`
	Name       *string `json:"name"`
	BaseURL    *string `json:"base_url"`
	APIPath    *string `json:"api_path"`
	Key        *string `json:"key"`
}

// UpdateUpstream updates an upstream
func UpdateUpstream(c *gin.Context) {
	upstreamID := c.Param("id")

	var upstream models.UpstreamConfig
	if err := models.DB.First(&upstream, upstreamID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Upstream not found"})
		return
	}

	var req UpdateUpstreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := make(map[string]interface{})
	if req.UpstreamID != nil {
		updates["upstream_id"] = *req.UpstreamID
	}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.BaseURL != nil {
		updates["base_url"] = *req.BaseURL
	}
	if req.APIPath != nil {
		updates["api_path"] = *req.APIPath
	}
	if req.Key != nil {
		updates["key"] = *req.Key
	}

	if len(updates) > 0 {
		if err := models.DB.Model(&upstream).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update upstream"})
			return
		}
	}

	// Reload
	models.DB.First(&upstream, upstreamID)
	c.JSON(http.StatusOK, upstream)
}

// DeleteUpstream deletes an upstream
func DeleteUpstream(c *gin.Context) {
	upstreamID := c.Param("id")

	tx := models.DB.Begin()

	// Delete model associations first
	if err := tx.Where("upstream_config_id = ?", upstreamID).Delete(&models.ModelUpstream{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete upstream associations"})
		return
	}

	result := tx.Delete(&models.UpstreamConfig{}, upstreamID)
	if result.Error != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete upstream"})
		return
	}

	if result.RowsAffected == 0 {
		tx.Rollback()
		c.JSON(http.StatusNotFound, gin.H{"error": "Upstream not found"})
		return
	}

	tx.Commit()
	c.JSON(http.StatusOK, gin.H{"message": "Upstream deleted"})
}

// ListUsers lists all users (admin)
func ListUsers(c *gin.Context) {
	var users []models.User
	if err := models.DB.Order("created_at desc").Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch users"})
		return
	}

	var result []gin.H
	for _, u := range users {
		// Count API keys
		var keyCount int64
		models.DB.Model(&models.APIKey{}).Where("user_id = ?", u.ID).Count(&keyCount)

		result = append(result, gin.H{
			"id":         u.ID,
			"username":   u.Username,
			"role":       u.Role,
			"created_at": u.CreatedAt,
			"key_count":  keyCount,
		})
	}

	c.JSON(http.StatusOK, result)
}

// GetAdminUsage gets global usage statistics (admin)
func GetAdminUsage(c *gin.Context) {
	// Get date range from query
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	// Build base query
	baseQuery := models.DB.Model(&models.UsageLog{})
	if startDate != "" {
		baseQuery = baseQuery.Where("created_at >= ?", startDate)
	}
	if endDate != "" {
		baseQuery = baseQuery.Where("created_at <= ?", endDate+" 23:59:59")
	}

	// Get total stats
	var totalInput, totalOutput int64
	var totalCost float64
	var totalRequests int64

	inputQuery := models.DB.Model(&models.UsageLog{})
	outputQuery := models.DB.Model(&models.UsageLog{})
	costQuery := models.DB.Model(&models.UsageLog{})
	countQuery := models.DB.Model(&models.UsageLog{})

	if startDate != "" {
		inputQuery = inputQuery.Where("created_at >= ?", startDate)
		outputQuery = outputQuery.Where("created_at >= ?", startDate)
		costQuery = costQuery.Where("created_at >= ?", startDate)
		countQuery = countQuery.Where("created_at >= ?", startDate)
	}
	if endDate != "" {
		inputQuery = inputQuery.Where("created_at <= ?", endDate+" 23:59:59")
		outputQuery = outputQuery.Where("created_at <= ?", endDate+" 23:59:59")
		costQuery = costQuery.Where("created_at <= ?", endDate+" 23:59:59")
		countQuery = countQuery.Where("created_at <= ?", endDate+" 23:59:59")
	}

	inputQuery.Select("COALESCE(SUM(input_tokens), 0)").Scan(&totalInput)
	outputQuery.Select("COALESCE(SUM(output_tokens), 0)").Scan(&totalOutput)
	costQuery.Select("COALESCE(SUM(cost_usd), 0)").Scan(&totalCost)
	countQuery.Count(&totalRequests)

	// Get usage by user
	type UserUsage struct {
		UserID       int64   `json:"user_id"`
		Username     string  `json:"username"`
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
		CostUSD      float64 `json:"cost_usd"`
		RequestCount int64   `json:"request_count"`
	}
	var userUsage []UserUsage

	userQuery := models.DB.Table("usage_logs").
		Select("usage_logs.user_id, users.username, COALESCE(SUM(usage_logs.input_tokens), 0) as input_tokens, COALESCE(SUM(usage_logs.output_tokens), 0) as output_tokens, COALESCE(SUM(usage_logs.cost_usd), 0) as cost_usd, COUNT(*) as request_count").
		Joins("LEFT JOIN users ON usage_logs.user_id = users.id")

	if startDate != "" {
		userQuery = userQuery.Where("usage_logs.created_at >= ?", startDate)
	}
	if endDate != "" {
		userQuery = userQuery.Where("usage_logs.created_at <= ?", endDate+" 23:59:59")
	}

	userQuery.Group("usage_logs.user_id, users.username").Scan(&userUsage)

	// Get usage by model
	type ModelUsage struct {
		ModelName    string  `json:"model_name"`
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
		CostUSD      float64 `json:"cost_usd"`
		RequestCount int64   `json:"request_count"`
	}
	var modelUsage []ModelUsage

	modelQuery := models.DB.Table("usage_logs")
	if startDate != "" {
		modelQuery = modelQuery.Where("created_at >= ?", startDate)
	}
	if endDate != "" {
		modelQuery = modelQuery.Where("created_at <= ?", endDate+" 23:59:59")
	}

	modelQuery.Select("model_name, COALESCE(SUM(input_tokens), 0) as input_tokens, COALESCE(SUM(output_tokens), 0) as output_tokens, COALESCE(SUM(cost_usd), 0) as cost_usd, COUNT(*) as request_count").
		Group("model_name").
		Scan(&modelUsage)

	c.JSON(http.StatusOK, gin.H{
		"totals": gin.H{
			"input_tokens":  totalInput,
			"output_tokens": totalOutput,
			"cost_usd":      totalCost,
			"requests":      totalRequests,
		},
		"by_user":  userUsage,
		"by_model": modelUsage,
	})
}

// generateRandomHex generates random hex string
func generateRandomHex(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
