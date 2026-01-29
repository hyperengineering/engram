---
story: "6.1"
title: "OpenAPI Specification"
status: implemented
designedBy: Clario
designedAt: "2026-01-29"
implementedBy: Spark
implementedAt: "2026-01-29"
frs: []
nfrs: []
---

# Technical Design: Story 6.1 - OpenAPI Specification

## 1. Story Summary

As a **Recall team integrating with Engram**, I want a formal OpenAPI specification documenting all API endpoints, so that I can generate client code, validate requests, and understand the complete API contract.

**Key Deliverables:**
- `docs/openapi.yaml` containing valid OpenAPI 3.0+ specification
- All 5 endpoints documented with request/response schemas
- RFC 7807 Problem Details error schema
- Bearer token authentication specification
- Validation constraints documented in schema

## 2. Gap Analysis

### Current State

| Artifact | Status | Notes |
|----------|--------|-------|
| API contracts | Implicit | Defined in code, story designs, architecture doc |
| OpenAPI spec | Missing | No formal specification exists |
| Schema definitions | Scattered | Types defined in `internal/types/types.go` |
| Error format | Documented | RFC 7807 in architecture.md |
| Authentication | Documented | Bearer token in architecture.md |

### Source Documents for Extraction

1. **Architecture Document** (`_bmad-output/planning-artifacts/architecture.md`)
   - API naming conventions (lines 229-236)
   - Data exchange formats (lines 289-297)
   - RFC 7807 error format (lines 272-287)
   - Authentication approach (lines 155-160)

2. **Type Definitions** (`internal/types/types.go`)
   - `LoreEntry`, `NewLoreEntry`
   - `IngestResult`, `DeltaResult`
   - `FeedbackEntry`, `FeedbackResult`, `FeedbackResultUpdate`
   - `StoreMetadata`, `StoreStats`
   - `HealthResponse`

3. **Validation Constraints** (`internal/validation/validation.go`)
   - Content max length: 4000 chars
   - Context max length: 1000 chars
   - Confidence range: 0.0-1.0
   - Valid categories enum
   - ULID format validation

4. **Handler Implementations** (`internal/api/handlers.go`)
   - Endpoint paths and methods
   - Request/response structures
   - Error response codes

## 3. OpenAPI Structure

### 3.1 Document Skeleton

```yaml
openapi: 3.0.3
info:
  title: Engram API
  description: Lore persistence and synchronization service for AI agent memory
  version: 1.0.0
  contact:
    name: Engram Team
  license:
    name: MIT

servers:
  - url: https://engram.fly.dev/api/v1
    description: Production
  - url: http://localhost:8080/api/v1
    description: Local development

tags:
  - name: Health
    description: Service health and status
  - name: Lore
    description: Lore ingestion and retrieval
  - name: Sync
    description: Snapshot and delta synchronization
  - name: Feedback
    description: Lore quality feedback

paths:
  # Endpoints defined below

components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      description: API key authentication

  schemas:
    # Schema definitions

  responses:
    # Common error responses

security:
  - bearerAuth: []
```

### 3.2 Endpoints to Document

| Endpoint | Method | Auth | Tag | Description |
|----------|--------|------|-----|-------------|
| `/health` | GET | No | Health | Service health and configuration |
| `/lore` | POST | Yes | Lore | Batch lore ingestion |
| `/lore/snapshot` | GET | Yes | Sync | Download full database snapshot |
| `/lore/delta` | GET | Yes | Sync | Incremental changes since timestamp |
| `/lore/feedback` | POST | Yes | Feedback | Submit lore quality feedback |

### 3.3 Schema Definitions

#### Core Types

```yaml
LoreEntry:
  type: object
  required: [id, content, category, confidence, source_ids, created_at, updated_at]
  properties:
    id:
      type: string
      pattern: '^[0-9A-HJKMNP-TV-Z]{26}$'
      description: ULID identifier
      example: "01ARZ3NDEKTSV4RRFFQ69G5FAV"
    content:
      type: string
      maxLength: 4000
      description: Lore content text
    context:
      type: string
      maxLength: 1000
      description: Additional context
    category:
      $ref: '#/components/schemas/Category'
    confidence:
      type: number
      format: float
      minimum: 0.0
      maximum: 1.0
      description: Confidence score
    embedding:
      type: string
      format: byte
      description: Base64-encoded float32 vector (1536 dimensions)
    source_ids:
      type: array
      items:
        type: string
      description: Source environment identifiers
    validation_count:
      type: integer
      minimum: 0
      description: Number of helpful feedback received
    last_validated_at:
      type: string
      format: date-time
      nullable: true
    created_at:
      type: string
      format: date-time
    updated_at:
      type: string
      format: date-time

NewLoreEntry:
  type: object
  required: [content, category, confidence]
  properties:
    content:
      type: string
      maxLength: 4000
      minLength: 1
    context:
      type: string
      maxLength: 1000
    category:
      $ref: '#/components/schemas/Category'
    confidence:
      type: number
      format: float
      minimum: 0.0
      maximum: 1.0

Category:
  type: string
  enum:
    - DEPENDENCY_BEHAVIOR
    - CODEBASE_PATTERN
    - USER_PREFERENCE
    - PROJECT_CONTEXT
    - DEBUGGING_INSIGHT
    - WORKFLOW_KNOWLEDGE
```

#### Request/Response Types

```yaml
IngestRequest:
  type: object
  required: [source_id, lore]
  properties:
    source_id:
      type: string
      minLength: 1
      description: Source environment identifier
    lore:
      type: array
      items:
        $ref: '#/components/schemas/NewLoreEntry'
      minItems: 1
      maxItems: 50

IngestResult:
  type: object
  properties:
    accepted:
      type: integer
      description: New entries created
    merged:
      type: integer
      description: Entries merged with existing
    rejected:
      type: integer
      description: Entries that failed validation
    errors:
      type: array
      items:
        $ref: '#/components/schemas/IngestError'

DeltaResult:
  type: object
  properties:
    lore:
      type: array
      items:
        $ref: '#/components/schemas/LoreEntry'
    deleted_ids:
      type: array
      items:
        type: string
    as_of:
      type: string
      format: date-time

FeedbackRequest:
  type: object
  required: [source_id, feedback]
  properties:
    source_id:
      type: string
      minLength: 1
    feedback:
      type: array
      items:
        $ref: '#/components/schemas/FeedbackEntry'
      minItems: 1
      maxItems: 50

FeedbackEntry:
  type: object
  required: [lore_id, type]
  properties:
    lore_id:
      type: string
      pattern: '^[0-9A-HJKMNP-TV-Z]{26}$'
    type:
      type: string
      enum: [helpful, not_relevant, incorrect]

FeedbackResult:
  type: object
  properties:
    updates:
      type: array
      items:
        $ref: '#/components/schemas/FeedbackResultUpdate'

FeedbackResultUpdate:
  type: object
  properties:
    lore_id:
      type: string
    previous_confidence:
      type: number
      format: float
    current_confidence:
      type: number
      format: float
    validation_count:
      type: integer
      description: Only present for helpful feedback

HealthResponse:
  type: object
  properties:
    status:
      type: string
      enum: [healthy]
    version:
      type: string
    embedding_model:
      type: string
    lore_count:
      type: integer
    last_snapshot:
      type: string
      format: date-time
      nullable: true
```

#### Error Types

```yaml
ProblemDetails:
  type: object
  required: [type, title, status]
  properties:
    type:
      type: string
      format: uri
      description: URI identifying the error type
      example: "https://engram.dev/errors/validation-error"
    title:
      type: string
      description: Human-readable error summary
      example: "Validation Error"
    status:
      type: integer
      description: HTTP status code
      example: 422
    detail:
      type: string
      description: Human-readable explanation
      example: "Content exceeds maximum length of 4000 characters"
    instance:
      type: string
      description: URI of the request that caused the error
      example: "/api/v1/lore"
    errors:
      type: array
      items:
        $ref: '#/components/schemas/ValidationError'
      description: Field-level validation errors (422 only)

ValidationError:
  type: object
  properties:
    field:
      type: string
      example: "lore[0].content"
    message:
      type: string
      example: "exceeds maximum length of 4000 characters"
```

### 3.4 Common Responses

```yaml
responses:
  Unauthorized:
    description: Missing or invalid authentication
    content:
      application/problem+json:
        schema:
          $ref: '#/components/schemas/ProblemDetails'
        example:
          type: "https://engram.dev/errors/unauthorized"
          title: "Unauthorized"
          status: 401
          detail: "Missing or invalid Authorization header"
          instance: "/api/v1/lore"

  ValidationError:
    description: Request validation failed
    content:
      application/problem+json:
        schema:
          $ref: '#/components/schemas/ProblemDetails'

  NotFound:
    description: Resource not found
    content:
      application/problem+json:
        schema:
          $ref: '#/components/schemas/ProblemDetails'

  ServiceUnavailable:
    description: Service temporarily unavailable
    content:
      application/problem+json:
        schema:
          $ref: '#/components/schemas/ProblemDetails'
    headers:
      Retry-After:
        schema:
          type: integer
        description: Seconds to wait before retrying
```

## 4. Endpoint Specifications

### 4.1 Health Endpoint

```yaml
/health:
  get:
    tags: [Health]
    summary: Check service health
    description: Returns service status, version, and configuration. No authentication required.
    operationId: getHealth
    security: []  # No auth required
    responses:
      '200':
        description: Service is healthy
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/HealthResponse'
            example:
              status: "healthy"
              version: "1.0.0"
              embedding_model: "text-embedding-3-small"
              lore_count: 1523
              last_snapshot: "2026-01-29T14:30:00Z"
```

### 4.2 Lore Ingest Endpoint

```yaml
/lore:
  post:
    tags: [Lore]
    summary: Ingest lore entries
    description: |
      Submit a batch of lore entries for persistence. Entries are validated,
      embedded, deduplicated, and stored. Semantically equivalent entries
      are merged rather than duplicated.
    operationId: ingestLore
    requestBody:
      required: true
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/IngestRequest'
    responses:
      '200':
        description: Batch processed successfully
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/IngestResult'
      '401':
        $ref: '#/components/responses/Unauthorized'
      '422':
        $ref: '#/components/responses/ValidationError'
```

### 4.3 Snapshot Endpoint

```yaml
/lore/snapshot:
  get:
    tags: [Sync]
    summary: Download full snapshot
    description: |
      Download a complete database snapshot for initial bootstrap.
      Includes all lore entries with embeddings for client-side semantic search.
    operationId: getSnapshot
    responses:
      '200':
        description: Snapshot file
        content:
          application/octet-stream:
            schema:
              type: string
              format: binary
      '401':
        $ref: '#/components/responses/Unauthorized'
      '503':
        $ref: '#/components/responses/ServiceUnavailable'
```

### 4.4 Delta Sync Endpoint

```yaml
/lore/delta:
  get:
    tags: [Sync]
    summary: Get incremental changes
    description: |
      Retrieve lore changes since a given timestamp. Returns new/updated
      entries and IDs of deleted entries.
    operationId: getDelta
    parameters:
      - name: since
        in: query
        required: true
        schema:
          type: string
          format: date-time
        description: ISO 8601 timestamp to get changes from
        example: "2026-01-28T00:00:00Z"
    responses:
      '200':
        description: Delta changes
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/DeltaResult'
      '400':
        description: Invalid timestamp format
        content:
          application/problem+json:
            schema:
              $ref: '#/components/schemas/ProblemDetails'
      '401':
        $ref: '#/components/responses/Unauthorized'
```

### 4.5 Feedback Endpoint

```yaml
/lore/feedback:
  post:
    tags: [Feedback]
    summary: Submit lore feedback
    description: |
      Submit batch feedback on lore entries. Feedback types:
      - `helpful`: +0.08 confidence, increments validation_count
      - `incorrect`: -0.15 confidence
      - `not_relevant`: no change

      Confidence is capped at 1.0 and floored at 0.0.
    operationId: submitFeedback
    requestBody:
      required: true
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/FeedbackRequest'
    responses:
      '200':
        description: Feedback processed
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/FeedbackResult'
      '401':
        $ref: '#/components/responses/Unauthorized'
      '404':
        $ref: '#/components/responses/NotFound'
      '422':
        $ref: '#/components/responses/ValidationError'
```

## 5. Validation Requirements

### 5.1 OpenAPI Linting

Use `spectral` or `openapi-generator` to validate:

```bash
# Install spectral
npm install -g @stoplight/spectral-cli

# Validate specification
spectral lint docs/openapi.yaml
```

**Required checks:**
- Valid OpenAPI 3.0+ syntax
- All $ref references resolve
- All required fields present
- Examples validate against schemas
- Security schemes properly applied

### 5.2 Code Generation Test

Verify spec can generate valid client code:

```bash
# Generate Go client
openapi-generator generate -i docs/openapi.yaml -g go -o /tmp/engram-client-go

# Generate TypeScript client
openapi-generator generate -i docs/openapi.yaml -g typescript-fetch -o /tmp/engram-client-ts
```

## 6. Implementation Checklist

- [x] Create `docs/openapi.yaml` with document skeleton
- [x] Define all component schemas from types.go
- [x] Define ProblemDetails error schema per RFC 7807
- [x] Document `/health` endpoint (no auth)
- [x] Document `/lore` POST endpoint
- [x] Document `/lore/snapshot` GET endpoint
- [x] Document `/lore/delta` GET endpoint
- [x] Document `/lore/feedback` POST endpoint
- [x] Add security scheme (Bearer token)
- [x] Add validation constraints (maxLength, minimum, maximum, pattern)
- [x] Add examples for all request/response schemas
- [x] Validate with spectral lint
- [ ] Test Go client generation (optional - tool not available in environment)
- [ ] Test TypeScript client generation (optional - tool not available in environment)

## 7. Design Decisions

### D1: OpenAPI 3.0.3 Version

**Decision:** Use OpenAPI 3.0.3 (not 3.1.x).

**Rationale:** Broader tooling support. OpenAPI 3.1 introduced JSON Schema alignment but many tools still have incomplete support.

### D2: Inline vs $ref Schemas

**Decision:** Define all schemas in `components/schemas` and reference via `$ref`.

**Rationale:** Reusability, cleaner endpoint definitions, better generated code.

### D3: Error Content-Type

**Decision:** Use `application/problem+json` for error responses.

**Rationale:** RFC 7807 specifies this media type for Problem Details. Signals to clients that the response follows the standard.

---

## 8. Post-Implementation Notes

### Implementation Journey

**What was the plan?**
- Create OpenAPI 3.0.3 specification documenting all 5 endpoints
- Include all schemas, error formats, and validation constraints
- Validate with linting tools

**What actually happened?**
- Created comprehensive 646-line OpenAPI specification
- Verified against actual `types.go` implementation (not Clario's design which had different category names)
- Categories corrected to match actual implementation: ARCHITECTURAL_DECISION, PATTERN_OUTCOME, INTERFACE_LESSON, EDGE_CASE_DISCOVERY, IMPLEMENTATION_FRICTION, TESTING_STRATEGY, DEPENDENCY_BEHAVIOR, PERFORMANCE_INSIGHT

**Deviations from design:**
- Clario's design had different category enum values (CODEBASE_PATTERN, USER_PREFERENCE, etc.) - spec uses actual implementation values
- Client code generation tests deferred (openapi-generator not available in environment)

### Specification Contents

| Section | Description |
|---------|-------------|
| Info | Title, description, version, license |
| Servers | Production (fly.dev) and local development |
| Tags | Health, Lore, Sync, Feedback |
| Paths | 5 endpoints fully documented |
| Schemas | 16 schema definitions including RFC 7807 errors |
| Security | Bearer token authentication |

### Files Created/Modified

- `docs/openapi.yaml` - 646 lines, OpenAPI 3.0.3 specification
- `.spectral.yaml` - Spectral linter configuration extending oas ruleset
- `Makefile` - Added `lint-openapi` target for OpenAPI validation

### Linting Validation

Added `make lint-openapi` target that runs `npx @stoplight/spectral-cli lint docs/openapi.yaml`.
OpenAPI spec passes all spectral OAS validation rules with no errors or warnings.

---

**Built and documented. Ready for your ruthless review.**
