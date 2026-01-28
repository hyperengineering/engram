package embedding

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// Compile-time interface check for OpenAI
var _ Embedder = (*OpenAI)(nil)

// mockEmbeddingsService implements EmbeddingsService for testing
type mockEmbeddingsService struct {
	response *openai.CreateEmbeddingResponse
	err      error
	// Track calls for verification
	callCount int
	lastInput []string
}

func (m *mockEmbeddingsService) New(ctx context.Context, params openai.EmbeddingNewParams, opts ...option.RequestOption) (*openai.CreateEmbeddingResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.callCount++

	// Extract input strings for verification
	if params.Input.Value != nil {
		if arr, ok := params.Input.Value.(openai.EmbeddingNewParamsInputArrayOfStrings); ok {
			m.lastInput = []string(arr)
		}
	}

	return m.response, m.err
}

// Helper to create a mock embedding response
func createMockResponse(embeddings [][]float64) *openai.CreateEmbeddingResponse {
	data := make([]openai.Embedding, len(embeddings))
	for i, emb := range embeddings {
		data[i] = openai.Embedding{
			Embedding: emb,
			Index:     int64(i),
		}
	}
	return &openai.CreateEmbeddingResponse{
		Data: data,
	}
}

// Helper to create a mock embedding response with custom indices (for order testing)
func createMockResponseWithIndices(embeddings [][]float64, indices []int64) *openai.CreateEmbeddingResponse {
	data := make([]openai.Embedding, len(embeddings))
	for i, emb := range embeddings {
		data[i] = openai.Embedding{
			Embedding: emb,
			Index:     indices[i],
		}
	}
	return &openai.CreateEmbeddingResponse{
		Data: data,
	}
}

// Helper to create a 1536-dimension embedding
func create1536DimEmbedding() []float64 {
	emb := make([]float64, 1536)
	for i := range emb {
		emb[i] = float64(i) * 0.001
	}
	return emb
}

// TestOpenAI_ImplementsEmbedder verifies compile-time interface satisfaction
func TestOpenAI_ImplementsEmbedder(t *testing.T) {
	// This is checked at compile time by the var declaration above
	// This test documents the requirement explicitly
	var _ Embedder = (*OpenAI)(nil)
}

// TestEmbed_Returns1536Dimensions verifies embedding dimension
func TestEmbed_Returns1536Dimensions(t *testing.T) {
	mock := &mockEmbeddingsService{
		response: createMockResponse([][]float64{create1536DimEmbedding()}),
	}

	client := &OpenAI{
		embeddings: mock,
		model:      openai.EmbeddingModelTextEmbedding3Small,
	}

	result, err := client.Embed(context.Background(), "test content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1536 {
		t.Errorf("expected 1536 dimensions, got %d", len(result))
	}
}

// TestEmbed_ConvertsFloat64ToFloat32 verifies type conversion
func TestEmbed_ConvertsFloat64ToFloat32(t *testing.T) {
	embedding := []float64{0.1, 0.2, 0.3, 0.4, 0.5}
	mock := &mockEmbeddingsService{
		response: createMockResponse([][]float64{embedding}),
	}

	client := &OpenAI{
		embeddings: mock,
		model:      openai.EmbeddingModelTextEmbedding3Small,
	}

	result, err := client.Embed(context.Background(), "test content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, v := range embedding {
		if result[i] != float32(v) {
			t.Errorf("index %d: expected %f, got %f", i, float32(v), result[i])
		}
	}
}

// TestEmbed_WrapsErrorWithContext verifies error wrapping
func TestEmbed_WrapsErrorWithContext(t *testing.T) {
	originalErr := errors.New("api error")
	mock := &mockEmbeddingsService{
		err: originalErr,
	}

	client := &OpenAI{
		embeddings: mock,
		model:      openai.EmbeddingModelTextEmbedding3Small,
	}

	_, err := client.Embed(context.Background(), "test content")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "embedding generation failed") {
		t.Errorf("error should contain 'embedding generation failed', got: %v", err)
	}

	if !errors.Is(err, originalErr) {
		t.Errorf("error should wrap original error")
	}
}

// TestEmbed_NoDataReturned verifies error when API returns empty data
func TestEmbed_NoDataReturned(t *testing.T) {
	mock := &mockEmbeddingsService{
		response: &openai.CreateEmbeddingResponse{
			Data: []openai.Embedding{},
		},
	}

	client := &OpenAI{
		embeddings: mock,
		model:      openai.EmbeddingModelTextEmbedding3Small,
	}

	_, err := client.Embed(context.Background(), "test content")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "embedding generation failed") {
		t.Errorf("error should contain 'embedding generation failed', got: %v", err)
	}
}

// TestEmbedBatch_ReturnsEmbeddingsInOrder verifies order preservation
func TestEmbedBatch_ReturnsEmbeddingsInOrder(t *testing.T) {
	// Create embeddings that will be returned out of order by mock
	emb0 := []float64{0.0, 0.0, 0.0}
	emb1 := []float64{1.0, 1.0, 1.0}
	emb2 := []float64{2.0, 2.0, 2.0}

	// Return embeddings in reverse order (2, 1, 0) but with correct indices
	mock := &mockEmbeddingsService{
		response: createMockResponseWithIndices(
			[][]float64{emb2, emb1, emb0},
			[]int64{2, 1, 0},
		),
	}

	client := &OpenAI{
		embeddings: mock,
		model:      openai.EmbeddingModelTextEmbedding3Small,
	}

	result, err := client.EmbedBatch(context.Background(), []string{"text0", "text1", "text2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify order: result[0] should be emb0 (index 0)
	if result[0][0] != float32(0.0) {
		t.Errorf("expected first embedding to be emb0, got %v", result[0])
	}
	if result[1][0] != float32(1.0) {
		t.Errorf("expected second embedding to be emb1, got %v", result[1])
	}
	if result[2][0] != float32(2.0) {
		t.Errorf("expected third embedding to be emb2, got %v", result[2])
	}
}

// TestEmbedBatch_HandlesUpTo50Items verifies batch processing
func TestEmbedBatch_HandlesUpTo50Items(t *testing.T) {
	embeddings := make([][]float64, 50)
	for i := range embeddings {
		embeddings[i] = []float64{float64(i)}
	}

	mock := &mockEmbeddingsService{
		response: createMockResponse(embeddings),
	}

	client := &OpenAI{
		embeddings: mock,
		model:      openai.EmbeddingModelTextEmbedding3Small,
	}

	inputs := make([]string, 50)
	for i := range inputs {
		inputs[i] = "text"
	}

	result, err := client.EmbedBatch(context.Background(), inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 50 {
		t.Errorf("expected 50 embeddings, got %d", len(result))
	}

	// Verify single API call
	if mock.callCount != 1 {
		t.Errorf("expected 1 API call, got %d", mock.callCount)
	}
}

// TestEmbedBatch_EmptyInput verifies early return for empty slice
func TestEmbedBatch_EmptyInput(t *testing.T) {
	mock := &mockEmbeddingsService{}

	client := &OpenAI{
		embeddings: mock,
		model:      openai.EmbeddingModelTextEmbedding3Small,
	}

	result, err := client.EmbedBatch(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Error("expected empty slice, got nil")
	}

	if len(result) != 0 {
		t.Errorf("expected 0 embeddings, got %d", len(result))
	}

	// Verify no API call was made
	if mock.callCount != 0 {
		t.Errorf("expected no API calls for empty input, got %d", mock.callCount)
	}
}

// TestEmbedBatch_WrapsErrorWithContext verifies error wrapping
func TestEmbedBatch_WrapsErrorWithContext(t *testing.T) {
	originalErr := errors.New("api error")
	mock := &mockEmbeddingsService{
		err: originalErr,
	}

	client := &OpenAI{
		embeddings: mock,
		model:      openai.EmbeddingModelTextEmbedding3Small,
	}

	_, err := client.EmbedBatch(context.Background(), []string{"text"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "batch embedding generation failed") {
		t.Errorf("error should contain 'batch embedding generation failed', got: %v", err)
	}

	if !errors.Is(err, originalErr) {
		t.Errorf("error should wrap original error")
	}
}

// TestEmbedBatch_MismatchedCount verifies error when response count doesn't match input
func TestEmbedBatch_MismatchedCount(t *testing.T) {
	// Return 2 embeddings for 3 inputs
	mock := &mockEmbeddingsService{
		response: createMockResponse([][]float64{
			{0.1}, {0.2},
		}),
	}

	client := &OpenAI{
		embeddings: mock,
		model:      openai.EmbeddingModelTextEmbedding3Small,
	}

	_, err := client.EmbedBatch(context.Background(), []string{"a", "b", "c"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "expected 3 embeddings") {
		t.Errorf("error should mention expected count, got: %v", err)
	}
}

// TestModelName_ReturnsConfiguredModel verifies model name getter
func TestModelName_ReturnsConfiguredModel(t *testing.T) {
	client := &OpenAI{
		model: openai.EmbeddingModelTextEmbedding3Small,
	}

	name := client.ModelName()
	if name != string(openai.EmbeddingModelTextEmbedding3Small) {
		t.Errorf("expected %s, got %s", openai.EmbeddingModelTextEmbedding3Small, name)
	}
}

// TestEmbed_RespectsContextCancellation verifies context propagation
func TestEmbed_RespectsContextCancellation(t *testing.T) {
	mock := &mockEmbeddingsService{
		response: createMockResponse([][]float64{create1536DimEmbedding()}),
	}

	client := &OpenAI{
		embeddings: mock,
		model:      openai.EmbeddingModelTextEmbedding3Small,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.Embed(ctx, "test content")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

// TestEmbedBatch_RespectsContextCancellation verifies context propagation
func TestEmbedBatch_RespectsContextCancellation(t *testing.T) {
	mock := &mockEmbeddingsService{
		response: createMockResponse([][]float64{{0.1}}),
	}

	client := &OpenAI{
		embeddings: mock,
		model:      openai.EmbeddingModelTextEmbedding3Small,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.EmbedBatch(ctx, []string{"text"})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

