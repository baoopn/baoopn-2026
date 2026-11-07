package profile

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
	"time"
)

// errUpstream marks GitHub failures other than rate limiting.
var errUpstream = errors.New("profile: github unavailable")

// rateLimitError is returned when GitHub's rate limit is exhausted.
type rateLimitError struct{ reset time.Time }

func (e *rateLimitError) Error() string {
	return "github rate limited until " + e.reset.UTC().Format(time.RFC3339)
}

type ghUser struct {
	Login    string `json:"login"`
	Name     string `json:"name"`
	Bio      string `json:"bio"`
	Location string `json:"location"`
	Avatar   string `json:"avatar_url"`
	Email    string `json:"email"`
	Blog     string `json:"blog"`
	HTMLURL  string `json:"html_url"`
}

// fetchGitHub loads the public GitHub user profile.
func (m *Module) fetchGitHub(ctx context.Context) (ghUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.apiBase+"/users/"+url.PathEscape(m.ghUser), nil)
	if err != nil {
		return ghUser{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "baoopn-api")
	if m.ghToken != "" {
		req.Header.Set("Authorization", "Bearer "+m.ghToken)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return ghUser{}, fmt.Errorf("%w: %v", errUpstream, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
	}()
	if resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0") {
		reset := time.Now().Add(time.Minute)
		if sec, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			reset = time.Unix(sec, 0)
		}
		return ghUser{}, &rateLimitError{reset: reset}
	}
	if resp.StatusCode != http.StatusOK {
		return ghUser{}, fmt.Errorf("%w: HTTP %d", errUpstream, resp.StatusCode)
	}
	var u ghUser
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&u); err != nil {
		return ghUser{}, fmt.Errorf("%w: decode: %v", errUpstream, err)
	}
	return u, nil
}

// partialFromGitHub maps a GitHub user to the Partial<Profile> the admin
// form merges in. Empty values are left out.
func partialFromGitHub(u ghUser) map[string]any {
	out := map[string]any{}
	put := func(k, v string) {
		if v = strings.TrimSpace(v); v != "" {
			out[k] = v
		}
	}
	put("name", u.Name)
	put("headline", u.Bio)
	put("location", u.Location)
	if validURL(u.Avatar) {
		put("avatar_url", u.Avatar)
	}
	if validEmail(strings.TrimSpace(u.Email)) {
		put("email", u.Email)
	}
	socials := []SocialLink{}
	if u.HTMLURL != "" && socialURL(u.HTMLURL) {
		socials = append(socials, SocialLink{Label: "GitHub", URL: u.HTMLURL, Icon: "github", Handle: "@" + u.Login})
	}
	if blog := strings.TrimSpace(u.Blog); blog != "" {
		if !strings.Contains(blog, "://") {
			blog = "https://" + blog
		}
		if socialURL(blog) {
			socials = append(socials, SocialLink{Label: "Website", URL: blog, Icon: "website"})
		}
	}
	if len(socials) > 0 {
		out["socials"] = socials
	}
	return out
}
