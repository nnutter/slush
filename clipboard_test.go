package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasURLScheme(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		{target: "https://example.com", want: true},
		{target: "http://localhost:8000/path?q=1", want: true},
		{target: "ftp://files/example", want: true},
		{target: "custom+scheme://x", want: true},
		{target: "a-b.c_d://x", want: false}, // underscore is not a scheme char
		{target: "1http://x", want: false},
		{target: "example.com", want: false},
		{target: "/remote/file.txt", want: false},
		{target: "relative/path", want: false},
		{target: "", want: false},
		{target: "https://", want: false},
		{target: "://missing-scheme", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			assert.Equal(t, tt.want, hasURLScheme(tt.target))
		})
	}
}
