package embedding

import (
	"context"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// OpenAI implements the embedding service using OpenAI's API
type OpenAI struct {
	client *openai.Client
	model  openai.EmbeddingModel
}

// NewOpenAI creates a new OpenAI embedding service
func NewOpenAI(apiKey, model string) *OpenAI {
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &OpenAI{
		client: client,
		model:  openai.EmbeddingModel(model),
	}
}

// Embed generates an embedding for the given text
func (o *OpenAI) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := o.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.F[openai.EmbeddingNewParamsInputUnion](
			openai.EmbeddingNewParamsInputArrayOfStrings([]string{text}),
		),
		Model: openai.F(o.model),
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("no embedding data returned")
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
	resp, err := o.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.F[openai.EmbeddingNewParamsInputUnion](
			openai.EmbeddingNewParamsInputArrayOfStrings(texts),
		),
		Model: openai.F(o.model),
	})
	if err != nil {
		return nil, err
	}

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
