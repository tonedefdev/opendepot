package storage

import "testing"

func TestRemoveTrailingSlash(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "forward slash", input: "modules/", want: "modules"},
		{name: "back slash", input: `modules\`, want: "modules"},
		{name: "no trailing separator", input: "modules", want: "modules"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := tt.input
			got, err := RemoveTrailingSlash(&input)
			if err != nil {
				t.Fatalf("RemoveTrailingSlash returned error: %v", err)
			}

			if *got != tt.want {
				t.Errorf("RemoveTrailingSlash(%q) = %q, want %q", tt.input, *got, tt.want)
			}
		})
	}
}

func TestRemoveTrailingSlashNil(t *testing.T) {
	if _, err := RemoveTrailingSlash(nil); err == nil {
		t.Error("expected error for nil input")
	}
}
