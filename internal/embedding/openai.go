package embedding

import (
	"context"
	"fmt"
	"sort"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// Compile-time interface check
var _ Embedder = (*OpenAI)(nil)

// EmbeddingsService defines the interface for making embedding API calls.
// This abstraction enables testing without calling the real OpenAI API.
type EmbeddingsService interface {
	New(ctx context.Context, params openai.EmbeddingNewParams, opts ...option.RequestOption) (*openai.CreateEmbeddingResponse, error)
}

// OpenAI implements the embedding service using OpenAI's API
type OpenAI struct {
	embeddings EmbeddingsService
	model      openai.EmbeddingModel
}

// NewOpenAI creates a new OpenAI embedding service
func NewOpenAI(apiKey, model string) *OpenAI {
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &OpenAI{
		embeddings: client.Embeddings,
		model:      openai.EmbeddingModel(model),
	}
}

// Embed generates an embedding for the given text
func (o *OpenAI) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := o.embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.F[openai.EmbeddingNewParamsInputUnion](
			openai.EmbeddingNewParamsInputArrayOfStrings([]string{text}),
		),
		Model: openai.F(o.model),
	})
	if err != nil {
		return nil, fmt.Errorf("embedding generation failed: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("embedding generation failed: no data returned")
	}

	// Convert float64 to float32
	embedding := make([]float32, len(resp.Data[0].Embedding))
	for i, v := range resp.Data[0].Embedding {
		embedding[i] = float32(v)
	}

	return embedding, nil
}

// EmbedBatch generates embeddings for multiple texts
func (o *OpenAI) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	resp, err := o.embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.F[openai.EmbeddingNewParamsInputUnion](
			openai.EmbeddingNewParamsInputArrayOfStrings(texts),
		),
		Model: openai.F(o.model),
	})
	if err != nil {
		return nil, fmt.Errorf("batch embedding generation failed: %w", err)
	}

	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("batch embedding generation failed: expected %d embeddings, got %d", len(texts), len(resp.Data))
	}

	// Sort by index to guarantee order matches input
	sort.Slice(resp.Data, func(i, j int) bool {
		return resp.Data[i].Index < resp.Data[j].Index
	})

	embeddings := make([][]float32, len(resp.Data))
	for i, data := range resp.Data {
		embedding := make([]float32, len(data.Embedding))
		for j, v := range data.Embedding {
			embedding[j] = float32(v)
		}
		embeddings[i] = embedding
	}

	return embeddings, nil
}

// ModelName returns the embedding model name
func (o *OpenAI) ModelName() string {
	return string(o.model)
}
