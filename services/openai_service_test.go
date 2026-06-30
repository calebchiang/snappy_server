package services

import "testing"

func TestNormalizeObjectWord(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple word",
			input: "cup",
			want:  "cup",
		},
		{
			name:  "article and punctuation",
			input: "A cup.",
			want:  "cup",
		},
		{
			name:  "extra words",
			input: "mug or cup",
			want:  "mug",
		},
		{
			name:  "empty",
			input: "   ",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeObjectWord(tt.input); got != tt.want {
				t.Fatalf("normalizeObjectWord(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
