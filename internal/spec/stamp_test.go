package spec

import (
	"os/exec"
	"strings"
	"testing"
)

// A person's name rides in the marker's date slot and parses back out,
// and the note is still a human's comment to every rule that asks.
func TestStampedNoteIsStillAUserComment(t *testing.T) {
	doc := "# T\n\n## Problem\n\nit hurts\n"
	out, err := AddComment(doc, 5, "user", Stamp("2026-09-27", "Simon (phone)\n"), "say who")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "%% @user(2026-09-27, Simon phone): say who") {
		t.Fatalf("marker = %q", out)
	}
	d := Parse(out)
	if len(d.Markers) != 1 {
		t.Fatalf("markers = %+v", d.Markers)
	}
	mk := d.Markers[0]
	if date, who := SplitStamp(mk.Date); mk.Author != "user" || date != "2026-09-27" || who != "Simon phone" || mk.Text != "say who" {
		t.Errorf("marker = %+v (date %q, who %q)", mk, date, who)
	}
	if len(d.UserOpenThreads()) != 1 {
		t.Errorf("a stamped user note does not hold the gate: %+v", d.Threads())
	}
	res, err := ResolveComment(out, mk.Line, "user", Stamp("2026-09-28", "Ana"))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(Parse(res).UserOpenThreads()); n != 0 {
		t.Errorf("a stamped user resolution left %d thread(s) open", n)
	}
	if date, who := SplitStamp("2026-09-27"); date != "2026-09-27" || who != "" {
		t.Errorf("SplitStamp(date) = %q, %q", date, who)
	}
	if Stamp("2026-09-27", " ") != "2026-09-27" {
		t.Error("an empty person must leave the bare date")
	}
}

func TestRevIsGitsBlobID(t *testing.T) {
	content := []byte("# spec\n\nbody\n")
	cmd := exec.Command("git", "hash-object", "--stdin")
	cmd.Stdin = strings.NewReader(string(content))
	want, err := cmd.Output()
	if err != nil {
		t.Skip("git not available:", err)
	}
	if got := Rev(content); got != strings.TrimSpace(string(want)) {
		t.Errorf("Rev = %s, git says %s", got, want)
	}
}

func TestHeadingLines(t *testing.T) {
	got := HeadingLines("# T\n## One\ntext\n### sub\n## Two \n")
	if len(got) != 2 || got[0] != (Heading{"One", 2}) || got[1] != (Heading{"Two", 5}) {
		t.Errorf("HeadingLines = %+v", got)
	}
}
