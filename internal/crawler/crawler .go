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
	"sync/atomic"
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
	pending        atomic.Int64
	pendingRes     atomic.Int64
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

func (c *Crawler) markVisited(u string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.visited[u] {
		return false
	}
	c.visited[u] = true
	return true
}

func (c *Crawler) worker(ctx context.Context, id int, jobs <-chan Job, newJobs chan<- Job, results chan<- pageResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}
			c.processURL(ctx, job, newJobs, results)
			c.pending.Add(-1)
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
		c.logger.Printf("[ERROR] %s: %v", job.URL, err)
		return
	}

	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.Printf("[ERROR] %s: %v", job.URL, err)
		return
	}
	defer resp.Body.Close()

	c.logger.Printf("[INFO] %s: http code: %d", job.URL, resp.StatusCode)

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		c.logger.Printf("[WARN] %s: redirect, skipping", job.URL)
		return
	}
	if resp.StatusCode != http.StatusOK {
		c.logger.Printf("[WARN] %s: status %d, skipping", job.URL, resp.StatusCode)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		c.logger.Printf("[WARN] %s: not html, skipping", job.URL)
		return
	}

	title, links, err := parser.ParseHTML(resp.Body)
	if err != nil {
		c.logger.Printf("[ERROR] %s: parsing: %v", job.URL, err)
		return
	}

	c.logger.Printf("[INFO] %s: title=%q links=%d depth=%d", job.URL, title, len(links), job.Depth)

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

	c.pendingRes.Add(1)
	select {
	case <-ctx.Done():
		c.pendingRes.Add(-1)
		return
	case results <- pageResult{
		node: &models.Node{
			Resource: job.URL,
			Title:    title,
			Links:    make([]*models.Node, 0),
		},
		links: absoluteLinks,
	}:
	}

	for _, nj := range nextJobs {
		c.pending.Add(1)
		select {
		case <-ctx.Done():
			c.pending.Add(-1)
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

	if strings.HasPrefix(link, "mailto:") ||
		strings.HasPrefix(link, "javascript:") ||
		strings.HasPrefix(link, "data:") ||
		strings.HasPrefix(link, "tel:") {
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

func sameDomain(base *url.URL, target string) bool {
	parsed, err := url.Parse(target)
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
		go func(id int) {
			c.worker(ctx, id, jobs, newJobs, results, &workersWG)
		}(id)
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

		c.pending.Store(int64(len(startURLs)))
		c.pendingRes.Store(0)

		for _, u := range startURLs {
			select {
			case jobs <- Job{URL: u, Depth: 0}:
			case <-ctx.Done():
				return
			}
		}

		pollTick := time.NewTicker(50 * time.Millisecond)
		defer pollTick.Stop()

		for {
			p := c.pending.Load()
			r := c.pendingRes.Load()
			if p == 0 && r == 0 {
				return
			}

			select {
			case <-ctx.Done():
				return

			case res, ok := <-results:
				if !ok {
					return
				}
				c.pendingRes.Add(-1)
				nodesByURL[res.node.Resource] = res.node
				linksByURL[res.node.Resource] = res.links

			case j, ok := <-newJobs:
				if !ok {
					newJobs = nil
					continue
				}
			put:
				for {
					select {
					case jobs <- j:
						break put
					case res, ok := <-results:
						if !ok {
							return
						}
						c.pendingRes.Add(-1)
						nodesByURL[res.node.Resource] = res.node
						linksByURL[res.node.Resource] = res.links
					case <-ctx.Done():
						return
					}
				}

			case <-pollTick.C:
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
