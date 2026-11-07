package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// GitHubError is a failed GitHub API call.
type GitHubError struct {
	Status      int // 0 for transport errors
	RateLimited bool
	Reset       time.Time
	Err         error
}

func (e *GitHubError) Error() string {
	if e.RateLimited {
		return "github: rate limited until " + e.Reset.UTC().Format(time.RFC3339)
	}
	if e.Err != nil {
		return "github: " + e.Err.Error()
	}
	return "github: HTTP " + strconv.Itoa(e.Status)
}

func (e *GitHubError) Unwrap() error { return e.Err }

// ghRepo is the subset of the GitHub repository object we store.
type ghRepo struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	FullName      string     `json:"full_name"`
	HTMLURL       string     `json:"html_url"`
	Homepage      string     `json:"homepage"`
	Description   string     `json:"description"`
	Language      string     `json:"language"`
	Topics        []string   `json:"topics"`
	Stars         int        `json:"stargazers_count"`
	Forks         int        `json:"forks_count"`
	Archived      bool       `json:"archived"`
	Fork          bool       `json:"fork"`
	Private       bool       `json:"private"`
	PushedAt      *time.Time `json:"pushed_at"`
	CreatedAt     *time.Time `json:"created_at"`
	DefaultBranch string     `json:"default_branch"`
}

// ghClient is a minimal GitHub REST client that tracks the rate limit.
type ghClient struct {
	http      *http.Client
	base      string
	token     string
	remaining atomic.Int64 // -1 = unknown
}

const maxRepoPages = 10

func newGHClient(hc *http.Client, base, token string) *ghClient {
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	if base == "" {
		base = "https://api.github.com"
	}
	c := &ghClient{http: hc, base: strings.TrimRight(base, "/"), token: token}
	c.remaining.Store(-1)
	return c
}

// get performs a GET; the caller closes the body of a non-nil response.
// Non-2xx statuses other than those in ok are returned as *GitHubError.
func (c *ghClient) get(ctx context.Context, rawURL, accept, etag string, ok ...int) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, &GitHubError{Err: err}
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "baoopn-api")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &GitHubError{Err: err}
	}
	if v, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Remaining"), 10, 64); err == nil {
		c.remaining.Store(v)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	for _, s := range ok {
		if resp.StatusCode == s {
			return resp, nil
		}
	}
	defer drain(resp)
	ge := &GitHubError{Status: resp.StatusCode}
	if resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0") {
		ge.RateLimited = true
		ge.Reset = time.Now().Add(time.Minute)
		if sec, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			ge.Reset = time.Unix(sec, 0)
		} else if sec, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			ge.Reset = time.Now().Add(time.Duration(sec) * time.Second)
		}
	}
	return nil, ge
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

// lowOnQuota reports whether fewer than 3 calls remain in the window.
func (c *ghClient) lowOnQuota() bool {
	r := c.remaining.Load()
	return r >= 0 && r < 3
}

// listRepos lists the user's repos, following Link rel="next" pages. A 304
// on the first page (etag match) returns notModified.
func (c *ghClient) listRepos(ctx context.Context, user, etag string) (repos []ghRepo, newEtag string, notModified bool, err error) {
	next := fmt.Sprintf("%s/users/%s/repos?type=owner&sort=pushed&per_page=100&page=1", c.base, url.PathEscape(user))
	for page := 0; next != "" && page < maxRepoPages; page++ {
		tag := ""
		if page == 0 {
			tag = etag
		}
		resp, err := c.get(ctx, next, "application/vnd.github+json", tag, http.StatusNotModified)
		if err != nil {
			return nil, "", false, err
		}
		if resp.StatusCode == http.StatusNotModified {
			drain(resp)
			return nil, etag, true, nil
		}
		if page == 0 {
			newEtag = resp.Header.Get("ETag")
		}
		var batch []ghRepo
		err = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&batch)
		link := resp.Header.Get("Link")
		drain(resp)
		if err != nil {
			return nil, "", false, &GitHubError{Status: resp.StatusCode, Err: errors.New("decode repos")}
		}
		repos = append(repos, batch...)
		next = nextLink(link)
		if next != "" && !strings.HasPrefix(next, c.base+"/") {
			next = "" // never send the token to another host
		}
	}
	return repos, newEtag, false, nil
}

// nextLink extracts the rel="next" URL from a Link header.
func nextLink(h string) string {
	for _, part := range strings.Split(h, ",") {
		segs := strings.Split(part, ";")
		if len(segs) < 2 {
			continue
		}
		u := strings.TrimSpace(segs[0])
		if !strings.HasPrefix(u, "<") || !strings.HasSuffix(u, ">") {
			continue
		}
		for _, s := range segs[1:] {
			s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
			if s == `rel="next"` || s == "rel=next" {
				return u[1 : len(u)-1]
			}
		}
	}
	return ""
}

func repoPath(full string) string {
	parts := strings.SplitN(full, "/", 2)
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// readme returns the raw README ("" when the repo has none).
func (c *ghClient) readme(ctx context.Context, full string) (string, error) {
	resp, err := c.get(ctx, c.base+"/repos/"+repoPath(full)+"/readme", "application/vnd.github.raw", "", http.StatusNotFound)
	if err != nil {
		return "", err
	}
	defer drain(resp)
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", &GitHubError{Err: errors.New("read readme")}
	}
	return string(b), nil
}

// languages returns bytes per language.
func (c *ghClient) languages(ctx context.Context, full string) (map[string]int64, error) {
	resp, err := c.get(ctx, c.base+"/repos/"+repoPath(full)+"/languages", "application/vnd.github+json", "")
	if err != nil {
		return nil, err
	}
	defer drain(resp)
	out := map[string]int64{}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, &GitHubError{Err: errors.New("decode languages")}
	}
	return out, nil
}

// readmeBases returns the link and image bases for rendering a README.
func readmeBases(full, branch string) (link, image string) {
	if branch == "" {
		branch = "HEAD"
	}
	return "https://github.com/" + full + "/blob/" + branch, "https://raw.githubusercontent.com/" + full + "/" + branch
}
