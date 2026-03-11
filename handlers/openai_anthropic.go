package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"llmgw/middleware"
	"llmgw/models"
	"log"
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
	Tools       []AnthropicTool    `json:"tools,omitempty"`
}

type AnthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
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
	Type  string                 `json:"type"`
	Text  string                 `json:"text,omitempty"`
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
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

	// Log the converted request for debugging
	log.Printf("OpenAI request body: %s", string(openaiBody))

	// Build upstream URL (OpenAI-compatible endpoint)
	upstreamURL := fmt.Sprintf("%s%s/chat/completions", modelWithUpstream.Upstream.BaseURL, modelWithUpstream.Upstream.APIPath)

	log.Printf("Upstream URL: %s", upstreamURL)

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

	// Log the request for debugging
	log.Printf("Anthropic request: model=%s, stream=%v, messages=%d", anthropicReq.Model, anthropicReq.Stream, len(anthropicReq.Messages))

	// Handle streaming
	if anthropicReq.Stream {
		log.Printf("Handling streaming request for model %s", anthropicReq.Model)
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

	log.Printf("Upstream response: status=%d, body=%s", resp.StatusCode, string(respBody))

	// Handle non-200 responses - pass through errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to extract error message from upstream response
		var errResp map[string]interface{}
		if json.Unmarshal(respBody, &errResp) == nil {
			log.Printf("Upstream error response: %+v", errResp)
			c.JSON(resp.StatusCode, errResp)
			return
		}
		c.JSON(resp.StatusCode, gin.H{"error": string(respBody), "type": "error"})
		return
	}

	// Convert OpenAI response to Anthropic format
	anthropicResp := convertOpenAIToAnthropicResponse(respBody, modelWithUpstream.Model.Name)
	log.Printf("Anthropic response: len=%d", len(anthropicResp))

	// Write Anthropic-formatted response using c.Data for proper handling
	c.Data(resp.StatusCode, "application/json", anthropicResp)

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
	requestID := fmt.Sprintf("req_%d", time.Now().UnixNano())
	log.Printf("[%s] Starting streaming request to upstream", requestID)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(upstreamReq)
	if err != nil {
		log.Printf("[%s] Streaming upstream request failed: %v", requestID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Upstream request failed: %v", err), "type": "error"})
		return
	}
	defer resp.Body.Close()

	log.Printf("[%s] Upstream response status: %d", requestID, resp.StatusCode)

	// Handle non-200 responses - read body and return error
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			c.JSON(resp.StatusCode, gin.H{"error": "Failed to read upstream error response", "type": "error"})
			return
		}
		log.Printf("[%s] Upstream error response: %s", requestID, string(respBody))
		var errResp map[string]interface{}
		if json.Unmarshal(respBody, &errResp) == nil {
			c.JSON(resp.StatusCode, errResp)
			return
		}
		c.JSON(resp.StatusCode, gin.H{"error": string(respBody), "type": "error"})
		return
	}

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
	var inputTokens, outputTokens int
	messageID := fmt.Sprintf("msg_%d", time.Now().UnixNano())

	log.Printf("[%s] Starting to send SSE events", requestID)

	// Send message_start event
	messageStart := fmt.Sprintf(`event: message_start
data: {"type":"message_start","message":{"id":"%s","type":"message","role":"assistant","content":[],"model":"%s","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}

`, messageID, modelWithUpstream.Model.Name)
	c.Writer.Write([]byte(messageStart))
	flusher.Flush()

	// Track content blocks
	type contentBlock struct {
		blockType   string // "text" or "tool_use"
		text        string
		toolID      string
		toolName    string
		toolArgs    string
	}
	var contentBlocks []contentBlock
	currentBlockIndex := -1
	var stopReason string = "end_turn"

	chunkCount := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				log.Printf("[%s] Stream ended after %d chunks", requestID, chunkCount)
				break
			}
			log.Printf("[%s] Stream read error: %v", requestID, err)
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
				chunkCount++
				if len(chunk.Choices) > 0 {
					choice := chunk.Choices[0]

					// Check finish reason
					if choice.FinishReason == "tool_calls" {
						stopReason = "tool_use"
					}

					if choice.Delta != nil {
						// Handle text content
						if choice.Delta.Content != "" {
							// Find or create text block
							if currentBlockIndex < 0 || contentBlocks[currentBlockIndex].blockType != "text" {
								// Start new text block
								contentBlocks = append(contentBlocks, contentBlock{blockType: "text"})
								currentBlockIndex = len(contentBlocks) - 1

								// Send content_block_start
								c.Writer.Write([]byte(fmt.Sprintf("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n", currentBlockIndex)))
								flusher.Flush()
							}

							contentBlocks[currentBlockIndex].text += choice.Delta.Content

							// Send delta
							deltaEvent := fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"text_delta\",\"text\":\"%s\"}}\n\n", currentBlockIndex, escapeJson(choice.Delta.Content))
							c.Writer.Write([]byte(deltaEvent))
							flusher.Flush()
						}

						// Handle tool calls
						if len(choice.Delta.ToolCalls) > 0 {
							for _, tc := range choice.Delta.ToolCalls {
								// Find existing tool block or create new one
								var blockIdx int = -1
								for i, b := range contentBlocks {
									if b.blockType == "tool_use" && b.toolID == tc.ID {
										blockIdx = i
										break
									}
								}

								if blockIdx < 0 && tc.ID != "" {
									// Start new tool_use block
									contentBlocks = append(contentBlocks, contentBlock{
										blockType: "tool_use",
										toolID:    tc.ID,
										toolName:  tc.Function.Name,
									})
									blockIdx = len(contentBlocks) - 1
									currentBlockIndex = blockIdx

									// Send content_block_start for tool_use
									c.Writer.Write([]byte(fmt.Sprintf("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":{\"type\":\"tool_use\",\"id\":\"%s\",\"name\":\"%s\",\"input\":{}}}\n\n", blockIdx, tc.ID, tc.Function.Name)))
									flusher.Flush()
								}

								// Stream tool arguments
								if tc.Function.Arguments != "" && blockIdx >= 0 {
									contentBlocks[blockIdx].toolArgs += tc.Function.Arguments

									// Send input_json_delta
									deltaEvent := fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"%s\"}}\n\n", blockIdx, escapeJson(tc.Function.Arguments))
									c.Writer.Write([]byte(deltaEvent))
									flusher.Flush()
								}
							}
						}
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

	// Send content_block_stop for all blocks
	for i := range contentBlocks {
		c.Writer.Write([]byte(fmt.Sprintf("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", i)))
		flusher.Flush()
	}

	// Send message_delta with stop_reason
	c.Writer.Write([]byte(fmt.Sprintf("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"%s\"},\"usage\":{\"output_tokens\":%d}}\n\n", stopReason, outputTokens)))
	flusher.Flush()

	// Send message_stop event
	c.Writer.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	flusher.Flush()

	log.Printf("[%s] Streaming complete: input_tokens=%d, output_tokens=%d, blocks=%d, stop_reason=%s", requestID, inputTokens, outputTokens, len(contentBlocks), stopReason)

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

	// Convert messages - handle tool_use and tool_result
	for _, msg := range anthropicReq.Messages {
		convertedMsgs := convertAnthropicMessageToOpenAI(msg)
		messages = append(messages, convertedMsgs...)
	}

	// Build request with optional fields only if non-zero
	req := OpenAIChatRequest{
		Model:    upstreamModelName, // Use upstream model name
		Messages: messages,
	}

	// Only include max_tokens if specified
	if anthropicReq.MaxTokens > 0 {
		req.MaxTokens = &anthropicReq.MaxTokens
	}

	// Only include temperature if specified (non-zero)
	if anthropicReq.Temperature != 0 {
		req.Temperature = &anthropicReq.Temperature
	}

	// Only include stream if true
	if anthropicReq.Stream {
		req.Stream = &anthropicReq.Stream
	}

	// Convert tools if present
	if len(anthropicReq.Tools) > 0 {
		req.Tools = convertAnthropicToolsToOpenAI(anthropicReq.Tools)
	}

	return req
}

// convertAnthropicMessageToOpenAI converts a single Anthropic message to one or more OpenAI messages
func convertAnthropicMessageToOpenAI(msg AnthropicMessage) []OpenAIMessage {
	content := msg.Content

	// Handle string content
	if str, ok := content.(string); ok {
		return []OpenAIMessage{{
			Role:    msg.Role,
			Content: str,
		}}
	}

	// Handle array of content blocks
	blocks, ok := content.([]interface{})
	if !ok {
		// Unknown format, pass through
		return []OpenAIMessage{{
			Role:    msg.Role,
			Content: content,
		}}
	}

	// Check if this message contains tool_result (user message with tool results)
	// In this case, we need to convert to separate "tool" role messages
	if msg.Role == "user" {
		var toolResults []OpenAIMessage
		var otherContent []interface{}

		for _, block := range blocks {
			if blockMap, ok := block.(map[string]interface{}); ok {
				blockType, _ := blockMap["type"].(string)
				if blockType == "tool_result" {
					// Convert to tool role message
					toolUseID, _ := blockMap["tool_use_id"].(string)
					resultContent := blockMap["content"]

					// Handle content - can be string or array
					var contentStr string
					switch v := resultContent.(type) {
					case string:
						contentStr = v
					case []interface{}:
						// Extract text from content blocks
						for _, c := range v {
							if cm, ok := c.(map[string]interface{}); ok {
								if cm["type"] == "text" {
									if t, ok := cm["text"].(string); ok {
										contentStr = t
										break
									}
								}
							}
						}
					}

					toolResults = append(toolResults, OpenAIMessage{
						Role:       "tool",
						ToolCallID: toolUseID,
						Content:    contentStr,
					})
				} else {
					otherContent = append(otherContent, block)
				}
			}
		}

		// If we have tool results, return them
		if len(toolResults) > 0 {
			// If there's other content too, we might need a user message first
			// But typically tool_result messages only contain tool results
			return toolResults
		}
	}

	// Check if this message contains tool_use (assistant message with tool calls)
	if msg.Role == "assistant" {
		var toolCalls []OpenAIToolCall
		var textContent string
		var otherContent []interface{}

		for _, block := range blocks {
			if blockMap, ok := block.(map[string]interface{}); ok {
				blockType, _ := blockMap["type"].(string)
				switch blockType {
				case "text":
					if text, ok := blockMap["text"].(string); ok {
						textContent = text
					}
				case "thinking":
					// Include thinking in text content
					if thinking, ok := blockMap["thinking"].(string); ok {
						if textContent != "" {
							textContent += "\n"
						}
						textContent += thinking
					}
				case "tool_use":
					// Convert to OpenAI tool call
					id, _ := blockMap["id"].(string)
					name, _ := blockMap["name"].(string)
					input := blockMap["input"]

					// Convert input to JSON string
					var argsStr string
					if input != nil {
						if argsBytes, err := json.Marshal(input); err == nil {
							argsStr = string(argsBytes)
						}
					}

					toolCalls = append(toolCalls, OpenAIToolCall{
						ID:   id,
						Type: "function",
						Function: OpenAIFunctionCall{
							Name:      name,
							Arguments: argsStr,
						},
					})
				default:
					otherContent = append(otherContent, block)
				}
			}
		}

		// Build assistant message with optional tool_calls
		openaiMsg := OpenAIMessage{
			Role: "assistant",
		}
		if textContent != "" {
			openaiMsg.Content = textContent
		}
		if len(toolCalls) > 0 {
			openaiMsg.ToolCalls = toolCalls
		}

		return []OpenAIMessage{openaiMsg}
	}

	// Default: convert content blocks to OpenAI format
	openaiContent := convertAnthropicContentToOpenAI(content)
	return []OpenAIMessage{{
		Role:    msg.Role,
		Content: openaiContent,
	}}
}

// convertAnthropicToolsToOpenAI converts Anthropic tools to OpenAI format
func convertAnthropicToolsToOpenAI(tools []AnthropicTool) []OpenAITool {
	var openaiTools []OpenAITool
	for _, tool := range tools {
		openaiTools = append(openaiTools, OpenAITool{
			Type: "function",
			Function: OpenAIFunctionDef{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}
	return openaiTools
}

// convertAnthropicContentToOpenAI converts Anthropic content to OpenAI format
func convertAnthropicContentToOpenAI(content interface{}) interface{} {
	switch v := content.(type) {
	case string:
		// Simple string - same in both
		return v
	case []interface{}:
		// Array of content blocks - convert each
		var openaiContent []interface{}
		for _, block := range v {
			if blockMap, ok := block.(map[string]interface{}); ok {
				blockType, _ := blockMap["type"].(string)
				switch blockType {
				case "text":
					// Text blocks are the same format
					if text, ok := blockMap["text"].(string); ok {
						openaiContent = append(openaiContent, map[string]interface{}{
							"type": "text",
							"text": text,
						})
					}
				case "thinking":
					// Convert thinking blocks to text blocks (for reasoning content)
					if thinking, ok := blockMap["thinking"].(string); ok {
						openaiContent = append(openaiContent, map[string]interface{}{
							"type": "text",
							"text": thinking,
						})
					}
				case "image":
					// Convert Anthropic image to OpenAI image_url
					if source, ok := blockMap["source"].(map[string]interface{}); ok {
						if sourceType, _ := source["type"].(string); sourceType == "base64" {
							mediaType, _ := source["media_type"].(string)
							data, _ := source["data"].(string)
							imageURL := fmt.Sprintf("data:%s;base64,%s", mediaType, data)
							openaiContent = append(openaiContent, map[string]interface{}{
								"type": "image_url",
								"image_url": map[string]interface{}{
									"url": imageURL,
								},
							})
						}
					}
				default:
					// Skip unsupported content types (thinking, tool_use, etc.)
					// Don't pass through - upstream APIs may not support them
					log.Printf("Skipping unsupported content type: %s", blockType)
				}
			}
		}
		if len(openaiContent) == 1 {
			// Single text block - simplify to string
			if textBlock, ok := openaiContent[0].(map[string]interface{}); ok {
				if textBlock["type"] == "text" {
					if text, ok := textBlock["text"].(string); ok {
						return text
					}
				}
			}
		}
		return openaiContent
	default:
		return content
	}
}

// convertOpenAIToAnthropicResponse converts OpenAI response to Anthropic format
func convertOpenAIToAnthropicResponse(openaiBody []byte, modelName string) []byte {
	var oiResp OpenAIChatResponse
	if err := json.Unmarshal(openaiBody, &oiResp); err != nil {
		// If parsing fails, return error response in Anthropic format
		return []byte(fmt.Sprintf(`{"type":"error","error":{"message":"Failed to parse upstream response: %s"}}`, err.Error()))
	}

	// Build content array
	var content []AnthropicContent
	var stopReason string = "end_turn"

	if len(oiResp.Choices) > 0 {
		choice := oiResp.Choices[0]

		// Check for tool calls
		if len(choice.Message.ToolCalls) > 0 {
			// Convert tool calls to tool_use content blocks
			for _, tc := range choice.Message.ToolCalls {
				// Parse arguments JSON
				var input map[string]interface{}
				if tc.Function.Arguments != "" {
					json.Unmarshal([]byte(tc.Function.Arguments), &input)
				}

				content = append(content, AnthropicContent{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: input,
				})
			}
			stopReason = "tool_use"
		} else {
			// Regular text content
			var contentText string
			switch v := choice.Message.Content.(type) {
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

			if contentText != "" {
				content = append(content, AnthropicContent{
					Type: "text",
					Text: contentText,
				})
			}
		}

		// Handle finish_reason
		if choice.FinishReason == "tool_calls" {
			stopReason = "tool_use"
		}
	}

	// Ensure at least one content block
	if len(content) == 0 {
		content = append(content, AnthropicContent{
			Type: "text",
			Text: "",
		})
	}

	anthropicResp := AnthropicMessagesResponse{
		ID:         fmt.Sprintf("msg_%s", oiResp.ID),
		Type:       "message",
		Role:       "assistant",
		Content:    content,
		Model:      modelName,
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
