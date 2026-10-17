package text

import "testing"

func TestSlugify(t *testing.T) {
	if got := Slugify("Hello, World!"); got != "hello-world" {
		t.Fatal(got)
	}
}
