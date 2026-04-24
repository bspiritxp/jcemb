package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewBootstrap(t *testing.T) {
	bootstrap := NewBootstrap()

	require.Equal(t, "bootstrap", bootstrap.Name)
}
