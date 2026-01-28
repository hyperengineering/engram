package embedding

import "context"

// Embedder defines the interface contract for embedding generation services.
type Embedder interface {
	Embed(ctx context.Context, content string) ([]float32, error)
	EmbedBatch(ctx context.Context, contents []string) ([][]float32, error)
	ModelName() string
}
