package agent

import "testing"

func TestClaudeModelIDHint(t *testing.T) {
	cases := []struct {
		in, want string
		bad      bool
	}{
		{"claude-haiku-4.5", "claude-haiku-4-5", true},
		{"claude-sonnet-4.5-20250929", "claude-sonnet-4-5-20250929", true},
		{"claude-3.5-sonnet", "claude-3-5-sonnet", true},
		{"claude-haiku-4-5", "", false},
		{"claude-sonnet-5", "", false},
		{"sonnet", "", false},
		{"us.anthropic.claude-3-5-sonnet-20241022-v2:0", "", false},
		{"gpt-5.1", "", false},
	}
	for _, c := range cases {
		got, bad := ClaudeModelIDHint(c.in)
		if got != c.want || bad != c.bad {
			t.Errorf("ClaudeModelIDHint(%q) = %q, %v; want %q, %v", c.in, got, bad, c.want, c.bad)
		}
	}
}
