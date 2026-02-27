package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"llmgw/middleware"
	"llmgw/models"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// OpenAI request/response structures
type OpenAIChatRequest struct {
	Model       string                 `json:"model"`
	Messages    []OpenAIMessage        `json:"messages"`
	MaxTokens   int                    `json:"max_tokens,omitempty"`
	Temperature float64                `json:"temperature,omitempty"`
	Stream      bool                   `json:"stream,omitempty"`
	Extra       map[string]interface{} `json:"-"` // Additional fields
}

type OpenAIMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type OpenAIChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage    `json:"usage"`
}

type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	Delta        *OpenAIDelta  `json:"delta,omitempty"`
	FinishReason string        `json:"finish_reason"`
}

type OpenAIDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type OpenAIModelsResponse struct {
	Object string        `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

// ModelWithUpstream holds a model with its upstream and the model name for that upstream
type ModelWithUpstream struct {
	Model           models.Model
	Upstream        models.UpstreamConfig
	UpstreamModelName string
}

// getModelWithUpstream finds a model by name and picks a random upstream
func getModelWithUpstream(modelName string) (*ModelWithUpstream, error) {
	var model models.Model
	if err := models.DB.Where("name = ? AND is_enabled = ?", modelName, true).
		Preload("Upstreams").First(&model).Error; err != nil {
		return nil, err
	}

	if len(model.Upstreams) == 0 {
		return nil, fmt.Errorf("no upstreams configured for model %s", modelName)
	}

	// Pick a random upstream
	upstream := model.Upstreams[rand.Intn(len(model.Upstreams))]

	// Get the upstream model name from the junction table
	var modelUpstream models.ModelUpstream
	if err := models.DB.Where("model_id = ? AND upstream_config_id = ?", model.ID, upstream.ID).
		First(&modelUpstream).Error; err != nil {
		return nil, fmt.Errorf("model-upstream mapping not found")
	}

	return &ModelWithUpstream{
		Model:             model,
		Upstream:          upstream,
		UpstreamModelName: modelUpstream.UpstreamModelName,
	}, nil
}

// OpenAIChatCompletions handles OpenAI-compatible chat completions
func OpenAIChatCompletions(c *gin.Context) {
	apiKey, _ := middleware.GetCurrentAPIKey(c)

	// Check rate limits
	if ok, used, limit := CheckRateLimit(apiKey.UserID); !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": fmt.Sprintf("Rate limit exceeded: %d/%d requests in 6-hour window", used, limit),
		})
		return
	}
	if ok, used, limit := CheckMonthlyLimit(apiKey.UserID); !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": fmt.Sprintf("Monthly rate limit exceeded: %d/%d requests this month", used, limit),
		})
		return
	}

	// Read request body
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Parse request to get model name
	var req map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	modelName, ok := req["model"].(string)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Model name required"})
		return
	}

	// Find model and pick random upstream
	modelWithUpstream, err := getModelWithUpstream(modelName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Model '%s' not found or not available", modelName)})
		return
	}

	// Replace model name in request with upstream model name
	req["model"] = modelWithUpstream.UpstreamModelName
	modifiedBody, err := json.Marshal(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process request"})
		return
	}

	// Build upstream URL using OpenAI path
	upstreamURL := fmt.Sprintf("%s%s/chat/completions", modelWithUpstream.Upstream.BaseURL, modelWithUpstream.Upstream.OpenAIPath)

	// Create upstream request
	upstreamReq, err := http.NewRequest("POST", upstreamURL, bytes.NewBuffer(modifiedBody))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create upstream request"})
		return
	}

	// Set headers
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Authorization", "Bearer "+modelWithUpstream.Upstream.Key)

	// Check if streaming
	isStream, _ := req["stream"].(bool)

	startTime := time.Now()

	// Send request
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(upstreamReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Upstream request failed: %v", err)})
		return
	}
	defer resp.Body.Close()

	latencyMs := time.Since(startTime).Milliseconds()

	// Handle streaming response
	if isStream {
		handleOpenAIStream(c, resp, apiKey.ID, apiKey.UserID, modelWithUpstream.Model, latencyMs)
		return
	}

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read upstream response"})
		return
	}

	// Copy status code and headers
	c.Status(resp.StatusCode)
	for k, v := range resp.Header {
		c.Header(k, v[0])
	}

	// Write response
	c.Writer.Write(respBody)

	// Only log usage if request was successful (2xx status)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Parse usage for logging
		var openaiResp OpenAIChatResponse
		if err := json.Unmarshal(respBody, &openaiResp); err == nil {
			if openaiResp.Usage.PromptTokens > 0 || openaiResp.Usage.CompletionTokens > 0 {
				logUsage(apiKey.ID, apiKey.UserID, modelWithUpstream.Model.Name,
					openaiResp.Usage.PromptTokens, openaiResp.Usage.CompletionTokens,
					latencyMs, modelWithUpstream.Model.PriceInputPerM, modelWithUpstream.Model.PriceOutputPerM)
			}
		}
	}
}

func handleOpenAIStream(c *gin.Context, resp *http.Response, apiKeyID, userID uint, model models.Model, latencyMs int64) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming not supported"})
		return
	}

	reader := bufio.NewReader(resp.Body)
	var totalInputTokens, totalOutputTokens int

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if strings.TrimSpace(data) == "[DONE]" {
				c.Writer.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()
				break
			}

			// Write to client
			c.Writer.Write([]byte(line))
			flusher.Flush()

			// Try to parse for usage (if included)
			var chunk map[string]interface{}
			if json.Unmarshal([]byte(data), &chunk) == nil {
				if usage, ok := chunk["usage"].(map[string]interface{}); ok {
					if pt, ok := usage["prompt_tokens"].(float64); ok {
						totalInputTokens = int(pt)
					}
					if ct, ok := usage["completion_tokens"].(float64); ok {
						totalOutputTokens = int(ct)
					}
				}
			}
		} else {
			c.Writer.Write([]byte(line))
			flusher.Flush()
		}
	}

	// Log usage if we have token info
	if totalInputTokens > 0 || totalOutputTokens > 0 {
		logUsage(apiKeyID, userID, model.Name, totalInputTokens, totalOutputTokens,
			latencyMs, model.PriceInputPerM, model.PriceOutputPerM)
	}
}

// OpenAIListModels lists available models
func OpenAIListModels(c *gin.Context) {
	var modelList []models.Model
	if err := models.DB.Where("is_enabled = ?", true).Find(&modelList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch models"})
		return
	}

	var data []OpenAIModel
	for _, m := range modelList {
		data = append(data, OpenAIModel{
			ID:      m.Name,
			Object:  "model",
			Created: m.CreatedAt.Unix(),
			OwnedBy: "llmgw",
		})
	}

	c.JSON(http.StatusOK, OpenAIModelsResponse{
		Object: "list",
		Data:   data,
	})
}

// OpenAIGetModel gets a specific model
func OpenAIGetModel(c *gin.Context) {
	modelName := c.Param("model")

	var model models.Model
	if err := models.DB.Where("name = ? AND is_enabled = ?", modelName, true).First(&model).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Model not found"})
		return
	}

	c.JSON(http.StatusOK, OpenAIModel{
		ID:      model.Name,
		Object:  "model",
		Created: model.CreatedAt.Unix(),
		OwnedBy: "llmgw",
	})
}

func logUsage(apiKeyID, userID uint, modelName string,
	inputTokens, outputTokens int, latencyMs int64, priceIn, priceOut float64) {
	// Calculate cost
	costUSD := (float64(inputTokens)/1000000)*priceIn + (float64(outputTokens)/1000000)*priceOut

	usageLog := models.UsageLog{
		APIKeyID:     apiKeyID,
		UserID:       userID,
		ModelName:    modelName,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		LatencyMs:    latencyMs,
		CostUSD:      costUSD,
	}

	models.DB.Create(&usageLog)
}