package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"llmgw/middleware"
	"llmgw/models"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Anthropic request/response structures
type AnthropicMessagesRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Messages    []AnthropicMessage `json:"messages"`
	System      interface{}        `json:"system,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
}

type AnthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type AnthropicMessagesResponse struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Content      []AnthropicContent `json:"content"`
	Model        string             `json:"model"`
	StopReason   *string            `json:"stop_reason,omitempty"`
	StopSequence *string            `json:"stop_sequence,omitempty"`
	Usage        AnthropicUsage     `json:"usage"`
}

type AnthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type AnthropicModel struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	DisplayName string `json:"display_name,omitempty"`
}

type AnthropicModelsResponse struct {
	Data []AnthropicModel `json:"data"`
}

// AnthropicMessages handles Anthropic-compatible messages
func AnthropicMessages(c *gin.Context) {
	apiKey, _ := middleware.GetCurrentAPIKey(c)

	// Check rate limits
	if ok, used, limit := CheckRateLimit(apiKey.UserID); !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": fmt.Sprintf("Rate limit exceeded: %d/%d requests in 6-hour window", used, limit),
			"type":  "error",
		})
		return
	}
	if ok, used, limit := CheckMonthlyLimit(apiKey.UserID); !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": fmt.Sprintf("Monthly rate limit exceeded: %d/%d requests this month", used, limit),
			"type":  "error",
		})
		return
	}

	// Read request body
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body", "type": "error"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Parse request to get model name
	var req map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON", "type": "error"})
		return
	}

	modelName, ok := req["model"].(string)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Model name required", "type": "error"})
		return
	}

	// Find model and pick random upstream
	modelWithUpstream, err := getModelWithUpstream(modelName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Model '%s' not found or not available", modelName), "type": "error"})
		return
	}

	// Replace model name in request with upstream model name
	req["model"] = modelWithUpstream.UpstreamModelName
	modifiedBody, err := json.Marshal(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process request", "type": "error"})
		return
	}

	// Build upstream URL using Anthropic path
	upstreamURL := fmt.Sprintf("%s%s/messages", modelWithUpstream.Upstream.BaseURL, modelWithUpstream.Upstream.AnthropicPath)

	// Create upstream request
	upstreamReq, err := http.NewRequest("POST", upstreamURL, bytes.NewBuffer(modifiedBody))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create upstream request", "type": "error"})
		return
	}

	// Set headers
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("x-api-key", modelWithUpstream.Upstream.Key)
	upstreamReq.Header.Set("anthropic-version", c.GetHeader("anthropic-version"))
	if upstreamReq.Header.Get("anthropic-version") == "" {
		upstreamReq.Header.Set("anthropic-version", "2023-06-01")
	}

	// Check if streaming
	isStream, _ := req["stream"].(bool)

	startTime := time.Now()

	// Send request
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(upstreamReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Upstream request failed: %v", err), "type": "error"})
		return
	}
	defer resp.Body.Close()

	latencyMs := time.Since(startTime).Milliseconds()

	// Handle streaming response
	if isStream {
		handleAnthropicStream(c, resp, apiKey.ID, apiKey.UserID, modelWithUpstream.Model, latencyMs)
		return
	}

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read upstream response", "type": "error"})
		return
	}

	// Copy status code and headers
	c.Status(resp.StatusCode)
	c.Header("Content-Type", "application/json")

	// Write response
	c.Writer.Write(respBody)

	// Parse usage for logging
	var anthropicResp AnthropicMessagesResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err == nil {
		logUsage(apiKey.ID, apiKey.UserID, modelWithUpstream.Model.Name,
			anthropicResp.Usage.InputTokens, anthropicResp.Usage.OutputTokens,
			latencyMs, modelWithUpstream.Model.PriceInputPerM, modelWithUpstream.Model.PriceOutputPerM)
	}
}

func handleAnthropicStream(c *gin.Context, resp *http.Response, apiKeyID, userID uint, model models.Model, latencyMs int64) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming not supported", "type": "error"})
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

			// Write to client
			c.Writer.Write([]byte(line))
			flusher.Flush()

			// Try to parse for usage (in message_start or message_delta)
			var event map[string]interface{}
			if json.Unmarshal([]byte(data), &event) == nil {
				if eventType, ok := event["type"].(string); ok {
					if eventType == "message_start" {
						if msg, ok := event["message"].(map[string]interface{}); ok {
							if usage, ok := msg["usage"].(map[string]interface{}); ok {
								if it, ok := usage["input_tokens"].(float64); ok {
									totalInputTokens = int(it)
								}
							}
						}
					} else if eventType == "message_delta" {
						if usage, ok := event["usage"].(map[string]interface{}); ok {
							if ot, ok := usage["output_tokens"].(float64); ok {
								totalOutputTokens = int(ot)
							}
						}
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

// AnthropicListModels lists available models (same as OpenAI)
func AnthropicListModels(c *gin.Context) {
	var modelList []models.Model
	if err := models.DB.Where("is_enabled = ?", true).Find(&modelList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch models", "type": "error"})
		return
	}

	var data []AnthropicModel
	for _, m := range modelList {
		data = append(data, AnthropicModel{
			ID:          m.Name,
			Type:        "model",
			DisplayName: m.Name,
		})
	}

	c.JSON(http.StatusOK, AnthropicModelsResponse{
		Data: data,
	})
}