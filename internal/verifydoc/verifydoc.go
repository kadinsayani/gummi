// Package verifydoc is the zero-token, deterministic Verify floor for a
// research document (FD-076): no open user `%%` threads, every inline
// `path:line` / `path:start-end` citation in the evidence section resolves
// against the managed repo checkout, and the document's checklist
// reconciles against the Slices/Out of scope contract. No agent, model, or
// API is ever invoked — Check is a pure function over the artifact text
// and a caller-supplied view of the repo's files.
//
// Which headings those two sections are found under depends on the card's
// research mode and is not this package's business to know: the caller
// passes a spec.Layout. A survey cites under `## Findings` and reconciles
// `## Questions`; a diagnosis cites under `## Evidence` and reconciles
// `## Causes`. The checks themselves are identical, which is the reason
// the mode is a layout rather than a second implementation.
//
// Citation format: a backtick-quoted `path:line` or `path:start-end` token
// inside the Findings section, optionally followed (after only whitespace)
// by a fenced code block quoting the verbatim content the citation claims
// sits at that location:
//
//	The retry loop lives at `internal/foo.go:42`
//	```go
//	return retryLoop()
//	```
//
// Coverage contract: every bullet under the layout's coverage section must
// be answered — its text must appear either in some slice's `requirements`
// list (`## Slices`, fenced `yaml`) or as the key of an explicit
// `- key: prose` line under `## Out of scope`. The comparison is the
// bullet's text, not its typography: case, runs of whitespace, a leading
// question number ("1.", "Q2:"), emphasis and code backticks, and trailing
// punctuation are ignored (normalize). Nothing looser is: a paraphrase is
// not a match, because the whole point of the check is that a reader can
// find each question's answer by searching for the question.
package verifydoc

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/morphis/gummi/internal/spec"
)

// Report is the structured verdict Check returns: one entry per broken
// citation and per unmapped coverage item, plus the open-thread count. It
// is read-only output — never written back into the artifact.
type Report struct {
	OpenThreads int
	Citations   []CitationIssue
	Coverage    []CoverageIssue
}

// Pass reports whether the document clears all three checks.
func (r Report) Pass() bool {
	return r.OpenThreads == 0 && len(r.Citations) == 0 && len(r.Coverage) == 0
}

// CitationIssue names one citation that failed to resolve and why.
type CitationIssue struct {
	Citation string // the raw "path:line" / "path:start-end" text
	Reason   string
}

// CoverageIssue names one coverage-section bullet — a survey's question,
// a diagnosis's cause — with neither a slice nor an out-of-scope line
// answering it.
type CoverageIssue struct {
	Item   string // the unmapped Questions bullet, verbatim
	Reason string
}

// Check runs the three deterministic checks against artifact and returns
// the aggregated report. files is the caller-resolved view of the managed
// repo: cited path -> its current lines (0-indexed slice, 1-based line
// numbers in citations). Checks run in order — open threads, citations,
// coverage — and every issue found is aggregated into one report.
func Check(artifact string, files map[string][]string, l spec.Layout) Report {
	doc := spec.Parse(artifact)
	return Report{
		OpenThreads: len(doc.UserOpenThreads()),
		Citations:   findCitations(artifact, files, l),
		Coverage:    findCoverage(artifact, l),
	}
}

// citationRe matches a backtick-quoted `path:line` or `path:start-end`
// token. Paths never contain a backtick, whitespace, or colon.
var citationRe = regexp.MustCompile("`([^`\\s:]+):(\\d+)(?:-(\\d+))?`")

// findCitations scans the artifact's evidence section for citation tokens,
// in document order, and resolves each against files.
func findCitations(artifact string, files map[string][]string, l spec.Layout) []CitationIssue {
	body, ok := spec.ViewSection(artifact, l.Evidence)
	if !ok {
		return nil
	}
	var issues []CitationIssue
	for _, m := range citationRe.FindAllStringSubmatchIndex(body, -1) {
		raw := body[m[0]:m[1]]
		path := body[m[2]:m[3]]
		start, _ := strconv.Atoi(body[m[4]:m[5]])
		end := start
		if m[6] >= 0 {
			end, _ = strconv.Atoi(body[m[6]:m[7]])
		}
		snippet, hasSnippet := findSnippet(body, m[1])
		if issue := resolveCitation(raw, path, start, end, snippet, hasSnippet, files); issue != nil {
			issues = append(issues, *issue)
		}
	}
	return issues
}

// CitedPaths returns the distinct, document-order paths named by every
// citation token in the artifact's evidence section, whether or not the
// citation ultimately resolves. Callers use this to build the files map
// Check needs — reading only the paths a citation actually names, never
// the whole checkout. Pass the same layout both calls use, or the files
// map will be built from a section Check does not read.
func CitedPaths(artifact string, l spec.Layout) []string {
	body, ok := spec.ViewSection(artifact, l.Evidence)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range citationRe.FindAllStringSubmatch(body, -1) {
		path := m[1]
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

// findSnippet looks for a fenced code block starting at the first
// non-whitespace position from i onward, returning its content and
// whether one was found. Anything other than whitespace between the
// citation token and the fence means there is no snippet.
func findSnippet(body string, i int) (string, bool) {
	j := i
	for j < len(body) && isSpace(body[j]) {
		j++
	}
	if !strings.HasPrefix(body[j:], "```") {
		return "", false
	}
	j += 3
	nl := strings.IndexByte(body[j:], '\n')
	if nl < 0 {
		return "", false
	}
	bodyStart := j + nl + 1
	closeIdx := strings.Index(body[bodyStart:], "```")
	if closeIdx < 0 {
		return "", false
	}
	return body[bodyStart : bodyStart+closeIdx], true
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

// resolveCitation applies the three containment/existence/snippet guards
// in order, returning the first failure. A path escaping the repo root is
// never read — files is trusted to hold only root-contained entries
// (fileMap's job at the engine layer), so containment is checked against
// the raw path text alone.
func resolveCitation(raw, path string, start, end int, snippet string, hasSnippet bool, files map[string][]string) *CitationIssue {
	if !contained(path) {
		return &CitationIssue{Citation: raw, Reason: "path escapes the repo root"}
	}
	lines, ok := files[path]
	if !ok {
		return &CitationIssue{Citation: raw, Reason: "file not found: " + path}
	}
	if start < 1 || end < start || end > len(lines) {
		return &CitationIssue{Citation: raw, Reason: "line out of range"}
	}
	if hasSnippet && !snippetPresent(lines, snippet) {
		return &CitationIssue{Citation: raw, Reason: "snippet no longer matches file content"}
	}
	return nil
}

// contained reports whether path stays under the repo root: no absolute
// form and no ".." path segment.
func contained(path string) bool {
	if strings.HasPrefix(path, "/") {
		return false
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// snippetPresent reports whether snippet's lines still appear as a
// contiguous run anywhere in lines — content-based, not line-number based,
// so a stanza shifted by a minor rebase still resolves while genuinely
// altered content is stale. Trailing whitespace is ignored per line.
func snippetPresent(lines []string, snippet string) bool {
	want := normalizeLines(strings.Split(snippet, "\n"))
	if len(want) == 0 {
		return true
	}
	got := normalizeLines(lines)
	if len(want) > len(got) {
		return false
	}
	for i := 0; i+len(want) <= len(got); i++ {
		if equalLines(got[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

func normalizeLines(lines []string) []string {
	// A fenced block's content ends with a trailing newline before the
	// closing ``` — strip the resulting empty final element so it doesn't
	// force an impossible match.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(l, " \t\r")
	}
	return out
}

func equalLines(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sliceEntry is one row of the `## Slices` fenced-YAML scaffold
// (internal/spec/research.go's slicesScaffold).
type sliceEntry struct {
	Title        string   `yaml:"title"`
	OneLiner     string   `yaml:"one-liner"`
	DependsOn    []string `yaml:"depends-on"`
	Requirements []string `yaml:"requirements"`
	ID           string   `yaml:"id"`
}

var yamlFenceRe = regexp.MustCompile("(?s)```ya?ml\\s*\\n(.*?)```")

// findCoverage reconciles every bullet in the layout's coverage section
// against the union of every slice's `requirements` entries and every
// `## Out of scope` line's key, in document order.
func findCoverage(artifact string, l spec.Layout) []CoverageIssue {
	questions := bullets(artifact, l.Coverage)
	if len(questions) == 0 {
		return nil
	}
	answered := requirementSet(artifact)
	outOfScope := outOfScopeKeys(artifact)

	var issues []CoverageIssue
	for _, q := range questions {
		k := normalize(q)
		if answered[k] || outOfScope[k] {
			continue
		}
		issues = append(issues, CoverageIssue{Item: q, Reason: "no slice or out-of-scope line answers it"})
	}
	return issues
}

// questionNumberRe matches the numbering an author puts in front of a
// question — "1.", "2)", "(3)", "Q4:", "#5" — which a slice's requirement
// quoting the question may or may not repeat.
var questionNumberRe = regexp.MustCompile(`^(?:\(\d+\)|[qQ]?\d+[.):]?|#\d+)(?:\s*[-—–:.])?\s+`)

// normalize is the coverage check's one notion of "the same question":
// the text with its typography taken off. It is deliberately small and
// deterministic — case, whitespace runs, a leading number, `**`/backtick
// emphasis and trailing punctuation — so two authors quoting one bullet
// agree, while a reworded question still does not.
func normalize(s string) string {
	s = strings.NewReplacer("**", "", "`", "").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	s = questionNumberRe.ReplaceAllString(s, "")
	s = strings.TrimRight(s, " ?.!:;,")
	return strings.ToLower(strings.TrimSpace(s))
}

// bullets returns the trimmed text of each top-level "- " bullet in the
// named section, in document order.
func bullets(artifact, section string) []string {
	body, ok := spec.ViewSection(artifact, section)
	if !ok {
		return nil
	}
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if s, ok := strings.CutPrefix(line, "- "); ok {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// requirementSet unions every slice's requirements entries from the
// `## Slices` fenced YAML block.
func requirementSet(artifact string) map[string]bool {
	body, ok := spec.ViewSection(artifact, "Slices")
	out := map[string]bool{}
	if !ok {
		return out
	}
	m := yamlFenceRe.FindStringSubmatch(body)
	if m == nil {
		return out
	}
	var entries []sliceEntry
	if err := yaml.Unmarshal([]byte(m[1]), &entries); err != nil {
		return out
	}
	for _, e := range entries {
		for _, r := range e.Requirements {
			if r = normalize(r); r != "" {
				out[r] = true
			}
		}
	}
	return out
}

// outOfScopeKeys reads `## Out of scope` lines shaped `- key: prose` and
// returns the set of keys, normalized.
//
// A question often carries a colon of its own ("Lines: count the last
// line?"), so the key is not simply the text before the FIRST colon:
// every prefix ending at a colon is a candidate key, and a question
// matches when it equals any of them.
func outOfScopeKeys(artifact string) map[string]bool {
	body, ok := spec.ViewSection(artifact, "Out of scope")
	out := map[string]bool{}
	if !ok {
		return out
	}
	for _, line := range strings.Split(body, "\n") {
		s, ok := strings.CutPrefix(line, "- ")
		if !ok {
			continue
		}
		for i := 0; i < len(s); i++ {
			if s[i] != ':' {
				continue
			}
			if key := normalize(s[:i]); key != "" {
				out[key] = true
			}
		}
	}
	return out
}

// CoverageRule states the coverage contract for a layout in the words a
// stage, a refusal and a report all use — one sentence, so the agent
// writing the document and the person reading the refusal are told the
// same rule the check applies.
func CoverageRule(l spec.Layout) string {
	return fmt.Sprintf("every `- ` bullet under `## %s` must be answered by name: repeat its text "+
		"in some row's `requirements` list in the fenced yaml under `## Slices`, or give it an "+
		"`## Out of scope` line shaped `- <the bullet's text>: <why it is not pursued>`. "+
		"Case, spacing, a leading question number and trailing punctuation do not matter; "+
		"a paraphrase does not count, and a `%%%%` note or a Findings paragraph answering the "+
		"question does not map it", l.Coverage)
}

// Summary is the report's one-line count, the form a refusal leads with.
func (r Report) Summary(l spec.Layout) string {
	noun := "question"
	if l.Coverage != "Questions" {
		noun = strings.ToLower(strings.TrimSuffix(l.Coverage, "s"))
	}
	return fmt.Sprintf("%d open thread%s, %d broken citation%s, %d unmapped %s%s",
		r.OpenThreads, plural(r.OpenThreads), len(r.Citations), plural(len(r.Citations)),
		len(r.Coverage), noun, plural(len(r.Coverage)))
}

// Explain is the report written for somebody who has to fix the document:
// what is wrong, item by item, and the rule each item breaks. Empty for a
// passing report.
func (r Report) Explain(l spec.Layout) string {
	if r.Pass() {
		return ""
	}
	var b strings.Builder
	if r.OpenThreads > 0 {
		fmt.Fprintf(&b, "%d open `%%%% @user` thread%s — each needs a resolution before the document can be done.\n",
			r.OpenThreads, plural(r.OpenThreads))
	}
	if len(r.Citations) > 0 {
		fmt.Fprintf(&b, "Broken citations under `## %s`:\n", l.Evidence)
		for _, c := range r.Citations {
			fmt.Fprintf(&b, "- %s — %s\n", c.Citation, c.Reason)
		}
	}
	if len(r.Coverage) > 0 {
		fmt.Fprintf(&b, "Unmapped `## %s` bullets:\n", l.Coverage)
		for _, c := range r.Coverage {
			fmt.Fprintf(&b, "- %s\n", c.Item)
		}
		b.WriteString("Rule: " + CoverageRule(l) + ".\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
