package main

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIVersion(t *testing.T) {
	for _, test := range []struct {
		name    string
		ldflags string
		want    string
	}{
		{name: "default", want: "dev"},
		{name: "linker override", ldflags: "-X main.version=v1.2.3", want: "v1.2.3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "slush.exe")
			build := exec.CommandContext(t.Context(), "go", "build", "-ldflags", test.ldflags, "-o", binary, ".")
			output, err := build.CombinedOutput()
			require.NoError(t, err, "%s", output)

			output, err = exec.CommandContext(t.Context(), binary, "--version").CombinedOutput()
			require.NoError(t, err, "%s", output)
			assert.Equal(t, "slush version "+test.want+"\n", string(output))
		})
	}
}
