# Getting Started with Engram

Get from zero to a running Engram instance in under 10 minutes.

## Prerequisites

- **Go 1.23 or later** — [Download Go](https://go.dev/dl/)
- **OpenAI API key** — Required for embedding generation. [Get an API key](https://platform.openai.com/api-keys)

## Quick Start

### Option 1: Run from Source

1. **Clone the repository**

   ```bash
   git clone https://github.com/hyperengineering/engram.git
   cd engram
   ```

2. **Set environment variables**

   ```bash
   export OPENAI_API_KEY="sk-your-openai-api-key"
   export ENGRAM_API_KEY="your-secret-api-key"
   ```

   Choose a secure, random string for `ENGRAM_API_KEY`. This authenticates API clients.

3. **Build and run**

   ```bash
   make build
   ./dist/engram
   ```

   You should see:

   ```
   {"level":"info","msg":"Starting Engram","address":"0.0.0.0:8080"}
   ```

4. **Verify health**

   ```bash
   curl http://localhost:8080/api/v1/health
   ```

   Expected response:

   ```json
   {
     "status": "healthy",
     "version": "1.0.0",
     "embedding_model": "text-embedding-3-small",
     "lore_count": 0,
     "last_snapshot": null
   }
   ```

### Option 2: Run with Docker

1. **Build the Docker image**

   ```bash
   docker build -t engram .
   ```

2. **Run the container**

   ```bash
   docker run -p 8080:8080 \
     -e OPENAI_API_KEY="sk-your-openai-api-key" \
     -e ENGRAM_API_KEY="your-secret-api-key" \
     -v engram_data:/data \
     engram
   ```

3. **Verify**

   ```bash
   curl http://localhost:8080/api/v1/health
   ```

## First Run Verification

### Check Health

```bash
curl http://localhost:8080/api/v1/health
```

### Ingest Your First Lore Entry

```bash
curl -X POST http://localhost:8080/api/v1/lore \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "source_id": "getting-started-test",
    "lore": [
      {
        "content": "Rails migrations run in alphabetical order by timestamp prefix",
        "context": "Discovered while debugging migration order issue",
        "category": "DEPENDENCY_BEHAVIOR",
        "confidence": 0.85
      }
    ]
  }'
```

Expected response:

```json
{
  "accepted": 1,
  "merged": 0,
  "rejected": 0,
  "errors": []
}
```

### Verify Storage

Check the health endpoint again to confirm lore count increased:

```bash
curl http://localhost:8080/api/v1/health
```

You should see `"lore_count": 1`.

## Troubleshooting

### Missing API Key Error

**Symptom:** Service fails to start with `OPENAI_API_KEY is required` or `ENGRAM_API_KEY is required`.

**Solution:** Ensure both environment variables are set:

```bash
export OPENAI_API_KEY="sk-..."
export ENGRAM_API_KEY="your-secret-key"
```

### Port Already in Use

**Symptom:** `listen tcp :8080: bind: address already in use`

**Solution:** Either stop the process using port 8080, or change Engram's port:

```bash
export ENGRAM_PORT=3000
./dist/engram
```

### Database Permission Denied

**Symptom:** `failed to open database: permission denied`

**Solution:** Ensure the `data/` directory exists and is writable:

```bash
mkdir -p data
chmod 755 data
```

### Dev Mode (Skip API Key Validation)

For local development without an OpenAI key:

```bash
export ENGRAM_DEV_MODE=true
./dist/engram
```

Note: Embedding generation will fail in dev mode, but the service will start.

## Next Steps

- [Configuration Reference](configuration.md) — All configuration options
- [API Usage Guide](api-usage.md) — Practical examples and workflows
- [Error Reference](errors.md) — Error types and resolutions
