package parser

import (
	"io"
	"strings"

	"golang.org/x/net/html"
)

func extractTitle(doc *html.Node) string {
	var title string
	var bypass func(*html.Node)

	bypass = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" {
			if n.FirstChild != nil {
				title = strings.TrimSpace(n.FirstChild.Data)
			}
			return
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			bypass(c)
		}
	}

	bypass(doc)
	return title
}

func extractLinks(doc *html.Node) []string {
	links := make([]string, 0)
	var bypass func(*html.Node)

	bypass = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					val := strings.TrimSpace(attr.Val)
					if val != "" {
						links = append(links, val)
					}
					break
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			bypass(c)
		}
	}
	bypass(doc)
	return links
}

func ParseHTML(r io.Reader) (string, []string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", nil, err
	}

	title := extractTitle(doc)
	links := extractLinks(doc)
	return title, links, nil
}
