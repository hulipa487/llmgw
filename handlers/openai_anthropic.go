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
	StreamOptions *StreamOptions   `json:"stream_options,omitempty"`
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
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

// AnthropicEvent represents Anthropic streaming events
type AnthropicEvent struct {
	Type         string          `json:"type"`
	Message      *AnthropicMessageStream `json:"message,omitempty"`
	Delta        *AnthropicDelta `json:"delta,omitempty"`
	Usage        *AnthropicUsage `json:"usage,omitempty"`
	Index        int             `json:"index,omitempty"`
	ContentBlock *AnthropicContentBlock `json:"content_block,omitempty"`
}

type AnthropicMessageStream struct {
	ID           string        `json:"id"`
	Type         string        `json:"type"`
	Role         string        `json:"role"`
	Content      []interface{} `json:"content"`
	Model        string        `json:"model"`
	StopReason   *string       `json:"stop_reason,omitempty"`
	StopSequence *string       `json:"stop_sequence,omitempty"`
	Usage        *AnthropicUsage `json:"usage,omitempty"`
}

type AnthropicDelta struct {
	Type         string `json:"type"`
	Text         string `json:"text"`
	StopReason   *string `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

type AnthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// AnthropicToOpenAI handles Anthropic-style requests and converts to OpenAI upstream
func AnthropicToOpenAI(c *gin.Context) {
	apiKey, _ := middleware.GetCurrentAPIKey(c)

	// Read request body
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body", "type": "error"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Parse Anthropic request
	var anthropicReq AnthropicMessagesRequest
	if err := json.Unmarshal(bodyBytes, &anthropicReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON", "type": "error"})
		return
	}

	// Find model and upstream
	modelWithUpstream, err := getModelWithUpstream(anthropicReq.Model, apiKey.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Model '%s' not found or not available", anthropicReq.Model), "type": "error"})
		return
	}

	// Convert Anthropic messages to OpenAI format
	// Use the upstream model name for the request
	openaiReq := convertAnthropicToOpenAIRequest(anthropicReq, modelWithUpstream.UpstreamModelName)

	// Marshal OpenAI request
	openaiBody, err := json.Marshal(openaiReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to convert request", "type": "error"})
		return
	}

	// Build upstream URL (OpenAI-compatible endpoint)
	upstreamURL := fmt.Sprintf("%s%s/chat/completions", modelWithUpstream.Upstream.BaseURL, modelWithUpstream.Upstream.APIPath)

	// Create upstream request
	upstreamReq, err := http.NewRequest("POST", upstreamURL, bytes.NewBuffer(openaiBody))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create upstream request", "type": "error"})
		return
	}

	// Set headers for OpenAI API
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Authorization", "Bearer "+modelWithUpstream.Upstream.Key)

	startTime := time.Now()

	// Handle streaming
	if anthropicReq.Stream {
		handleAnthropicStreaming(c, upstreamReq, apiKey, modelWithUpstream, startTime)
		return
	}

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

	// Handle non-200 responses - pass through errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to extract error message from upstream response
		var errResp map[string]interface{}
		if json.Unmarshal(respBody, &errResp) == nil {
			c.JSON(resp.StatusCode, errResp)
			return
		}
		c.JSON(resp.StatusCode, gin.H{"error": string(respBody), "type": "error"})
		return
	}

	// Convert OpenAI response to Anthropic format
	anthropicResp := convertOpenAIToAnthropicResponse(respBody, modelWithUpstream.Model.Name)

	// Write Anthropic-formatted response
	c.Status(resp.StatusCode)
	c.Header("Content-Type", "application/json")
	c.Writer.Write(anthropicResp)

	// Log usage if successful
	var oiResp OpenAIChatResponse
	if err := json.Unmarshal(respBody, &oiResp); err == nil {
		if oiResp.Usage.PromptTokens > 0 || oiResp.Usage.CompletionTokens > 0 {
			logUsage(apiKey.ID, apiKey.UserID, modelWithUpstream.Model.Name,
				oiResp.Usage.PromptTokens, oiResp.Usage.CompletionTokens,
				latencyMs, modelWithUpstream.Model.PriceInputPerM, modelWithUpstream.Model.PriceOutputPerM)
		}
	}
}

// handleAnthropicStreaming handles streaming requests from Anthropic to OpenAI upstream
func handleAnthropicStreaming(c *gin.Context, upstreamReq *http.Request, apiKey *models.APIKey, modelWithUpstream *ModelWithUpstream, startTime time.Time) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(upstreamReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Upstream request failed: %v", err), "type": "error"})
		return
	}
	defer resp.Body.Close()

	latencyMs := time.Since(startTime).Milliseconds()

	// Set up streaming response headers for Anthropic SSE
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
	messageID := fmt.Sprintf("msg_%d", time.Now().UnixNano())

	// Send message_start event
	c.Writer.Write([]byte(fmt.Sprintf("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"%s\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"%s\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n", messageID, modelWithUpstream.Model.Name)))
	flusher.Flush()

	// Send content_block_start event
	c.Writer.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"))
	flusher.Flush()

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
			data = strings.TrimSpace(data)

			if data == "[DONE]" {
				continue
			}

			// Parse OpenAI streaming chunk
			var chunk OpenAIChatResponse
			if err := json.Unmarshal([]byte(data), &chunk); err == nil {
				if len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
					deltaText := chunk.Choices[0].Delta.Content
					if deltaText != "" {
						contentBuilder.WriteString(deltaText)

						// Send delta event
						event := fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"%s\"}}\n\n", escapeJson(deltaText))
						c.Writer.Write([]byte(event))
						flusher.Flush()
					}
				}

				// Extract usage if available
				if chunk.Usage.PromptTokens > 0 {
					inputTokens = chunk.Usage.PromptTokens
				}
				if chunk.Usage.CompletionTokens > 0 {
					outputTokens = chunk.Usage.CompletionTokens
				}
			}
		}
	}

	// Send content_block_stop event
	c.Writer.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
	flusher.Flush()

	// Send message_delta with stop_reason
	stopReason := "end_turn"
	c.Writer.Write([]byte(fmt.Sprintf("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"%s\"},\"usage\":{\"output_tokens\":%d}}\n\n", stopReason, outputTokens)))
	flusher.Flush()

	// Send message_stop event
	c.Writer.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	flusher.Flush()

	// Log usage
	if inputTokens > 0 || outputTokens > 0 {
		logUsage(apiKey.ID, apiKey.UserID, modelWithUpstream.Model.Name,
			inputTokens, outputTokens, latencyMs,
			modelWithUpstream.Model.PriceInputPerM, modelWithUpstream.Model.PriceOutputPerM)
	}
}

// convertAnthropicToOpenAIRequest converts Anthropic request to OpenAI format
func convertAnthropicToOpenAIRequest(anthropicReq AnthropicMessagesRequest, upstreamModelName string) OpenAIChatRequest {
	var messages []OpenAIMessage

	// Add system message if present
	if anthropicReq.System != nil {
		if sysStr, ok := anthropicReq.System.(string); ok {
			messages = append(messages, OpenAIMessage{
				Role:    "system",
				Content: sysStr,
			})
		}
	}

	// Convert messages
	for _, msg := range anthropicReq.Messages {
		messages = append(messages, OpenAIMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	return OpenAIChatRequest{
		Model:       upstreamModelName, // Use upstream model name
		Messages:    messages,
		MaxTokens:   anthropicReq.MaxTokens,
		Temperature: anthropicReq.Temperature,
		Stream:      anthropicReq.Stream,
	}
}

// convertOpenAIToAnthropicResponse converts OpenAI response to Anthropic format
func convertOpenAIToAnthropicResponse(openaiBody []byte, modelName string) []byte {
	var oiResp OpenAIChatResponse
	if err := json.Unmarshal(openaiBody, &oiResp); err != nil {
		// If parsing fails, return error response in Anthropic format
		return []byte(fmt.Sprintf(`{"type":"error","error":{"message":"Failed to parse upstream response: %s"}}`, err.Error()))
	}

	// Extract content from choices
	var contentText string
	if len(oiResp.Choices) > 0 {
		content := oiResp.Choices[0].Message.Content
		switch v := content.(type) {
		case string:
			contentText = v
		case []interface{}:
			// Handle multi-part content (extract text from first text part)
			for _, part := range v {
				if partMap, ok := part.(map[string]interface{}); ok {
					if partMap["type"] == "text" {
						if text, ok := partMap["text"].(string); ok {
							contentText = text
							break
						}
					}
				}
			}
		}
	}

	stopReason := "end_turn"
	anthropicResp := AnthropicMessagesResponse{
		ID:   fmt.Sprintf("msg_%s", oiResp.ID),
		Type: "message",
		Role: "assistant",
		Content: []AnthropicContent{
			{
				Type: "text",
				Text: contentText,
			},
		},
		Model: modelName,
		StopReason: &stopReason,
		Usage: AnthropicUsage{
			InputTokens:  oiResp.Usage.PromptTokens,
			OutputTokens: oiResp.Usage.CompletionTokens,
		},
	}

	result, _ := json.Marshal(anthropicResp)
	return result
}

// escapeJson escapes special characters for JSON string
func escapeJson(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}

// AnthropicListModels lists available models
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
