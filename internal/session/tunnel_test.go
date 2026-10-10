package session

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoteForwardBusy(t *testing.T) {
	busy := fmt.Errorf("open clipboard forward: Error: remote port forwarding failed for listen port 2489")
	assert.True(t, remoteForwardBusy(busy))
	assert.False(t, remoteForwardBusy(errors.New("open clipboard forward: Permission denied")))
	assert.False(t, remoteForwardBusy(nil))
}
