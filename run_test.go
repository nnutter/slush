package main

import (
	"errors"
	"os/exec"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExitCode(t *testing.T) {
	assert.Equal(t, 0, exitCode(nil))
	assert.Equal(t, 1, exitCode(errors.New("boom")))

	var fail *exec.Cmd
	if runtime.GOOS == "windows" {
		fail = exec.Command("cmd", "/c", "exit 7")
	} else {
		fail = exec.Command("false")
	}
	err := fail.Run()
	require.Error(t, err)

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	if runtime.GOOS == "windows" {
		assert.Equal(t, 7, exitCode(err))
		return
	}
	assert.Equal(t, 1, exitCode(err))
}
