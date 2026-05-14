package markdown

import (
	"fmt"
	"math"
)

func meanPoolNormalized(vectors [][]float32) ([]float32, error) {
	if len(vectors) == 0 {
		return nil, nil
	}
	dim := len(vectors[0])
	if dim == 0 {
		return nil, nil
	}
	pooled := make([]float32, dim)
	for _, vector := range vectors {
		if len(vector) != dim {
			return nil, fmt.Errorf("markdown: tag vector dimension mismatch: expected=%d actual=%d", dim, len(vector))
		}
		for index, value := range vector {
			pooled[index] += value
		}
	}
	count := float32(len(vectors))
	var sumSquares float64
	for index := range pooled {
		pooled[index] /= count
		sumSquares += float64(pooled[index] * pooled[index])
	}
	if sumSquares == 0 {
		return pooled, nil
	}
	scale := float32(1 / math.Sqrt(sumSquares))
	for index := range pooled {
		pooled[index] *= scale
	}
	return pooled, nil
}
