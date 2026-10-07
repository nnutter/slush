package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForwardValue(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"8080", "localhost:8080:localhost:8080"},
		{"8080:80", "localhost:8080:localhost:80"},
		{"8080:db", "localhost:8080:db:8080"},
		{"8080:db:5432", "localhost:8080:db:5432"},
		{"127.0.0.1:8080", "127.0.0.1:8080:localhost:8080"},
		{"127.0.0.1:8080:80", "127.0.0.1:8080:localhost:80"},
		{"127.0.0.1:8080:db", "127.0.0.1:8080:db:8080"},
		{"127.0.0.1:8080:db:5432", "127.0.0.1:8080:db:5432"},
		{":8080::80", "localhost:8080:localhost:80"},
		{"8080::", "localhost:8080:localhost:8080"},
		{"[::1]:8080:[2001:db8::1]:80", "[::1]:8080:[2001:db8::1]:80"},
		{"8080:[::1]", "localhost:8080:[::1]:8080"},
		{"00080", "localhost:80:localhost:80"},
	} {
		t.Run(test.input, func(t *testing.T) {
			var value forwardValues
			require.NoError(t, value.Set(test.input))
			assert.Equal(t, test.want, value.String())
			// Each occurrence appends, rather than overwrites or splits on CSV.
			require.NoError(t, value.Set("9000"))
			assert.Equal(t, test.want+", localhost:9000:localhost:9000", value.String())
		})
	}
}

func TestForwardValueRejectsInvalidInput(t *testing.T) {
	for _, test := range []struct{ input, detail string }{
		{"", "listen port"},
		{"0", "between 1 and 65535"},
		{"65536", "between 1 and 65535"},
		{"8080:db:0", "destination port"},
		{"8080:db:nope", "destination port"},
		{"8080:[::1", "missing ']'"},
		{"8080:[db]:80", "IPv6 address"},
		{"8080:/tmp/socket", "invalid TCP host"},
		{"8080:db:80:extra", "too many components"},
		{"8080,9000", "listen port"},
	} {
		t.Run(test.input, func(t *testing.T) {
			var value forwardValues
			require.NoError(t, value.Set("8080"))
			err := value.Set(test.input)
			require.ErrorContains(t, err, test.detail)
			assert.Equal(t, "localhost:8080:localhost:8080", value.String(), "invalid input must not alter existing forwards")
		})
	}
}
