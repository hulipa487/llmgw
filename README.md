# LLM Gateway (llmgw)

A lightweight LLM API gateway that proxies requests to upstream LLM providers with OpenAI and Anthropic compatible APIs.

## Features

- **Unified API**: Single endpoint for both OpenAI and Anthropic format requests
- **Multiple Upstreams**: Configure multiple upstream providers with load balancing
- **Model Aliasing**: Expose models under different names at the gateway level
- **Invite-Only Registration**: Users require invite codes to register
- **Rate Limiting**: 800 requests per 6-hour window, 9600 requests per month
- **Usage Tracking**: Track token usage per user and model
- **Admin Panel**: Web UI for managing models, upstreams, invite codes, and users
- **User Panel**: Web UI for API key management and usage monitoring

## Quick Start

```bash
# Set DATABASE_URL (PostgreSQL connection string)
export DATABASE_URL="postgres://user:password@localhost:5432/llmgw?sslmode=disable"

# Build
go build -o llmgw .

# Run
./llmgw
```

On first run, admin credentials will be printed to the console. Save these!

## Configuration

Configuration can be set via `config.json` or environment variables:

| Setting | Env Variable | Default |
|---------|-------------|---------|
| Host | `LLMGW_HOST` | `127.0.0.1` |
| Port | `LLMGW_PORT` | `8080` |
| Database | `DATABASE_URL` | Required |

Example `config.json`:
```json
{
    "host": "127.0.0.1",
    "port": 8080,
    "db": "postgres://user:password@localhost:5432/llmgw?sslmode=disable"
}
```

## API Endpoints

### OpenAI-Compatible (`/openai/*`)

```bash
# Chat completions
curl -X POST http://localhost:8080/openai/chat/completions \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}'

# List models
curl http://localhost:8080/openai/models \
  -H "Authorization: Bearer YOUR_API_KEY"
```

### Anthropic-Compatible (`/anthropic/v1/*`)

```bash
# Messages
curl -X POST http://localhost:8080/anthropic/v1/messages \
  -H "x-api-key: YOUR_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-3-opus","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}'

# List models
curl http://localhost:8080/anthropic/v1/models \
  -H "x-api-key: YOUR_API_KEY"
```

## Web Interface

- **User Panel**: `http://localhost:8080/user/dashboard`
- **Admin Panel**: `http://localhost:8080/admin` (redirects to login)

## User Rate Limits

- **6-Hour Window**: 800 requests
- **Monthly Limit**: 9,600 requests
- **API Keys**: Maximum 10 active keys per user

## Data Model

### Upstreams
An upstream is an LLM provider configuration:
- **ID**: Unique identifier
- **Name**: Display name
- **Base URL**: Provider API endpoint
- **OpenAI Path**: Path for OpenAI-format requests (e.g., `/v1`)
- **Anthropic Path**: Path for Anthropic-format requests (e.g., `/v1`)
- **API Key**: Provider API key

### Models
A model is a gateway-level model that can be mapped to one or more upstreams:
- **Name**: The name exposed at the gateway (e.g., `gpt-4`)
- **Upstreams**: Multiple upstream configurations with:
  - **Upstream**: Reference to an upstream provider
  - **Upstream Model Name**: The actual model name sent to the upstream

When a model has multiple upstreams, requests are load-balanced randomly.

## Workflow

1. **Admin**: Login at `/admin`, generate invite codes
2. **Admin**: Create upstreams (e.g., OpenAI, Anthropic providers)
3. **Admin**: Create models, mapping gateway names to upstream model names
4. **User**: Register with invite code, create API keys
5. **Client**: Make requests using the API key

## Example Setup

### 1. Admin Login
Navigate to `/admin` and use the credentials printed on startup.

### 2. Generate Invite Codes
In the admin panel, go to "Invites" and generate codes.

### 3. Create an Upstream
```
ID: openai-main
Name: OpenAI Main
Base URL: https://api.openai.com
OpenAI Path: /v1
Anthropic Path: /v1
API Key: sk-xxx
```

### 4. Create a Model
```
Name: gpt-4
Upstreams:
  - Upstream: OpenAI Main
  - Upstream Model Name: gpt-4-turbo-preview
```

### 5. User Registration
Navigate to `/register` with an invite code.

### 6. Create API Key
In the user panel, generate an API key.

### 7. Make Requests
```bash
curl -X POST http://localhost:8080/openai/chat/completions \
  -H "Authorization: Bearer llmgw_xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}'
```

## Tech Stack

- **Framework**: Gin
- **Database**: PostgreSQL with GORM
- **Auth**: Session cookies (web), API keys (API)
- **Templates**: Go HTML templates