//go:build integration

// Integration gate contract:
// - Default `go test ./...` MUST NOT require external services.
// - Run integration coverage only with: `go test -tags=integration ./...`
// - Future end-to-end tests can additionally gate on $INTEGRATION=1 if needed.
package testkit

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntegrationGate(t *testing.T) {
	require.Equal(t, "1", os.Getenv("INTEGRATION"), "integration tests are controlled by build tag plus env gate")
}
