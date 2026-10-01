package crawler

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func logger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

func newTestServer(t *testing.T, pages map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, html := range pages {
		path, html := path, html
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != path {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, html)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestCrawler_Basic(t *testing.T) {
	srv := newTestServer(t, map[string]string{"/": `<html><head><title>Home</title></head><body>Hello</body></html>`})
	c := New(logger(), 2, 5*time.Second)
	roots := c.Run(context.Background(), []string{srv.URL})

	root, ok := roots[srv.URL]
	if !ok {
		t.Fatalf("no root for %s", srv.URL)
	}
	if root.Title != "Home" {
		t.Errorf("title: expected Home, but got: %q", root.Title)
	}
	if len(root.Links) != 0 {
		t.Errorf("links: expected 0, but got: %d", len(root.Links))
	}
}

func TestCrawler_FollowsLinksInSameDomain(t *testing.T) {
	srv := newTestServer(t, map[string]string{
		"/":        `<html><head><title>Home</title></head><body><a href="/about">About</a><a href="/contact">Contact</a></body></html>`,
		"/about":   `<html><head><title>About</title></head><body></body></html>`,
		"/contact": `<html><head><title>Contact</title></head><body></body></html>`,
	})

	c := New(logger(), 2, 5*time.Second)
	roots := c.Run(context.Background(), []string{srv.URL})

	root := roots[srv.URL]
	if root == nil {
		t.Fatal("no root")
	}
	if root.Title != "Home" {
		t.Errorf("root title: expected Home, but got %q", root.Title)
	}
	if len(root.Links) != 2 {
		t.Fatalf("links: expected 2, but got %d", len(root.Links))
	}

	titles := map[string]bool{}
	for _, child := range root.Links {
		titles[child.Title] = true
	}
	if !titles["About"] || !titles["Contact"] {
		t.Errorf("no children found: %v", titles)
	}
}

func TestCrawler_SkipsExternalLinks(t *testing.T) {
	srv := newTestServer(t, map[string]string{
		"/": `<html><head><title>Home</title></head>
		<body>
			<a href="https://google.com">Google</a>
			<a href="/internal">Internal</a>
		</body></html>`,
		"/internal": `<html><head><title>Internal</title></head><body></body></html>`,
	})

	c := New(logger(), 2, 5*time.Second)
	roots := c.Run(context.Background(), []string{srv.URL})

	root := roots[srv.URL]
	if len(root.Links) != 1 {
		t.Fatalf("links: expected 1, but got %d", len(root.Links))
	}
	if root.Links[0].Title != "Internal" {
		t.Errorf("links: expected Internal, but got %q", root.Links[0].Title)
	}
}

func TestCrawler_SkipsNonHTML(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Home</title></head>
		<body><a href="/image.png">Image</a></body></html>`)
	})
	mux.HandleFunc("/image.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake png"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(logger(), 2, 5*time.Second)
	roots := c.Run(context.Background(), []string{srv.URL})

	root := roots[srv.URL]
	if len(root.Links) != 0 {
		t.Errorf("links: expected 0, but got %d", len(root.Links))
	}
}

func TestCrawler_SkipRedirects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Home</title></head>
		<body><a href="/old">Old</a></body></html>`)
	})

	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/new", http.StatusMovedPermanently)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(logger(), 2, 5*time.Second)
	roots := c.Run(context.Background(), []string{srv.URL})

	root := roots[srv.URL]
	if len(root.Links) != 0 {
		t.Errorf("links: expected 0, but got: %d", len(root.Links))
	}
}

func TestCrawler_NoDuplicateRequests(t *testing.T) {
	var counter int64
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&counter, 1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Home</title></head>
		<body>
			<a href="/a">A</a>
			<a href="/b">B</a>
			<a href="/a">A again</a>
		</body></html>`)
	})
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&counter, 1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>A</title></head><body></body></html>`)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&counter, 1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>B</title></head><body></body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(logger(), 2, 5*time.Second)
	c.Run(context.Background(), []string{srv.URL})

	if got := atomic.LoadInt64(&counter); got != 3 {
		t.Errorf("requests: expected 3, but got %d", got)
	}
}

func TestCrawler_PartialFailure(t *testing.T) {
	srv := newTestServer(t, map[string]string{
		"/": `<html><head><title>Home</title></head>
		<body>
			<a href="/broken">Broken</a>
			<a href="/ok">OK</a>
		</body></html>`,
		"/ok": `<html><head><title>OK</title></head><body></body></html>`,
	})

	c := New(logger(), 2, 5*time.Second)
	roots := c.Run(context.Background(), []string{srv.URL})

	root := roots[srv.URL]
	if root == nil {
		t.Fatal("no root")
	}
	if len(root.Links) != 1 {
		t.Fatalf("links: expected 1, but got %d", len(root.Links))
	}
	if root.Links[0].Title != "OK" {
		t.Errorf("expected OK, but got %q", root.Links[0].Title)
	}
}

func TestCrawler_DuplicatesChildren(t *testing.T) {
	srv := newTestServer(t, map[string]string{
		"/": `<html><head><title>Home</title></head><body>
			<a href="/a">A</a>
			<a href="/a">A again</a>
			<a href="/a">A third time</a>
		</body></html>`,
		"/a": `<html><head><title>A</title></head><body></body></html>`,
	})

	c := New(logger(), 2, 5*time.Second)
	roots := c.Run(context.Background(), []string{srv.URL})

	root := roots[srv.URL]

	if root == nil {
		t.Fatal("no root")
	}

	if len(roots) != 1 {
		t.Fatalf("roots: expected 1 link, but got: %d", len(roots))
	}

	if root.Links[0].Title != "A" {
		t.Errorf("child title: expected 'A', but got: %q", root.Links[0].Title)
	}
}
