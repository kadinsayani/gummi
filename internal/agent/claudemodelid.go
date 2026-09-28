package agent

import (
	"regexp"
	"strings"
)

// dottedClaudeVersion matches a Claude model id whose version is spelled
// with a dot — "claude-haiku-4.5", "claude-sonnet-4.5-20250929", or the
// older family-last "claude-3.5-sonnet".
var dottedClaudeVersion = regexp.MustCompile(`^(claude-(?:[a-z]+-)?)(\d+)\.(\d+)(.*)$`)

// ClaudeModelIDHint reports whether model is spelled in a form the claude
// CLI refuses, and the spelling it accepts.
//
// Anthropic's model ids spell a version with dashes (claude-haiku-4-5);
// other backends that serve the same models — GitHub Copilot's CLI among
// them — spell it with a dot (claude-haiku-4.5). A profiles.yaml written
// for one and pointed at the other names a model the claude CLI rejects
// at the first turn ("There's an issue with the selected model"), and
// nothing short of a live probe said so. The dotted form is never a valid
// Anthropic id, so this is a fact about the string, not a guess about a
// model list that goes stale — a Bedrock- or Vertex-style id (which the
// claude CLI also takes, and which may contain dots of its own) does not
// start with "claude-<family>-<n>." and is left alone.
func ClaudeModelIDHint(model string) (suggest string, bad bool) {
	m := dottedClaudeVersion.FindStringSubmatch(strings.TrimSpace(model))
	if m == nil {
		return "", false
	}
	return m[1] + m[2] + "-" + m[3] + m[4], true
}
