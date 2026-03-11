# LLM Gateway (llmgw)

A lightweight LLM API gateway that proxies requests to upstream LLM providers with OpenAI-compatible APIs. Includes an Anthropic-to-OpenAI converter for seamless integration.

## Features

- **OpenAI-Compatible API**: Standard `/openai/chat/completions` endpoint
- **Anthropic-Compatible API**: `/anthropic/v1/messages` with automatic conversion to OpenAI format
- **Tool Support**: Full support for `tool_use`, `tool_result`, and function calling
- **Multiple Upstreams**: Configure multiple upstream providers with automatic load balancing
- **Model Aliasing**: Expose models under different names at the gateway level
- **MTFPass Authentication**: Telegram-based authentication via @mtfpassbot
- **Credits System**: Rolling window credits (5-hour window, monthly limits)
- **Usage Tracking**: Track token usage per user and model
- **Admin Panel**: Web UI for managing models, upstreams, and users
- **User Panel**: Web UI for API key management and usage monitoring

## Quick Start

```bash
# Build
go build -o llmgw .

# Run with config file
./llmgw -c /etc/llmgw.conf
```

## Configuration

Configuration is via JSON config file (default: `/etc/llmgw.conf`):

```json
{
    "host": "127.0.0.1",
    "port": 8080,
    "db": "postgres://user:password@localhost:5432/llmgw?sslmode=disable",
    "mtfpass_url": "https://mtfpass.example.com"
}
```

| Setting | Required | Description |
|---------|----------|-------------|
| `host` | No | Bind address (default: `127.0.0.1`) |
| `port` | No | Port (default: `8080`) |
| `db` | Yes | PostgreSQL connection string |
| `mtfpass_url` | Yes | MTFPass authentication service URL |

## API Endpoints

### OpenAI-Compatible API

```bash
# Chat completions (streaming and non-streaming)
curl -X POST http://localhost:8080/openai/chat/completions \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}'

# List models
curl http://localhost:8080/openai/models \
  -H "Authorization: Bearer YOUR_API_KEY"

# Get specific model
curl http://localhost:8080/openai/models/gpt-4 \
  -H "Authorization: Bearer YOUR_API_KEY"

# Top-level models endpoint (for compatibility)
curl http://localhost:8080/models \
  -H "Authorization: Bearer YOUR_API_KEY"
```

### Anthropic-Compatible API

The Anthropic endpoint accepts requests in Anthropic format and converts them to OpenAI format for upstream providers. Responses are converted back to Anthropic format.

```bash
# Messages (streaming and non-streaming)
curl -X POST http://localhost:8080/anthropic/v1/messages \
  -H "x-api-key: YOUR_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-opus",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello"}]
  }'

# With tools
curl -X POST http://localhost:8080/anthropic/v1/messages \
  -H "x-api-key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "max_tokens": 1024,
    "tools": [{
      "name": "get_weather",
      "description": "Get weather info",
      "input_schema": {
        "type": "object",
        "properties": {
          "location": {"type": "string"}
        }
      }
    }],
    "messages": [{"role": "user", "content": "What is the weather in Tokyo?"}]
  }'
```

## Web Interface

- **User Panel**: `http://localhost:8080/user/dashboard`
- **Admin Panel**: `http://localhost:8080/admin/dashboard`

Authentication is handled via MTFPass. Users must send `/start` to @mtfpassbot on Telegram to log in.

## Data Model

### Upstreams
An upstream is an LLM provider configuration:
- **Upstream ID**: Unique identifier
- **Name**: Display name
- **Base URL**: Provider API endpoint (e.g., `https://api.openai.com`)
- **API Path**: Path for API requests (e.g., `/v1`)
- **API Key**: Provider API key

### Models
A model is a gateway-level model that can be mapped to one or more upstreams:
- **Name**: The name exposed at the gateway (e.g., `gpt-4`)
- **Price**: Input/output price per million tokens
- **Enabled**: Whether the model is available
- **Upstreams**: Multiple upstream configurations with:
  - **Upstream**: Reference to an upstream provider
  - **Upstream Model Name**: The actual model name sent to the upstream

When a model has multiple upstreams, requests are load-balanced randomly with sticky sessions per API key.

### Users
Users are authenticated via MTFPass:
- **ID**: Telegram user ID (int64)
- **Username**: Telegram username
- **Role**: `user` or `admin`

### API Keys
- **Key Prefix**: First 8 characters for identification
- **Full Key**: Only shown once at creation
- **Active**: Can be disabled

### Usage Logs
- **API Key ID**: Which key was used
- **User ID**: Which user made the request
- **Model Name**: Gateway model name
- **Tokens**: Input/output token counts
- **Cost**: Calculated cost in USD
- **Latency**: Request latency in milliseconds

## Anthropic-to-OpenAI Conversion

The gateway automatically converts between Anthropic and OpenAI formats:

### Content Types
| Anthropic | OpenAI |
|-----------|--------|
| `text` | `text` |
| `thinking` | `text` (merged) |
| `image` (base64) | `image_url` |
| `tool_use` | `tool_calls` |
| `tool_result` | `role: "tool"` |

### Streaming Events
| Anthropic Event | Description |
|-----------------|-------------|
| `message_start` | Start of message |
| `content_block_start` | Start of content block (text or tool_use) |
| `content_block_delta` | Text delta or input_json_delta |
| `content_block_stop` | End of content block |
| `message_delta` | Stop reason and usage |
| `message_stop` | End of message |

## Credits System

Users have credits managed by MTFPass:
- **Rolling Window**: 100 credits per 5-hour window
- **Monthly Limit**: 18,000 credits per month
- **Admins**: Unlimited credits

## Workflow

1. **User Authentication**: Send `/start` to @mtfpassbot on Telegram
2. **Admin Setup**: Create upstreams and models in admin panel
3. **User Access**: Users create API keys in user panel
4. **API Requests**: Make requests using API keys

## Example Setup

### 1. Create an Upstream
```
Upstream ID: openai-main
Name: OpenAI Main
Base URL: https://api.openai.com
API Path: /v1
API Key: sk-xxx
```

### 2. Create a Model
```
Name: gpt-4
Price Input ($/1M tokens): 30.00
Price Output ($/1M tokens): 60.00
Enabled: Yes
Upstreams:
  - Upstream: OpenAI Main
  - Upstream Model Name: gpt-4-turbo-preview
```

### 3. Make Requests
```bash
# OpenAI format
curl -X POST http://localhost:8080/openai/chat/completions \
  -H "Authorization: Bearer llmgw_xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}'

# Anthropic format (converted to OpenAI upstream)
curl -X POST http://localhost:8080/anthropic/v1/messages \
  -H "x-api-key: llmgw_xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}'
```

## Tech Stack

- **Framework**: Gin
- **Database**: PostgreSQL with GORM
- **Auth**: MTFPass JWT cookies (web), API keys (API)
- **Templates**: Go HTML templates

## License

MIT