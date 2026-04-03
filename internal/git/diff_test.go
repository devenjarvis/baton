package git_test

import (
	"testing"

	"github.com/devenjarvis/baton/internal/git"
)

func TestParseDiffFiles_Empty(t *testing.T) {
	files := git.ParseDiffFiles("")
	if len(files) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(files))
	}
}

func TestParseDiffFiles_SingleModified(t *testing.T) {
	rawDiff := `diff --git a/foo.go b/foo.go
index abc1234..def5678 100644
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package main

+// added comment
 func main() {}
`
	files := git.ParseDiffFiles(rawDiff)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	f := files[0]
	if f.Status != "M" {
		t.Errorf("expected Status \"M\", got %q", f.Status)
	}
	if f.Path != "foo.go" {
		t.Errorf("expected Path \"foo.go\", got %q", f.Path)
	}
	if len(f.Lines) == 0 {
		t.Error("expected non-empty Lines")
	}
	if f.Lines[0] != "diff --git a/foo.go b/foo.go" {
		t.Errorf("first line should be diff header, got %q", f.Lines[0])
	}
}

func TestParseDiffFiles_AddedAndDeleted(t *testing.T) {
	rawDiff := `diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..1234567
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+hello
+world
diff --git a/old.txt b/old.txt
deleted file mode 100644
index 1234567..0000000
--- a/old.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-goodbye
-world
`
	files := git.ParseDiffFiles(rawDiff)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	// First file: added
	if files[0].Path != "new.txt" {
		t.Errorf("expected first path \"new.txt\", got %q", files[0].Path)
	}
	if files[0].Status != "A" {
		t.Errorf("expected first status \"A\", got %q", files[0].Status)
	}

	// Second file: deleted
	if files[1].Path != "old.txt" {
		t.Errorf("expected second path \"old.txt\", got %q", files[1].Path)
	}
	if files[1].Status != "D" {
		t.Errorf("expected second status \"D\", got %q", files[1].Status)
	}
}

func TestParseDiffFiles_BinaryFile(t *testing.T) {
	rawDiff := `diff --git a/image.png b/image.png
index abc1234..def5678 100644
Binary files a/image.png and b/image.png differ
`
	files := git.ParseDiffFiles(rawDiff)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	f := files[0]
	if f.Path != "image.png" {
		t.Errorf("expected Path \"image.png\", got %q", f.Path)
	}
	if f.Status != "M" {
		t.Errorf("expected Status \"M\", got %q", f.Status)
	}
	if len(f.Lines) == 0 {
		t.Error("expected non-empty Lines for binary file")
	}
	// Should not have any +/- content lines
	for _, line := range f.Lines {
		if len(line) > 0 && (line[0] == '+' || line[0] == '-') {
			t.Errorf("binary file should not have +/- lines, got %q", line)
		}
	}
}
