package diffannot

import (
	"reflect"
	"testing"
)

const parseSample = `diff --git a/main.go b/main.go
index 1111111..2222222 100644
--- a/main.go
+++ b/main.go
@@ -1,4 +1,5 @@ package main
 package main

-func a() {}
+func a() { b() }
+func b() {}
 // end
\ No newline at end of file
diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/new.txt
@@ -0,0 +1 @@
+hello
diff --git a/gone.txt b/gone.txt
deleted file mode 100644
index 4444444..0000000
--- a/gone.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-bye
-now
diff --git a/old name.md b/new name.md
similarity index 90%
rename from old name.md
rename to new name.md
index 5555555..6666666 100644
--- a/old name.md
+++ b/new name.md
@@ -3 +3 @@ title
-x
+y
diff --git a/logo.png b/logo.png
index 7777777..8888888 100644
Binary files a/logo.png and b/logo.png differ`

func TestParseReadsEveryKindOfFile(t *testing.T) {
	lines := Lines(parseSample + "\n")
	files := Parse(lines)
	type shape struct {
		Path, OldPath, Status string
		Binary                bool
		Add, Del, Hunks       int
	}
	var got []shape
	for _, f := range files {
		got = append(got, shape{f.Path, f.OldPath, f.Status, f.Binary, f.Add, f.Del, len(f.Hunks)})
	}
	want := []shape{
		{"main.go", "", StatusModified, false, 2, 1, 1},
		{"new.txt", "", StatusAdded, false, 1, 0, 1},
		{"gone.txt", "", StatusDeleted, false, 0, 2, 1},
		{"new name.md", "old name.md", StatusRenamed, false, 1, 1, 1},
		{"logo.png", "", StatusModified, true, 0, 0, 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files =\n%+v\nwant\n%+v", got, want)
	}

	h := files[0].Hunks[0]
	if h.Header != "@@ -1,4 +1,5 @@ package main" {
		t.Errorf("header = %q", h.Header)
	}
	wantLines := []Line{
		{Op: ' ', Old: 1, New: 1, Text: "package main", Idx: 5},
		{Op: ' ', Old: 2, New: 2, Text: "", Idx: 6},
		{Op: '-', Old: 3, Text: "func a() {}", Idx: 7},
		{Op: '+', New: 3, Text: "func a() { b() }", Idx: 8},
		{Op: '+', New: 4, Text: "func b() {}", Idx: 9},
		{Op: ' ', Old: 4, New: 5, Text: "// end", Idx: 10},
	}
	if !reflect.DeepEqual(h.Lines, wantLines) {
		t.Fatalf("lines =\n%+v\nwant\n%+v", h.Lines, wantLines)
	}
	// (the blank context line above has lost its leading space, as an
	// editor or a trim can make it; it is still a line of the file)
	//
	// Idx is the coordinate Anchor works in: the line it names is the
	// line the parse says it is.
	for _, l := range h.Lines {
		if raw := lines[l.Idx]; raw != "" && raw[1:] != l.Text {
			t.Errorf("idx %d names %q, parse says %q", l.Idx, lines[l.Idx], l.Text)
		}
	}
	if d := files[2].Hunks[0].Lines; d[0].Old != 1 || d[1].Old != 2 || d[1].New != 0 {
		t.Errorf("deleted lines = %+v", d)
	}
	if r := files[3].Hunks[0].Lines; r[0].Old != 3 || r[1].New != 3 {
		t.Errorf("renamed lines = %+v", r)
	}
}

func TestLinesMatchesTheAnchorSplit(t *testing.T) {
	if got := Lines("a\nb\n\n"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("Lines = %q", got)
	}
}
