package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithReverseTunnel(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		flag string
		want []string
	}{
		{
			name: "injects when missing",
			in:   []string{"user@host"},
			flag: "-R",
			want: []string{"-R", reverseTunnel, "user@host"},
		},
		{
			name: "skips when separate -R already present",
			in:   []string{"-R", reverseTunnel, "user@host"},
			flag: "-R",
			want: []string{"-R", reverseTunnel, "user@host"},
		},
		{
			name: "skips when combined -R already present",
			in:   []string{"-R" + reverseTunnel, "user@host"},
			flag: "-R",
			want: []string{"-R" + reverseTunnel, "user@host"},
		},
		{
			name: "injects when different -R present",
			in:   []string{"-R", "2222:127.0.0.1:22", "user@host"},
			flag: "-R",
			want: []string{"-R", reverseTunnel, "-R", "2222:127.0.0.1:22", "user@host"},
		},
		{
			name: "empty args",
			in:   nil,
			flag: "-R",
			want: []string{"-R", reverseTunnel},
		},
		{
			name: "injects with -r flag",
			in:   []string{"user@host"},
			flag: "-r",
			want: []string{"-r", reverseTunnel, "user@host"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withReverseTunnel(tt.in, tt.flag)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWithReverseTunnelDoesNotMutateInput(t *testing.T) {
	in := []string{"user@host"}
	_ = withReverseTunnel(in, "-R")
	assert.Equal(t, []string{"user@host"}, in)
}
