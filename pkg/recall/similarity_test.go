package recall

import (
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
		tolerance float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 1.0,
			tolerance: 0.001,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 0.0,
			tolerance: 0.001,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{-1, 0, 0},
			expected: -1.0,
			tolerance: 0.001,
		},
		{
			name:     "similar vectors",
			a:        []float32{1, 1, 0},
			b:        []float32{1, 0, 0},
			expected: float32(1 / math.Sqrt(2)),
			tolerance: 0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CosineSimilarity(tt.a, tt.b)
			if math.Abs(float64(result-tt.expected)) > float64(tt.tolerance) {
				t.Errorf("Expected %f, got %f", tt.expected, result)
			}
		})
	}
}

func TestPackUnpackEmbedding(t *testing.T) {
	original := []float32{0.1, 0.2, 0.3, -0.5, 1.0}

	packed := PackEmbedding(original)
	unpacked := UnpackEmbedding(packed)

	if len(unpacked) != len(original) {
		t.Errorf("Expected length %d, got %d", len(original), len(unpacked))
	}

	for i := range original {
		if math.Abs(float64(original[i]-unpacked[i])) > 0.0001 {
			t.Errorf("Index %d: expected %f, got %f", i, original[i], unpacked[i])
		}
	}
}

func TestCosineDistance(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}

	distance := CosineDistance(a, b)
	if math.Abs(float64(distance)) > 0.001 {
		t.Errorf("Expected distance 0, got %f", distance)
	}
}
