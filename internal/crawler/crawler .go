package crawler

import (
	"context"
	"crawler-api/internal/models"
	"crawler-api/internal/parser"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defMaxWorkers = 10
	jobsBuffer    = 100
	resultBuffer  = 100
)

type Crawler struct {
	client         *http.Client
	logger         *log.Logger
	maxDepth       int
	maxWorkers     int
	requestTimeout time.Duration
	visited        map[string]bool
	mu             sync.Mutex
}

type Job struct {
	URL   string
	Depth int
}

type pageResult struct {
	node  *models.Node
	links []string
}

func New(logger *log.Logger, maxDepth int, reqTimeout time.Duration) *Crawler {
	client := &http.Client{
		Timeout: reqTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &Crawler{
		client:         client,
		logger:         logger,
		maxDepth:       maxDepth,
		maxWorkers:     defMaxWorkers,
		requestTimeout: reqTimeout,
		visited:        make(map[string]bool),
	}
}

func (c *Crawler) markVisited(url string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.visited[url] {
		return false
	}
	c.visited[url] = true
	return true
}

func (c *Crawler) worker(ctx context.Context, id int, jobs chan Job, newJobs chan<- Job, results chan<- pageResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			c.logger.Printf("[INFO] worker %d terminates upon receiving a signal ctx", id)
			return
		case job, ok := <-jobs:
			if !ok {
				c.logger.Printf("[INFO] worker %d: the channel is closed", id)
				return
			}
			c.processURL(ctx, job, newJobs, results)
		}
	}
}

func (c *Crawler) processURL(ctx context.Context, job Job, newJobs chan<- Job, results chan<- pageResult) {
	if !c.markVisited(job.URL) {
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, job.URL, nil)
	if err != nil {
		c.logger.Printf("[ERROR] %s: error of creating a timeout: %v", job.URL, err)
		return
	}

	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.Printf("[ERROR] %s: request: %v", job.URL, err)
		return
	}
	defer resp.Body.Close()

	c.logger.Printf("[INFO] %s: http code: %d", job.URL, resp.StatusCode)

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		c.logger.Printf("[WARN] %s: redirect on %s, skipping", job.URL, resp.Header.Get("Location"))
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.logger.Printf("[WARN] %s: status: %d, skipping", job.URL, resp.StatusCode)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		c.logger.Printf("[WARN] %s: not html: %s, skipping", job.URL, contentType)
		return
	}

	title, links, err := parser.ParseHTML(resp.Body)
	if err != nil {
		c.logger.Printf("[ERROR] %s: parsing: %v", job.URL, err)
		return
	}

	c.logger.Printf("[INFO] %s: title: %s; links count: %d; depth: %d", job.URL, title, len(links), job.Depth)

	absoluteLinks := make([]string, 0)
	nextJobs := make([]Job, 0)

	if job.Depth < c.maxDepth {
		baseURL, err := url.Parse(job.URL)
		if err != nil {
			c.logger.Printf("[ERROR] %s: base parse URL: %v", job.URL, err)
		} else {
			for _, link := range links {
				absolute, ok := resolveURL(baseURL, link)
				if !ok {
					continue
				}
				if !sameDomain(baseURL, absolute) {
					continue
				}

				absoluteLinks = append(absoluteLinks, absolute)
				nextJobs = append(nextJobs, Job{URL: absolute, Depth: job.Depth + 1})
			}
		}

	}

	result := pageResult{
		node: &models.Node{
			Resource: job.URL,
			Title:    title,
			Links:    make([]*models.Node, 0)},
		links: absoluteLinks,
	}

	select {
	case <-ctx.Done():
		return
	case results <- result:
	}

	for _, nj := range nextJobs {
		select {
		case <-ctx.Done():
			return
		case newJobs <- nj:
		}
	}
}

func resolveURL(base *url.URL, link string) (string, bool) {
	link = strings.TrimSpace(link)
	if link == "" {
		return "", false
	}

	if strings.HasPrefix(link, "mailto:") || strings.HasPrefix(link, "javascript:") || strings.HasPrefix(link, "data:") || strings.HasPrefix(link, "tel:") {
		return "", false
	}

	if strings.HasPrefix(link, "#") {
		return "", false
	}

	parsed, err := url.Parse(link)
	if err != nil {
		return "", false
	}

	absolute := base.ResolveReference(parsed)
	if absolute.Scheme != "http" && absolute.Scheme != "https" {
		return "", false
	}

	return absolute.String(), true
}

func sameDomain(base *url.URL, link string) bool {
	parsed, err := url.Parse(link)
	if err != nil {
		return false
	}
	return base.Host == parsed.Host
}

func (c *Crawler) Run(ctx context.Context, startURLs []string) map[string]*models.Node {
	jobs := make(chan Job, jobsBuffer)
	newJobs := make(chan Job, jobsBuffer)
	results := make(chan pageResult, resultBuffer)

	var workersWG sync.WaitGroup
	var schedulerWG sync.WaitGroup

	for id := 1; id <= c.maxWorkers; id++ {
		workersWG.Add(1)
		go func() {
			c.worker(ctx, id, jobs, newJobs, results, &workersWG)
		}()
	}

	go func() {
		workersWG.Wait()
		close(newJobs)
	}()

	nodesByURL := make(map[string]*models.Node)
	linksByURL := make(map[string][]string)

	schedulerWG.Add(1)
	go func() {
		defer schedulerWG.Done()
		defer close(jobs)
		defer close(results)

		pending := len(startURLs)

		for _, u := range startURLs {
			select {
			case jobs <- Job{URL: u, Depth: 0}:
			case <-ctx.Done():
				return
			}
		}

		for pending > 0 {
			select {
			case <-ctx.Done():
				return

			case res, ok := <-results:
				if !ok {
					continue
				}
				nodesByURL[res.node.Resource] = res.node
				linksByURL[res.node.Resource] = res.links
				pending--

			case j, ok := <-newJobs:
				if !ok {
					continue
				}
				pending++
			put:
				for {
					select {
					case jobs <- j:
						break put
					case res, ok := <-results:
						if !ok {
							continue
						}
						nodesByURL[res.node.Resource] = res.node
						linksByURL[res.node.Resource] = res.links
						pending--
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	schedulerWG.Wait()
	roots := make(map[string]*models.Node, len(startURLs))
	for _, u := range startURLs {
		root, ok := nodesByURL[u]
		if !ok {
			root = &models.Node{Resource: u, Links: make([]*models.Node, 0)}
		}
		roots[u] = BuildTree(root, nodesByURL, linksByURL, make(map[string]bool))
	}

	return roots
}

func BuildTree(node *models.Node, nodesByURL map[string]*models.Node, linksByURL map[string][]string, visited map[string]bool) *models.Node {
	if visited[node.Resource] {
		return &models.Node{
			Resource: node.Resource,
			Title:    node.Title,
			Links:    make([]*models.Node, 0),
		}
	}

	visited[node.Resource] = true

	out := &models.Node{
		Resource: node.Resource,
		Title:    node.Title,
		Links:    make([]*models.Node, 0),
	}

	seenChildren := make(map[string]bool)

	for _, link := range linksByURL[node.Resource] {
		if seenChildren[link] {
			continue
		}
		seenChildren[link] = true

		child, ok := nodesByURL[link]
		if !ok {
			continue
		}
		out.Links = append(out.Links, BuildTree(child, nodesByURL, linksByURL, visited))
	}

	return out
}
