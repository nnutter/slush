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
	assert.Equal(t, "ssh", clientBinary(modeMosh))
}

func TestReverseTunnelFlag(t *testing.T) {
	assert.Equal(t, "-R", reverseTunnelFlag(modeSSH))
	assert.Equal(t, "-r", reverseTunnelFlag(modeET))
	assert.Equal(t, "-R", reverseTunnelFlag(modeMosh))
}

func TestReverseTunnelArg(t *testing.T) {
	assert.Equal(t, sshReverseTunnel, reverseTunnelArg(modeSSH))
	assert.Equal(t, etReverseTunnel, reverseTunnelArg(modeET))
	assert.Equal(t, sshReverseTunnel, reverseTunnelArg(modeMosh))
}

func TestWithReverseTunnel(t *testing.T) {
	tests := []struct {
		name   string
		in     []string
		flag   string
		tunnel string
		want   []string
	}{
		{
			name:   "ssh injects when missing",
			in:     []string{"user@host"},
			flag:   "-R",
			tunnel: sshReverseTunnel,
			want:   []string{"-R", sshReverseTunnel, "user@host"},
		},
		{
			name:   "ssh skips when separate -R already present",
			in:     []string{"-R", sshReverseTunnel, "user@host"},
			flag:   "-R",
			tunnel: sshReverseTunnel,
			want:   []string{"-R", sshReverseTunnel, "user@host"},
		},
		{
			name:   "ssh skips when combined -R already present",
			in:     []string{"-R" + sshReverseTunnel, "user@host"},
			flag:   "-R",
			tunnel: sshReverseTunnel,
			want:   []string{"-R" + sshReverseTunnel, "user@host"},
		},
		{
			name:   "ssh injects when different -R present",
			in:     []string{"-R", "2222:127.0.0.1:22", "user@host"},
			flag:   "-R",
			tunnel: sshReverseTunnel,
			want:   []string{"-R", sshReverseTunnel, "-R", "2222:127.0.0.1:22", "user@host"},
		},
		{
			name:   "ssh empty args",
			in:     nil,
			flag:   "-R",
			tunnel: sshReverseTunnel,
			want:   []string{"-R", sshReverseTunnel},
		},
		{
			name:   "et injects when missing",
			in:     []string{"user@host"},
			flag:   "-r",
			tunnel: etReverseTunnel,
			want:   []string{"-r", etReverseTunnel, "user@host"},
		},
		{
			name:   "et skips when separate -r already present",
			in:     []string{"-r", etReverseTunnel, "user@host"},
			flag:   "-r",
			tunnel: etReverseTunnel,
			want:   []string{"-r", etReverseTunnel, "user@host"},
		},
		{
			name:   "et skips when combined -r already present",
			in:     []string{"-r" + etReverseTunnel, "user@host"},
			flag:   "-r",
			tunnel: etReverseTunnel,
			want:   []string{"-r" + etReverseTunnel, "user@host"},
		},
		{
			name:   "et skips when --reversetunnel already present",
			in:     []string{"--reversetunnel", etReverseTunnel, "user@host"},
			flag:   "-r",
			tunnel: etReverseTunnel,
			want:   []string{"--reversetunnel", etReverseTunnel, "user@host"},
		},
		{
			name:   "et skips when --reversetunnel= already present",
			in:     []string{"--reversetunnel=" + etReverseTunnel, "user@host"},
			flag:   "-r",
			tunnel: etReverseTunnel,
			want:   []string{"--reversetunnel=" + etReverseTunnel, "user@host"},
		},
		{
			name:   "et injects when different -r present",
			in:     []string{"-r", "2222:22", "user@host"},
			flag:   "-r",
			tunnel: etReverseTunnel,
			want:   []string{"-r", etReverseTunnel, "-r", "2222:22", "user@host"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withReverseTunnel(tt.in, tt.flag, tt.tunnel)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWithReverseTunnelDoesNotMutateInput(t *testing.T) {
	in := []string{"user@host"}
	_ = withReverseTunnel(in, "-R", sshReverseTunnel)
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
