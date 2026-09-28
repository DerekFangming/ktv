package library

import (
	"fmt"
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

	all, err := cat.Search("", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 2 || len(all.Songs) != 2 || all.Limit != PageSize {
		t.Fatalf("stored %+v", all)
	}
	found, err := cat.Search("女儿", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if found.Total != 1 || len(found.Songs) != 1 || found.Songs[0].Song != "女儿情" || found.Songs[0].Path != "万晓利-女儿情-国语-流行.mkv" {
		t.Fatalf("song search: %+v", found)
	}
	bySinger, err := cat.Search("u2", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if bySinger.Total != 1 || len(bySinger.Songs) != 1 || bySinger.Songs[0].Path != "rock/U2-with or without you-英语-流行.mkv" {
		t.Fatalf("singer search: %+v", bySinger)
	}

	n, err := cat.Clear()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("cleared %d", n)
	}
	left, err := cat.Search("", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if left.Total != 0 || len(left.Songs) != 0 {
		t.Fatalf("expected empty catalog, got %+v", left)
	}
}

func TestSearchPage(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 25; i++ {
		name := filepath.Join(dir, fmt.Sprintf("Singer-Song%02d-国语-流行.mkv", i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cat, err := Open(filepath.Join(t.TempDir(), ".data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cat.Close()
	if _, err := cat.Scan(dir); err != nil {
		t.Fatal(err)
	}

	first, err := cat.Search("", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 25 || len(first.Songs) != 20 || first.Offset != 0 {
		t.Fatalf("first page: total=%d len=%d offset=%d", first.Total, len(first.Songs), first.Offset)
	}
	second, err := cat.Search("", 20, 20)
	if err != nil {
		t.Fatal(err)
	}
	if second.Total != 25 || len(second.Songs) != 5 || second.Songs[0].Path == first.Songs[0].Path {
		t.Fatalf("second page: %+v", second)
	}
}
