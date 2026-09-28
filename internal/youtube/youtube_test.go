package youtube

import "testing"

func TestParseID(t *testing.T) {
	id, err := ParseID("https://www.youtube.com/watch?v=7wtfhZwyrcc&list=RD7wtfhZwyrcc&start_radio=1")
	if err != nil || id != "7wtfhZwyrcc" {
		t.Fatalf("watch url: %q %v", id, err)
	}
	id, err = ParseID("https://youtu.be/7wtfhZwyrcc")
	if err != nil || id != "7wtfhZwyrcc" {
		t.Fatalf("short url: %q %v", id, err)
	}
	id, err = ParseID("https://www.youtube.com/shorts/7wtfhZwyrcc")
	if err != nil || id != "7wtfhZwyrcc" {
		t.Fatalf("shorts: %q %v", id, err)
	}
	if _, err := ParseID("https://vimeo.com/123"); err == nil {
		t.Fatal("expected a non-YouTube URL to be rejected")
	}
	if _, ok := ID(File("7wtfhZwyrcc")); !ok {
		t.Fatal("expected stored file to round-trip")
	}
}
