package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFilename(t *testing.T) {
	singer, song, language, style, ok := ParseFilename("万晓利-女儿情-国语-流行.mkv")
	if !ok || singer != "万晓利" || song != "女儿情" || language != "国语" || style != "流行" {
		t.Fatalf("got %q %q %q %q ok=%v", singer, song, language, style, ok)
	}
	if _, _, _, _, ok := ParseFilename("JOHN LEGEND-all of me-英语-流行.MKV"); !ok {
		t.Fatal("expected case-insensitive extension to be valid")
	}
	for _, name := range []string{
		"test.mkv",
		"only-three-parts.mkv",
		"too-many-parts-here-now.mkv",
		"empty--language-style.mkv",
		"notes.txt",
	} {
		if _, _, _, _, ok := ParseFilename(name); ok {
			t.Fatalf("expected %s to be invalid", name)
		}
	}
}

func TestScanSkipsInvalidAndDuplicates(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "rock")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	touch := func(path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	touch(filepath.Join(dir, "万晓利-女儿情-国语-流行.mkv"))
	touch(filepath.Join(dir, "test.mkv"))
	touch(filepath.Join(nested, "U2-with or without you-英语-流行.mkv"))

	cat, err := Open(filepath.Join(t.TempDir(), ".data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cat.Close()

	first, err := cat.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Added != 2 || first.SkippedInvalid != 1 || first.SkippedDuplicate != 0 {
		t.Fatalf("first scan: %+v", first)
	}

	second, err := cat.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second.Added != 0 || second.SkippedDuplicate != 2 || second.SkippedInvalid != 1 {
		t.Fatalf("second scan: %+v", second)
	}

	all, err := cat.Search("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("stored %d songs", len(all))
	}
	found, err := cat.Search("女儿")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Song != "女儿情" || found[0].Path != "万晓利-女儿情-国语-流行.mkv" {
		t.Fatalf("song search: %+v", found)
	}
	bySinger, err := cat.Search("u2")
	if err != nil {
		t.Fatal(err)
	}
	if len(bySinger) != 1 || bySinger[0].Path != "rock/U2-with or without you-英语-流行.mkv" {
		t.Fatalf("singer search: %+v", bySinger)
	}

	n, err := cat.Clear()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("cleared %d", n)
	}
	left, err := cat.Search("")
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("expected empty catalog, got %+v", left)
	}
}
