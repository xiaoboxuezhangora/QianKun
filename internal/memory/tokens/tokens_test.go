package tokens

import "testing"

func TestEstimateTextTokens(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "empty", text: "", want: 0},
		{name: "whitespace", text: " \n\t", want: 0},
		{name: "english", text: "abcd efgh", want: 2},
		{name: "chinese", text: "你好世界", want: 6},
		{name: "mixed", text: "hello 世界", want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EstimateTextTokens(tt.text); got != tt.want {
				t.Fatalf("EstimateTextTokens(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}
