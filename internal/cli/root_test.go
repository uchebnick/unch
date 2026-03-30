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
		{name: "create command", args: []string{"create", "ci"}, want: "create"},
		{name: "init command", args: []string{"init"}, want: "init"},
		{name: "index command", args: []string{"index", "--root", "."}, want: "index"},
		{name: "search command", args: []string{"search", "RunCLI"}, want: "search"},
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
