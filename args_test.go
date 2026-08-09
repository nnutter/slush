package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTakeModeFlags(t *testing.T) {
	tests := []struct {
		name     string
		in       []string
		wantMode clientMode
		wantRest []string
		wantErr  string
	}{
		{
			name:     "absent defaults to ssh",
			in:       []string{"user@host"},
			wantMode: modeSSH,
			wantRest: []string{"user@host"},
		},
		{
			name:     "leading --et",
			in:       []string{"--et", "user@host"},
			wantMode: modeET,
			wantRest: []string{"user@host"},
		},
		{
			name:     "leading --mosh",
			in:       []string{"--mosh", "user@host"},
			wantMode: modeMosh,
			wantRest: []string{"user@host"},
		},
		{
			name:     "repeated leading --et",
			in:       []string{"--et", "--et", "user@host"},
			wantMode: modeET,
			wantRest: []string{"user@host"},
		},
		{
			name:     "repeated leading --mosh",
			in:       []string{"--mosh", "--mosh", "user@host"},
			wantMode: modeMosh,
			wantRest: []string{"user@host"},
		},
		{
			name:     "not leading is left alone",
			in:       []string{"user@host", "--et"},
			wantMode: modeSSH,
			wantRest: []string{"user@host", "--et"},
		},
		{
			name:     "empty",
			in:       nil,
			wantMode: modeSSH,
			wantRest: nil,
		},
		{
			name:    "--et then --mosh",
			in:      []string{"--et", "--mosh", "user@host"},
			wantErr: "--et and --mosh are mutually exclusive",
		},
		{
			name:    "--mosh then --et",
			in:      []string{"--mosh", "--et", "user@host"},
			wantErr: "--et and --mosh are mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMode, gotRest, err := takeModeFlags(tt.in)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantMode, gotMode)
			assert.Equal(t, tt.wantRest, gotRest)
		})
	}
}

func TestClientBinary(t *testing.T) {
	assert.Equal(t, "ssh", clientBinary(modeSSH))
	assert.Equal(t, "et", clientBinary(modeET))
	assert.Equal(t, "mosh", clientBinary(modeMosh))
}

func TestTakeSSHForwards(t *testing.T) {
	tests := []struct {
		name         string
		in           []string
		wantForwards []string
		wantRest     []string
		wantErr      string
	}{
		{
			name:         "no forwards",
			in:           []string{"user@host"},
			wantForwards: nil,
			wantRest:     []string{"user@host"},
		},
		{
			name:         "separate -L",
			in:           []string{"-L", "8080:127.0.0.1:8080", "user@host"},
			wantForwards: []string{"-L", "8080:127.0.0.1:8080"},
			wantRest:     []string{"user@host"},
		},
		{
			name:         "combined -L",
			in:           []string{"-L8080:127.0.0.1:8080", "user@host"},
			wantForwards: []string{"-L", "8080:127.0.0.1:8080"},
			wantRest:     []string{"user@host"},
		},
		{
			name:         "separate -R",
			in:           []string{"-R", "9000:127.0.0.1:9000", "user@host"},
			wantForwards: []string{"-R", "9000:127.0.0.1:9000"},
			wantRest:     []string{"user@host"},
		},
		{
			name:         "combined -R",
			in:           []string{"-R9000:127.0.0.1:9000", "user@host"},
			wantForwards: []string{"-R", "9000:127.0.0.1:9000"},
			wantRest:     []string{"user@host"},
		},
		{
			name: "multiple forwards preserve order",
			in: []string{
				"-L", "8080:127.0.0.1:8080",
				"-R", "9000:127.0.0.1:9000",
				"-L3000:127.0.0.1:3000",
				"user@host",
			},
			wantForwards: []string{
				"-L", "8080:127.0.0.1:8080",
				"-R", "9000:127.0.0.1:9000",
				"-L", "3000:127.0.0.1:3000",
			},
			wantRest: []string{"user@host"},
		},
		{
			name:         "forwards after other flags",
			in:           []string{"-p", "60001", "-L", "8080:127.0.0.1:8080", "user@host"},
			wantForwards: []string{"-L", "8080:127.0.0.1:8080"},
			wantRest:     []string{"-p", "60001", "user@host"},
		},
		{
			name:         "does not look past --",
			in:           []string{"--", "-L", "8080:127.0.0.1:8080", "user@host"},
			wantForwards: nil,
			wantRest:     []string{"--", "-L", "8080:127.0.0.1:8080", "user@host"},
		},
		{
			name:    "missing -L argument",
			in:      []string{"-L"},
			wantErr: "-L requires an argument",
		},
		{
			name:    "missing -R argument",
			in:      []string{"-R"},
			wantErr: "-R requires an argument",
		},
		{
			name:         "separate -L consumes next arg",
			in:           []string{"-L", "user@host"},
			wantForwards: []string{"-L", "user@host"},
			wantRest:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotForwards, gotRest, err := takeSSHForwards(tt.in)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantForwards, gotForwards)
			assert.Equal(t, tt.wantRest, gotRest)
		})
	}
}

func TestWithReverseTunnel(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "injects when missing",
			in:   []string{"user@host"},
			want: []string{"-R", sshReverseTunnel, "user@host"},
		},
		{
			name: "skips when separate -R already present",
			in:   []string{"-R", sshReverseTunnel, "user@host"},
			want: []string{"-R", sshReverseTunnel, "user@host"},
		},
		{
			name: "skips when combined -R already present",
			in:   []string{"-R" + sshReverseTunnel, "user@host"},
			want: []string{"-R" + sshReverseTunnel, "user@host"},
		},
		{
			name: "injects when different -R present",
			in:   []string{"-R", "2222:127.0.0.1:22", "user@host"},
			want: []string{"-R", sshReverseTunnel, "-R", "2222:127.0.0.1:22", "user@host"},
		},
		{
			name: "preserves -L forwards",
			in:   []string{"-L", "8080:127.0.0.1:8080", "user@host"},
			want: []string{"-R", sshReverseTunnel, "-L", "8080:127.0.0.1:8080", "user@host"},
		},
		{
			name: "empty args",
			in:   nil,
			want: []string{"-R", sshReverseTunnel},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withReverseTunnel(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWithReverseTunnelDoesNotMutateInput(t *testing.T) {
	in := []string{"user@host"}
	_ = withReverseTunnel(in)
	assert.Equal(t, []string{"user@host"}, in)
}

func TestMoshDestination(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    string
		wantErr string
	}{
		{
			name: "bare host",
			in:   []string{"user@host"},
			want: "user@host",
		},
		{
			name: "host after flags",
			in:   []string{"-p", "60001", "--predict=always", "user@host"},
			want: "user@host",
		},
		{
			name: "combined -p",
			in:   []string{"-p60001", "user@host"},
			want: "user@host",
		},
		{
			name: "after --",
			in:   []string{"--", "user@host", "true"},
			want: "user@host",
		},
		{
			name: "with remote command",
			in:   []string{"user@host", "tmux", "a"},
			want: "user@host",
		},
		{
			name:    "missing",
			in:      []string{"-p", "60001"},
			wantErr: "missing mosh destination host",
		},
		{
			name:    "empty",
			in:      nil,
			wantErr: "missing mosh destination host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := moshDestination(tt.in)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestETDestination(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    string
		wantErr string
	}{
		{
			name: "bare host",
			in:   []string{"user@host"},
			want: "user@host",
		},
		{
			name: "host with et port",
			in:   []string{"user@host:2022"},
			want: "user@host:2022",
		},
		{
			name: "host after flags",
			in:   []string{"-p", "2022", "user@host"},
			want: "user@host",
		},
		{
			name: "after --",
			in:   []string{"--", "user@host"},
			want: "user@host",
		},
		{
			name:    "missing",
			in:      []string{"-p", "2022"},
			wantErr: "missing et destination host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := etDestination(tt.in)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSSHHostFromETDestination(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "host", want: "host"},
		{in: "user@host", want: "user@host"},
		{in: "user@host:2022", want: "user@host"},
		{in: "host:2022", want: "host"},
		{in: "[::1]", want: "[::1]"},
		{in: "[::1]:2022", want: "[::1]"},
		{in: "user@[::1]:2022", want: "user@[::1]"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, sshHostFromETDestination(tt.in))
		})
	}
}

func TestWithMoshSSHControlPath(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "injects when missing",
			in:   []string{"user@host"},
			want: []string{"--ssh=ssh -o ControlPath=/tmp/c", "user@host"},
		},
		{
			name: "appends to --ssh=",
			in:   []string{"--ssh=ssh -p 2222", "user@host"},
			want: []string{"--ssh=ssh -p 2222 -o ControlPath=/tmp/c", "user@host"},
		},
		{
			name: "appends to separate --ssh",
			in:   []string{"--ssh", "ssh -p 2222", "user@host"},
			want: []string{"--ssh", "ssh -p 2222 -o ControlPath=/tmp/c", "user@host"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withMoshSSHControlPath(tt.in, "/tmp/c")
			assert.Equal(t, tt.want, got)
		})
	}
}
