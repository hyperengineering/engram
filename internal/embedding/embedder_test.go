package embedding

import (
	"context"
	"testing"
)

// mockEmbedder is a compile-time check that the Embedder interface can be implemented.
type mockEmbedder struct{}

var _ Embedder = (*mockEmbedder)(nil)

func (m *mockEmbedder) Embed(ctx context.Context, content string) ([]float32, error) {
	return nil, nil
}
func (m *mockEmbedder) EmbedBatch(ctx context.Context, contents []string) ([][]float32, error) {
	return nil, nil
}
func (m *mockEmbedder) ModelName() string {
	return ""
}

func TestEmbedderInterfaceSatisfaction(t *testing.T) {
	var _ Embedder = (*mockEmbedder)(nil)
}
