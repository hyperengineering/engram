# Error Reference

Engram returns errors in [RFC 7807 Problem Details](https://www.rfc-editor.org/rfc/rfc7807) format.

## Error Response Format

All error responses include these fields:

| Field | Type | Description |
|-------|------|-------------|
| `type` | string (URI) | Unique identifier for the error type |
| `title` | string | Human-readable summary |
| `status` | integer | HTTP status code |
| `detail` | string | Detailed explanation of the error |
| `instance` | string | Request path that caused the error |

**Content-Type:** `application/problem+json`

## Error Types

### Unauthorized (401)

**Type URI:** `https://engram.dev/errors/unauthorized`

The request is missing authentication or the provided credentials are invalid.

**Causes:**

- Missing `Authorization` header
- Invalid Bearer token format
- Incorrect API key

**Resolution:**

- Verify your API key is correct
- Ensure header format: `Authorization: Bearer YOUR_API_KEY`
- Check that the API key matches the `ENGRAM_API_KEY` environment variable on the server

**Example Response:**

```json
{
  "type": "https://engram.dev/errors/unauthorized",
  "title": "Unauthorized",
  "status": 401,
  "detail": "Missing or invalid API key",
  "instance": "/api/v1/lore"
}
```

---

### Bad Request (400)

**Type URI:** `https://engram.dev/errors/bad-request`

The request is malformed or contains invalid data that cannot be processed.

**Causes:**

- Malformed JSON in request body
- Invalid timestamp format in query parameters
- Missing required query parameters

**Resolution:**

- Verify JSON syntax is valid
- Use RFC 3339 format for timestamps (e.g., `2026-01-29T10:00:00Z`)
- Include all required parameters

**Example Response:**

```json
{
  "type": "https://engram.dev/errors/bad-request",
  "title": "Bad Request",
  "status": 400,
  "detail": "Invalid JSON: unexpected end of JSON input",
  "instance": "/api/v1/lore"
}
```

**Example (Invalid Timestamp):**

```json
{
  "type": "https://engram.dev/errors/bad-request",
  "title": "Bad Request",
  "status": 400,
  "detail": "Invalid since timestamp: must be RFC3339 format (e.g., 2026-01-29T10:00:00Z)",
  "instance": "/api/v1/lore/delta"
}
```

**Example (Missing Parameter):**

```json
{
  "type": "https://engram.dev/errors/bad-request",
  "title": "Bad Request",
  "status": 400,
  "detail": "Missing required query parameter: since",
  "instance": "/api/v1/lore/delta"
}
```

---

### Not Found (404)

**Type URI:** `https://engram.dev/errors/not-found`

The requested resource does not exist.

**Causes:**

- Lore ID doesn't exist in the database
- Deleted lore entry

**Resolution:**

- Verify the lore ID is correct
- Check if the lore was deleted via delta sync
- Use delta sync to get current lore IDs

**Example Response:**

```json
{
  "type": "https://engram.dev/errors/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "Resource not found",
  "instance": "/api/v1/lore/feedback"
}
```

---

### Validation Error (422)

**Type URI:** `https://engram.dev/errors/validation-error`

The request contains fields that fail validation rules.

**Extended Format:**

Validation errors include an additional `errors` array with specific field errors:

```json
{
  "type": "https://engram.dev/errors/validation-error",
  "title": "Validation Error",
  "status": 422,
  "detail": "Request contains invalid fields",
  "instance": "/api/v1/lore",
  "errors": [
    {"field": "source_id", "message": "is required"},
    {"field": "lore[0].content", "message": "exceeds maximum length of 4000 characters"}
  ]
}
```

**Common Validation Failures:**

| Field | Rule | Error Message |
|-------|------|---------------|
| `source_id` | Required | `is required` |
| `lore` | Required, 1-50 items | `is required and must not be empty` |
| `lore` | Max 50 items | `exceeds maximum batch size of 50` |
| `lore[n].content` | Required | `is required` |
| `lore[n].content` | Max 4000 chars | `exceeds maximum length of 4000 characters` |
| `lore[n].content` | Valid UTF-8 | `must be valid UTF-8` |
| `lore[n].content` | No null bytes | `must not contain null bytes` |
| `lore[n].context` | Max 1000 chars | `exceeds maximum length of 1000 characters` |
| `lore[n].category` | Required | `is required` |
| `lore[n].category` | Valid enum | `must be one of: ARCHITECTURAL_DECISION, PATTERN_OUTCOME, ...` |
| `lore[n].confidence` | Range 0.0-1.0 | `must be between 0.0 and 1.0` |
| `feedback[n].lore_id` | Required | `is required` |
| `feedback[n].lore_id` | Valid ULID | `must be a valid ULID (26 characters)` |
| `feedback[n].type` | Required | `is required` |
| `feedback[n].type` | Valid enum | `must be one of: helpful, not_relevant, incorrect` |

**Valid Lore Categories:**

- `ARCHITECTURAL_DECISION`
- `PATTERN_OUTCOME`
- `INTERFACE_LESSON`
- `EDGE_CASE_DISCOVERY`
- `IMPLEMENTATION_FRICTION`
- `TESTING_STRATEGY`
- `DEPENDENCY_BEHAVIOR`
- `PERFORMANCE_INSIGHT`

**Valid Feedback Types:**

- `helpful`
- `not_relevant`
- `incorrect`

**Resolution:**

- Check the `errors` array for specific field failures
- Fix each field according to the validation rules
- Resubmit the request

---

### Conflict (409)

**Type URI:** `https://engram.dev/errors/conflict`

The request conflicts with the current state of the resource.

**Causes:**

- Attempting to create a duplicate entry when deduplication is disabled

**Resolution:**

- Check if the lore already exists
- Use the existing entry or modify the content

**Example Response:**

```json
{
  "type": "https://engram.dev/errors/conflict",
  "title": "Conflict",
  "status": 409,
  "detail": "Duplicate entry",
  "instance": "/api/v1/lore"
}
```

---

### Internal Server Error (500)

**Type URI:** `https://engram.dev/errors/internal-error`

An unexpected error occurred on the server.

**Causes:**

- Database errors
- Embedding service failures (after retries)
- Unexpected runtime errors

**Resolution:**

- Retry the request after a short delay
- Check server logs for details
- Contact support if the error persists

**Example Response:**

```json
{
  "type": "https://engram.dev/errors/internal-error",
  "title": "Internal Server Error",
  "status": 500,
  "detail": "Internal Server Error",
  "instance": "/api/v1/lore"
}
```

**Note:** Internal error details are intentionally hidden to prevent information leakage.

---

### Service Unavailable (503)

**Type URI:** `https://engram.dev/errors/service-unavailable`

The service is temporarily unable to handle the request.

**Causes:**

- Snapshot not yet generated (first startup)
- Snapshot generation in progress
- Embedding service unavailable

**Resolution:**

- Check the `Retry-After` header for recommended wait time
- Retry after the indicated interval
- For snapshot requests, wait for initial snapshot generation

**Example Response:**

```json
{
  "type": "https://engram.dev/errors/service-unavailable",
  "title": "Service Unavailable",
  "status": 503,
  "detail": "Snapshot not yet available. Please retry after the indicated interval.",
  "instance": "/api/v1/lore/snapshot"
}
```

**Response Headers:**

```
Retry-After: 60
```

---

## HTTP Status Code Summary

| Code | Type | When Returned |
|------|------|---------------|
| 200 | Success | All successful requests |
| 400 | Bad Request | Malformed JSON, invalid parameters |
| 401 | Unauthorized | Missing or invalid API key |
| 404 | Not Found | Lore ID doesn't exist |
| 409 | Conflict | Duplicate entry conflict |
| 422 | Validation Error | Field validation failures |
| 500 | Internal Error | Unexpected server errors |
| 503 | Service Unavailable | Snapshot not ready, embedding unavailable |

## Handling Errors in Code

### Check for Problem Details

```python
import requests

response = requests.post(url, json=data, headers=headers)

if response.status_code >= 400:
    problem = response.json()
    print(f"Error: {problem['title']}")
    print(f"Detail: {problem['detail']}")

    # For validation errors, check specific field errors
    if response.status_code == 422 and 'errors' in problem:
        for error in problem['errors']:
            print(f"  {error['field']}: {error['message']}")
```

### Retry Logic for Transient Errors

```python
import time

def make_request_with_retry(url, headers, max_retries=3):
    for attempt in range(max_retries):
        response = requests.get(url, headers=headers)

        if response.status_code == 503:
            retry_after = int(response.headers.get('Retry-After', 60))
            time.sleep(retry_after)
            continue

        if response.status_code == 500:
            time.sleep(2 ** attempt)  # Exponential backoff
            continue

        return response

    raise Exception("Max retries exceeded")
```
