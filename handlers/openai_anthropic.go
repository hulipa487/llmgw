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

type AnthropicModelsResponse struct {
	Data []AnthropicModel `json:"data"`
}

type AnthropicModel struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name,omitempty"`
}

// OpenAIToAnthropic converts OpenAI chat request to Anthropic format and forwards to upstream
func OpenAIToAnthropic(c *gin.Context) {
	apiKey, _ := middleware.GetCurrentAPIKey(c)

	// Read request body
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body", "type": "error"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Parse OpenAI request
	var openaiReq OpenAIChatRequest
	if err := json.Unmarshal(bodyBytes, &openaiReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON", "type": "error"})
		return
	}

	// Find model and upstream
	modelWithUpstream, err := getModelWithUpstream(openaiReq.Model, apiKey.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Model '%s' not found or not available", openaiReq.Model), "type": "error"})
		return
	}

	// Convert OpenAI messages to Anthropic format
	anthropicReq := convertOpenAIToAnthropic(openaiReq)

	// Marshal Anthropic request
	anthropicBody, err := json.Marshal(anthropicReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to convert request", "type": "error"})
		return
	}

	// Build upstream URL (Anthropic-compatible endpoint)
	upstreamURL := fmt.Sprintf("%s%s/messages", modelWithUpstream.Upstream.BaseURL, modelWithUpstream.Upstream.APIPath)

	// Create upstream request
	upstreamReq, err := http.NewRequest("POST", upstreamURL, bytes.NewBuffer(anthropicBody))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create upstream request", "type": "error"})
		return
	}

	// Set headers for Anthropic API
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("x-api-key", modelWithUpstream.Upstream.Key)
	upstreamReq.Header.Set("anthropic-version", "2023-06-01")

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

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read upstream response", "type": "error"})
		return
	}

	// Convert Anthropic response to OpenAI format
	openaiResp := convertAnthropicToOpenAI(respBody, modelWithUpstream.Model.Name)

	// Write OpenAI-formatted response
	c.Status(resp.StatusCode)
	c.Header("Content-Type", "application/json")
	c.Writer.Write(openaiResp)

	// Log usage if successful
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var antResp AnthropicMessagesResponse
		if err := json.Unmarshal(respBody, &antResp); err == nil {
			if antResp.Usage.InputTokens > 0 || antResp.Usage.OutputTokens > 0 {
				logUsage(apiKey.ID, apiKey.UserID, modelWithUpstream.Model.Name,
					antResp.Usage.InputTokens, antResp.Usage.OutputTokens,
					latencyMs, modelWithUpstream.Model.PriceInputPerM, modelWithUpstream.Model.PriceOutputPerM)
			}
		}
	}
}

// convertOpenAIToAnthropic converts OpenAI chat request to Anthropic messages format
func convertOpenAIToAnthropic(openaiReq OpenAIChatRequest) AnthropicMessagesRequest {
	var messages []AnthropicMessage
	var systemMsg interface{}

	for _, msg := range openaiReq.Messages {
		switch msg.Role {
		case "system":
			systemMsg = msg.Content
		case "assistant":
			messages = append(messages, AnthropicMessage{
				Role:    "assistant",
				Content: msg.Content,
			})
		case "user":
			messages = append(messages, AnthropicMessage{
				Role:    "user",
				Content: msg.Content,
			})
		}
	}

	return AnthropicMessagesRequest{
		Model:       openaiReq.Model,
		MaxTokens:   openaiReq.MaxTokens,
		Messages:    messages,
		System:      systemMsg,
		Temperature: openaiReq.Temperature,
		Stream:      openaiReq.Stream,
	}
}

// convertAnthropicToOpenAI converts Anthropic response to OpenAI chat format
func convertAnthropicToOpenAI(anthropicBody []byte, modelName string) []byte {
	var antResp AnthropicMessagesResponse
	if err := json.Unmarshal(anthropicBody, &antResp); err != nil {
		// If parsing fails, return original
		return anthropicBody
	}

	// Combine content into single text
	var contentText string
	for _, c := range antResp.Content {
		if c.Type == "text" {
			contentText += c.Text
		}
	}

	openaiResp := OpenAIChatResponse{
		ID:      antResp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   modelName,
		Choices: []OpenAIChoice{
			{
				Index: 0,
				Message: OpenAIMessage{
					Role:    "assistant",
					Content: contentText,
				},
				FinishReason: "stop",
			},
		},
		Usage: OpenAIUsage{
			PromptTokens:     antResp.Usage.InputTokens,
			CompletionTokens: antResp.Usage.OutputTokens,
			TotalTokens:      antResp.Usage.InputTokens + antResp.Usage.OutputTokens,
		},
	}

	result, _ := json.Marshal(openaiResp)
	return result
}

// StreamOpenAIToAnthropic handles streaming conversion from OpenAI to Anthropic format
func StreamOpenAIToAnthropic(c *gin.Context) {
	apiKey, _ := middleware.GetCurrentAPIKey(c)

	// Read request body
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body", "type": "error"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Parse OpenAI request
	var openaiReq OpenAIChatRequest
	if err := json.Unmarshal(bodyBytes, &openaiReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON", "type": "error"})
		return
	}

	if !openaiReq.Stream {
		// Not a streaming request, use regular handler
		OpenAIToAnthropic(c)
		return
	}

	// Find model and upstream
	modelWithUpstream, err := getModelWithUpstream(openaiReq.Model, apiKey.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Model '%s' not found or not available", openaiReq.Model), "type": "error"})
		return
	}

	// Convert OpenAI messages to Anthropic format
	anthropicReq := convertOpenAIToAnthropic(openaiReq)

	// Marshal Anthropic request
	anthropicBody, err := json.Marshal(anthropicReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to convert request", "type": "error"})
		return
	}

	// Build upstream URL
	upstreamURL := fmt.Sprintf("%s%s/messages", modelWithUpstream.Upstream.BaseURL, modelWithUpstream.Upstream.APIPath)

	// Create upstream request
	upstreamReq, err := http.NewRequest("POST", upstreamURL, bytes.NewBuffer(anthropicBody))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create upstream request", "type": "error"})
		return
	}

	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("x-api-key", modelWithUpstream.Upstream.Key)
	upstreamReq.Header.Set("anthropic-version", "2023-06-01")

	startTime := time.Now()

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(upstreamReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Upstream request failed: %v", err), "type": "error"})
		return
	}
	defer resp.Body.Close()

	latencyMs := time.Since(startTime).Milliseconds()

	// Set up streaming response
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming not supported", "type": "error"})
		return
	}

	reader := bufio.NewReader(resp.Body)
	var contentBuilder strings.Builder
	var inputTokens, outputTokens int

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		if strings.HasPrefix(line, "event: ") {
			continue // Skip event type lines
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Parse Anthropic delta
			if strings.Contains(data, "content_block_delta") || strings.Contains(data, "content_block_start") {
				var event map[string]interface{}
				if json.Unmarshal([]byte(data), &event) == nil {
					if delta, ok := event["delta"].(map[string]interface{}); ok {
						if text, ok := delta["text"].(string); ok && text != "" {
							contentBuilder.WriteString(text)
						}
					}
					if usage, ok := event["usage"].(map[string]interface{}); ok {
						if it, ok := usage["input_tokens"].(float64); ok {
							inputTokens = int(it)
						}
						if ot, ok := usage["output_tokens"].(float64); ok {
							outputTokens = int(ot)
						}
					}
				}
			}

			// Convert to OpenAI streaming format and forward
			openaiChunk := createOpenAIStreamingChunk(contentBuilder.String(), modelWithUpstream.Model.Name)
			c.Writer.Write([]byte(openaiChunk))
			flusher.Flush()
		}
	}

	// Send final chunk
	c.Writer.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()

	// Log usage
	if inputTokens > 0 || outputTokens > 0 {
		logUsage(apiKey.ID, apiKey.UserID, modelWithUpstream.Model.Name,
			inputTokens, outputTokens, latencyMs,
			modelWithUpstream.Model.PriceInputPerM, modelWithUpstream.Model.PriceOutputPerM)
	}
}

// createOpenAIStreamingChunk creates an OpenAI-compatible streaming chunk
func createOpenAIStreamingChunk(content string, modelName string) string {
	chunk := OpenAIChatResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().Unix()),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   modelName,
		Choices: []OpenAIChoice{
			{
				Index: 0,
				Delta: &OpenAIDelta{
					Content: content,
				},
				FinishReason: "",
			},
		},
	}

	data, _ := json.Marshal(chunk)
	return fmt.Sprintf("data: %s\n\n", string(data))
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
