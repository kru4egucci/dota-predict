package telegram

import (
	"testing"
)

func TestMDToTelegramHTML(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "bold",
			in:   "**hello**",
			want: "<b>hello</b>",
		},
		{
			name: "heading",
			in:   "## Summary",
			want: "<b>Summary</b>",
		},
		{
			name: "inline code",
			in:   "use `fmt.Println`",
			want: "use <code>fmt.Println</code>",
		},
		{
			name: "code block",
			in:   "```go\nfmt.Println()\n```",
			want: "<pre>fmt.Println()\n</pre>",
		},
		{
			name: "html escape",
			in:   "1 < 2 & 3 > 1",
			want: "1 &lt; 2 &amp; 3 &gt; 1",
		},
		{
			name: "horizontal rule removed",
			in:   "above\n---\nbelow",
			want: "above\n\nbelow",
		},
		{
			name: "combined",
			in:   "# Title\n**bold** and `code`",
			want: "<b>Title</b>\n<b>bold</b> and <code>code</code>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MDToTelegramHTML(tt.in)
			if got != tt.want {
				t.Errorf("MDToTelegramHTML(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}
}
