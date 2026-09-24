package parser

import (
	"strings"
	"testing"
)

func TestParseHTML_Basic(t *testing.T) {
	htmlInput := `<html>
	<head><title>Test</title></head>
	<body>
	  <a href="/about">About</a>
	  <div>
	    <a href="https://google.com">Гугл</a>
	  </div>
	</body>
	</html>`

	title, links, err := ParseHTML(strings.NewReader(htmlInput))
	if err != nil {
		t.Fatalf("expected no error, but got: %v", err)
	}

	if title != "Test" {
		t.Errorf("title: expected %s, but got: %s", "Test", title)
	}

	expectedLinks := []string{"/about", "https://google.com"}
	if len(links) != len(expectedLinks) {
		t.Fatalf("links: expected %d links, but got: %d", len(expectedLinks), len(links))
	}

	for i, l := range links {
		if l != expectedLinks[i] {
			t.Errorf("links[%d]: expected: %s but got: %s", i, expectedLinks[i], l)
		}
	}
}

func TestParseHTML_EmptyTitle(t *testing.T) {
	htmlInput := `<html><head></head><body><p>Hello</p></body></html>`

	title, _, err := ParseHTML(strings.NewReader(htmlInput))
	if err != nil {
		t.Fatalf("expected no error, but got: %v", err)
	}

	if title != "" {
		t.Errorf("expected empty title, but got: %s", title)
	}
}

func TestParseHTML_NoLinks(t *testing.T) {
	htmlInput := `<html><head>title>Test</title></head><body><p>Hello</p></body></html>`

	_, links, err := ParseHTML(strings.NewReader(htmlInput))
	if err != nil {
		t.Fatalf("expected no error, but got: %v", err)
	}

	if len(links) != 0 {
		t.Errorf("expected 0 links, but got: %d", len(links))
	}

	if links == nil {
		t.Error("links must be empty slice, not nil")
	}
}

func TestParseHTML_UpperCaseTags(t *testing.T) {
	htmlInput := `<HTML><HEAD><TITLE>Test</TITLE></HEAD><BODY><A HREF="/x">X</A></BODY></HTML>`

	title, links, err := ParseHTML(strings.NewReader(htmlInput))
	if err != nil {
		t.Fatalf("expected no error, but got: %v", err)
	}

	if title != "Test" {
		t.Errorf("title: expected %s but got: %s", "Test", title)
	}

	if len(links) != 1 || links[0] != "/x" {
		t.Errorf("links: expected [/x], but got: %v", links)
	}
}

func TestParseHTML_EmptyHref(t *testing.T) {
	htmlInput := `<html><head><title>T</title></head>
	<body>
	  <a href="">пустая</a>
	  <a href="/valid">валидная</a>
	</body></html>`

	_, links, err := ParseHTML(strings.NewReader(htmlInput))
	if err != nil {
		t.Fatalf("expected no error, but got: %v", err)
	}

	if len(links) != 1 {
		t.Errorf("expected 1 link, but got: %d", len(links))
	}

	if links[0] != "/valid" {
		t.Errorf("links[0]: expected [/valid], but got: %s", links[0])
	}
}

func TestParseHTML_AnchorWithoutHref(t *testing.T) {
	htmlInput := `<html><head><title>Test</title></head>
	<body><a name="anchor">without link</a></body></html>`

	_, links, err := ParseHTML(strings.NewReader(htmlInput))
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	if len(links) != 0 {
		t.Errorf("expected 0 links, but got: %v", links)
	}
}

func testParseHTML_MultipleHref(t *testing.T) {
	htmlInput := `<html><head><title>T</title></head><body><a href="/first" href="/second">X</a></body></html>`

	_, links, err := ParseHTML(strings.NewReader(htmlInput))
	if err != nil {
		t.Fatalf("expected no error, but got: %v", err)
	}

	if len(links) != 1 || links[0] != "/first" {
		t.Errorf("links: expected [/first], but got: %s", links[0])
	}
}

func TestParseHTML_NestedLinks(t *testing.T) {
	htmlInput := `<html><head><title>Test</title></head>
	<body>
	  <div>
	    <section>
	      <article>
	        <a href="/deep">Deep</a>
	      </article>
	    </section>
	  </div>
	  <a href="/top">Top</a>
	</body></html>`

	_, links, err := ParseHTML(strings.NewReader(htmlInput))
	if err != nil {
		t.Fatalf("expected no error, but got: %v", err)
	}

	if len(links) != 2 {
		t.Errorf("links: expected 2 links, but got %d: %v", len(links), links)
	}

	if links[0] != "/deep" {
		t.Errorf("links: expected [/deep], but got: %s", links[0])
	}

	if links[1] != "/top" {
		t.Errorf("links: expected [/top], but got: %s", links[1])
	}
}
