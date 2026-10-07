package remote

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoteEnvPrefix(t *testing.T) {
	prefix := EnvPrefix("tok123")
	assert.Contains(t, prefix, "SLUSH=1")
	assert.Contains(t, prefix, "SLUSH_TOKEN=tok123")
	assert.Contains(t, prefix, "BROWSER=slush-open")
	assert.Contains(t, prefix, "PATH=")
	assert.Contains(t, prefix, "/slush/bin")
	assert.True(t, strings.HasSuffix(prefix, "; "))
}
