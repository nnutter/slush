package main

import (
	"strings"
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

func TestSSHHostOperand(t *testing.T) {
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
			name: "host with remote command",
			in:   []string{"user@host", "tmux", "a"},
			want: "user@host",
		},
		{
			name: "port and key flags",
			in:   []string{"-p", "2222", "-i", "~/.ssh/id", "user@host"},
			want: "user@host",
		},
		{
			name: "combined short port",
			in:   []string{"-p2222", "user@host"},
			want: "user@host",
		},
		{
			name: "login name flags",
			in:   []string{"-l", "user", "host"},
			want: "host",
		},
		{
			name: "combined login name",
			in:   []string{"-luser", "host"},
			want: "host",
		},
		{
			name: "ssh option",
			in:   []string{"-o", "StrictHostKeyChecking=no", "user@host"},
			want: "user@host",
		},
		{
			name: "forwards skipped",
			in:   []string{"-L", "8080:127.0.0.1:8080", "-R9000:127.0.0.1:9000", "user@host"},
			want: "user@host",
		},
		{
			name: "jump host",
			in:   []string{"-J", "jump", "user@host"},
			want: "user@host",
		},
		{
			name: "flag bundles",
			in:   []string{"-vv", "-At", "user@host"},
			want: "user@host",
		},
		{
			name: "after --",
			in:   []string{"--", "user@host"},
			want: "user@host",
		},
		{
			name:    "missing",
			in:      []string{"-p", "2222"},
			wantErr: "missing ssh destination host",
		},
		{
			name:    "empty",
			in:      nil,
			wantErr: "missing ssh destination host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sshHostOperand(tt.in)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRemoteEnvPrefix(t *testing.T) {
	prefix := remoteEnvPrefix("tok123")
	assert.Contains(t, prefix, "SLUSH=1")
	assert.Contains(t, prefix, "SLUSH_TOKEN=tok123")
	assert.Contains(t, prefix, "BROWSER=slush-open")
	assert.Contains(t, prefix, "PATH=")
	assert.Contains(t, prefix, "/slush/bin")
	assert.True(t, strings.HasSuffix(prefix, "; "))
}

func TestWithRemoteEnvSSH(t *testing.T) {
	tests := []struct {
		name         string
		in           []string
		wantContains []string
		wantHead     []string
		interactive  bool
		wantErr      string
	}{
		{
			name:         "interactive wraps login shell",
			in:           []string{"user@host"},
			wantHead:     []string{"user@host"},
			wantContains: []string{`exec "${SHELL:-/bin/sh}" -l`, "SLUSH=1"},
			interactive:  true,
		},
		{
			name:         "command is joined like ssh does",
			in:           []string{"user@host", "tmux", "new", "-s", "x"},
			wantHead:     []string{"user@host"},
			wantContains: []string{"tmux new -s x"},
			interactive:  false,
		},
		{
			name:         "options stay ahead of host",
			in:           []string{"-p", "2222", "user@host", "true"},
			wantHead:     []string{"-p", "2222", "user@host"},
			wantContains: []string{"true"},
			interactive:  false,
		},
		{
			name:    "missing host",
			in:      []string{"-p", "2222"},
			wantErr: "missing ssh destination host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, interactive, err := withRemoteEnvSSH(tt.in, "tok123")
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.interactive, interactive)
			assert.Equal(t, tt.wantHead, got[:len(tt.wantHead)])
			joined := strings.Join(got, " ")
			for _, want := range tt.wantContains {
				assert.Contains(t, joined, want)
			}
		})
	}
}

func TestWithRemoteEnvMosh(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr string
	}{
		{
			name: "interactive inserts separator and wrapper",
			in:   []string{"user@host"},
			want: []string{"--", "user@host", "sh", "-c"},
		},
		{
			name: "existing separator is normalized",
			in:   []string{"--", "user@host"},
			want: []string{"--", "user@host", "sh", "-c"},
		},
		{
			name: "mosh options stay ahead",
			in:   []string{"-p", "60001", "--ssh=ssh -p 2222", "user@host"},
			want: []string{"-p", "60001", "--ssh=ssh -p 2222", "--", "user@host", "sh", "-c"},
		},
		{
			name: "command becomes exec argv",
			in:   []string{"user@host", "tmux", "a"},
			want: []string{"--", "user@host", "sh", "-c"},
		},
		{
			name:    "missing host",
			in:      []string{"-p", "60001"},
			wantErr: "missing mosh destination host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := withRemoteEnvMosh(tt.in, "tok123")
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got[:len(tt.want)])
			// The wrapper script always carries the session env.
			assert.Contains(t, got[len(tt.want)], "SLUSH=1")
			assert.Contains(t, got[len(tt.want)], "SLUSH_TOKEN=tok123")
		})
	}
}

func TestWithRemoteEnvMoshCommandTail(t *testing.T) {
	got, err := withRemoteEnvMosh([]string{"user@host", "tmux", "a"}, "tok123")
	require.NoError(t, err)
	// ... sh -c '<prefix>exec "$@"' sh tmux a
	assert.Equal(t, []string{"sh", "tmux", "a"}, got[len(got)-3:])
	assert.Contains(t, got[len(got)-4], `exec "$@"`)

	got, err = withRemoteEnvMosh([]string{"user@host"}, "tok123")
	require.NoError(t, err)
	assert.Contains(t, got[len(got)-1], `exec "${SHELL:-/bin/sh}" -l`)
}

func TestWithSSHControlPath(t *testing.T) {
	got := withSSHControlPath([]string{"user@host"}, sessionParams{controlPath: "/tmp/c"})
	assert.Equal(t, []string{"-o", "ControlPath=/tmp/c", "user@host"}, got)
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
			got := withMoshSSHControlPath(tt.in, sessionParams{controlPath: "/tmp/c"})
			assert.Equal(t, tt.want, got)
		})
	}
}
