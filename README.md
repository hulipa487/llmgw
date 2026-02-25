# LLM Gateway (llmgw)

A lightweight LLM API gateway that proxies requests to upstream LLM providers. Supports both OpenAI and Anthropic compatible APIs.

## Features

- **Unified API**: Single endpoint for both OpenAI and Anthropic format requests
- **Multiple Upstreams**: Configure multiple upstream providers with load balancing
- **Model Aliasing**: Expose models under different names at the gateway level
- **Usage Tracking**: Track token usage and costs per user
- **Admin Panel**: Web UI for managing models, upstreams, and users
- **User Panel**: Web UI for users to manage API keys and view usage

## Quick Start

```bash
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
| Database | `LLMGW_DB` | `llmgw.db` |

Example `config.json`:
```json
{
    "host": "127.0.0.1",
    "port": 8080,
    "db": "llmgw.db"
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
- **Admin Panel**: `http://localhost:8080/admin/dashboard`

## Data Model

### Upstreams

An upstream is an LLM provider configuration:
- **ID**: Unique identifier
- **Name**: Display name
- **Base URL**: Provider API endpoint
- **OpenAI Path**: Path appended for OpenAI-format requests (e.g., `/v1`)
- **Anthropic Path**: Path appended for Anthropic-format requests (e.g., `/v1`)
- **API Key**: Provider API key

### Models

A model is a gateway-level model that can be mapped to one or more upstreams:
- **Name**: The name exposed at the gateway (e.g., `gpt-4`)
- **Upstreams**: Multiple upstream configurations with:
  - **Upstream**: Reference to an upstream provider
  - **Upstream Model Name**: The actual model name to send to the upstream (e.g., `gpt-4-turbo-preview`)

When a model has multiple upstreams, requests are load-balanced randomly across them.

## Workflow

1. **Admin**: Create upstreams (e.g., OpenAI, Anthropic, other providers)
2. **Admin**: Create models, mapping gateway names to upstream model names
3. **User**: Register and create API keys
4. **Client**: Make requests using the API key

## Example Setup

### 1. Login as Admin
Navigate to `/admin/login` and use the credentials printed on startup.

### 2. Create an Upstream
```
Name: OpenAI Main
Base URL: https://api.openai.com
OpenAI Path: /v1
Anthropic Path: /v1  (or your proxy path)
API Key: sk-xxx
```

### 3. Create a Model
```
Name: gpt-4
Upstreams:
  - Upstream: OpenAI Main
  - Upstream Model Name: gpt-4-turbo-preview
```

### 4. Create User and API Key
Navigate to `/login` to register a user, then create an API key in the user panel.

### 5. Make Requests
```bash
curl -X POST http://localhost:8080/openai/chat/completions \
  -H "Authorization: Bearer llmgw_xxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}'
```

## Load Balancing

When a model has multiple upstreams configured, the gateway randomly selects one for each request. This provides:
- **Failover**: If one provider is down, others can handle requests
- **Cost Optimization**: Route to cheaper providers when possible
- **Rate Limit Management**: Distribute load across multiple API keys

## License

MIT