package skill

import "testing"

func TestParseFrontmatterVersion(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "quoted value",
			in:   "---\nname: sprawl\nversion: \"0.1.0\"\ndescription: x\n---\n# body\n",
			want: "0.1.0",
		},
		{
			name: "bare value",
			in:   "---\nname: sprawl\nversion: 0.2.3\n---\nbody\n",
			want: "0.2.3",
		},
		{
			name: "single-quoted value",
			in:   "---\nversion: '1.0.0'\n---\n",
			want: "1.0.0",
		},
		{
			name: "no frontmatter",
			in:   "version: 0.1.0\nbody\n",
			want: "",
		},
		{
			name: "frontmatter without version",
			in:   "---\nname: sprawl\ndescription: x\n---\nbody\n",
			want: "",
		},
		{
			name: "unterminated frontmatter",
			in:   "---\nversion: 0.1.0\n",
			want: "",
		},
		{
			name: "version after body is ignored",
			in:   "---\nname: sprawl\n---\nversion: 0.5.0\n",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseFrontmatterVersion([]byte(tc.in)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
