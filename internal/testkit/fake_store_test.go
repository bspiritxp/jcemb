package testkit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFakeStore(t *testing.T) {
	store := NewFakeStore()
	store.Put("k", "v")

	value, ok := store.Get("k")
	require.True(t, ok)
	require.Equal(t, "v", value)
}
