package lancedb

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
)

func BenchmarkStoreSearchTagFusion(b *testing.B) {
	store := newBenchStore(b, 3, 512)
	baseQuery := domain.SearchQuery{
		Vector:    []float32{1, 0, 0},
		TagVector: []float32{0, 1, 0},
		Limit:     20,
		Tags:      []string{"go"},
	}

	b.Run("content-only", func(b *testing.B) {
		query := baseQuery
		query.UseTagFusion = false
		query.TagWeight = 0.35
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			results, err := store.Search(context.Background(), query)
			if err != nil {
				b.Fatalf("search: %v", err)
			}
			if len(results) == 0 {
				b.Fatal("expected results")
			}
		}
	})

	b.Run("fusion", func(b *testing.B) {
		query := baseQuery
		query.UseTagFusion = true
		query.TagWeight = 0.35
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			results, err := store.Search(context.Background(), query)
			if err != nil {
				b.Fatalf("search: %v", err)
			}
			if len(results) == 0 {
				b.Fatal("expected results")
			}
		}
	})
}

func newBenchStore(b *testing.B, vectorDim int, count int) *Store {
	b.Helper()

	vectorStore, err := New(domain.StoreConfig{
		RootDir:   b.TempDir(),
		Namespace: Name,
		Provider:  "ollama",
		Model:     "bge-m3",
		Splitter:  "markdown",
		VectorDim: vectorDim,
		DBVersion: "lancedb-v1",
		CreatedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		b.Fatalf("new store: %v", err)
	}
	store, ok := vectorStore.(*Store)
	if !ok {
		b.Fatal("expected *Store")
	}
	records := make([]domain.VectorRecord, 0, count)
	for i := 0; i < count; i++ {
		record := newVectorRecord(fmt.Sprintf("chunk-%d", i), fmt.Sprintf("docs/%03d.md", i), 0, []string{"go"}, benchVector(i, vectorDim))
		record.TagVector = benchTagVector(i, vectorDim)
		records = append(records, record)
	}
	if err := store.Upsert(context.Background(), records); err != nil {
		b.Fatalf("upsert: %v", err)
	}
	return store
}

func benchVector(i int, dim int) []float32 {
	vector := make([]float32, dim)
	vector[i%dim] = 1
	vector[(i+1)%dim] = 0.25
	return vector
}

func benchTagVector(i int, dim int) []float32 {
	vector := make([]float32, dim)
	vector[(i+1)%dim] = 1
	vector[(i+2)%dim] = 0.25
	return vector
}
