package markdown

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	src := "# Title\n\n## Getting `started`\n\nHello **world** [x](docs/a.md)\n\n```go\nfunc main() {}\n```\n\n<script>x</script>\n"
	r, err := Render(src, Options{LinkBase: "https://github.com/o/r/blob/main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.TOC) != 1 || r.TOC[0].Text != "Getting started" || r.TOC[0].ID != "getting-started" {
		t.Fatalf("toc = %+v", r.TOC)
	}
	if !strings.Contains(r.HTML, `class="chroma"`) {
		t.Fatalf("no chroma classes: %s", r.HTML)
	}
	if !strings.Contains(r.HTML, `href="https://github.com/o/r/blob/main/docs/a.md"`) {
		t.Fatalf("link not resolved: %s", r.HTML)
	}
	if strings.Contains(r.HTML, "<script>") {
		t.Fatal("untrusted render kept raw HTML")
	}
}

func TestSlugifyExcerpt(t *testing.T) {
	if got := Slugify("Hello, World! Go 1.27"); got != "hello-world-go-1-27" {
		t.Fatal(got)
	}
	if got := Excerpt("# T\n\nFirst [link](x) **bold** line.\nsecond\n\nnext para", 100); got != "First link bold line. second" {
		t.Fatal(got)
	}
}
