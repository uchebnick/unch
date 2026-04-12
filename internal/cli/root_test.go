package cli

import "testing"

func TestDetectCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    string
		wantErr bool
	}{
		{name: "default index", args: nil, want: "index"},
		{name: "auth command", args: []string{"auth", "openrouter"}, want: "auth"},
		{name: "create command", args: []string{"create", "ci"}, want: "create"},
		{name: "bind command", args: []string{"bind", "ci", "https://github.com/acme/widgets/actions/workflows/unch-index.yml"}, want: "bind"},
		{name: "init command", args: []string{"init"}, want: "init"},
		{name: "index command", args: []string{"index", "--root", "."}, want: "index"},
		{name: "remote command", args: []string{"remote", "sync"}, want: "remote"},
		{name: "search command", args: []string{"search", "RunCLI"}, want: "search"},
		{name: "start command", args: []string{"start", "mcp"}, want: "start"},
		{name: "version flag", args: []string{"--version"}, want: "version"},
		{name: "version command", args: []string{"version"}, want: "version"},
		{name: "index flags without command", args: []string{"--root", "."}, want: "index"},
		{name: "unknown command", args: []string{"download", "."}, wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, _, err := detectCommand(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for args %v", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("detectCommand(%v) returned error: %v", tt.args, err)
			}
			if got != tt.want {
				t.Fatalf("detectCommand(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}
