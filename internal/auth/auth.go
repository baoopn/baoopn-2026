// Package auth verifies Clerk session tokens and Clerk user API keys for the
// admin API and enforces the admin allow-list (user IDs, public_metadata.role,
// or verified emails).
// The Clerk session token should carry the custom claims
// {"role": "{{user.public_metadata.role}}", "email": "{{user.primary_email_address}}"}.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	clerkhttp "github.com/clerk/clerk-sdk-go/v2/http"
	"github.com/clerk/clerk-sdk-go/v2/user"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

// User is the authenticated admin.
type User struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	ImageURL string `json:"image_url"`
	Role     string `json:"role,omitempty"`
	// Via is how the request authenticated: "session" or "api_key".
	Via string `json:"-"`
}

type ctxKey struct{}

// errNoVerifiedEmail means the Clerk user exists but has no verified email
// (and no role/ID grant): always forbidden, never a claims fallback.
var errNoVerifiedEmail = errors.New("user has no verified email address")

// UserFrom returns the admin stored in the request context.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}

type tokenClaims struct {
	Role  string `json:"role"`
	Email string `json:"email"`
}

type cached struct {
	u   User
	exp time.Time
}

// Authenticator verifies tokens and checks the allow-list.
type Authenticator struct {
	emails, userIDs []string
	role            string
	parties         []string
	allowLoopback   bool
	log             *slog.Logger

	verifyKey keyVerifier
	fetchUser userFetcher
	now       func() time.Time

	mu       sync.Mutex
	cache    map[string]cached
	keyCache map[[32]byte]keyCached
}

// Config configures the authenticator.
type Config struct {
	SecretKey      string
	AllowedEmails  []string
	AllowedUserIDs []string
	AdminRole      string
	// PublicURL's origin is the authorized party (azp) for tokens.
	PublicURL string
	// Dev accepts tokens minted on any loopback origin.
	Dev bool
}

// New configures Clerk and returns the authenticator.
func New(c Config, log *slog.Logger) *Authenticator {
	clerk.SetKey(c.SecretKey)
	var emails []string
	for _, e := range c.AllowedEmails {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			emails = append(emails, e)
		}
	}
	var ids []string
	for _, id := range c.AllowedUserIDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	a := &Authenticator{emails: emails, userIDs: ids, role: c.AdminRole, allowLoopback: c.Dev,
		log: log, cache: map[string]cached{}, keyCache: map[[32]byte]keyCached{},
		verifyKey: verifyAPIKey, fetchUser: user.Get, now: time.Now}
	if u, err := url.Parse(c.PublicURL); err == nil && u.Host != "" {
		a.parties = []string{u.Scheme + "://" + u.Host}
	}
	return a
}

// Middleware requires a valid session token or user API key for an
// allow-listed user.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	verify := clerkhttp.WithHeaderAuthorization(
		clerkhttp.AuthorizationFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			a.log.WarnContext(r.Context(), "clerk token rejected")
			httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "sign in required")
		})),
		clerkhttp.Leeway(10*time.Second),
		clerkhttp.CustomClaimsConstructor(func(context.Context) any { return &tokenClaims{} }),
		clerkhttp.AuthorizedParty(a.partyAllowed),
	)
	session := verify(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := clerk.SessionClaimsFromContext(r.Context())
		if !ok || claims.Subject == "" {
			httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "sign in required")
			return
		}
		tc, _ := claims.Custom.(*tokenClaims)
		u, err := a.lookup(r.Context(), claims.Subject)
		switch {
		case err == nil:
			if tc != nil && tc.Role != "" {
				u.Role = tc.Role
			}
		case errors.Is(err, errNoVerifiedEmail):
			httpapi.Error(w, http.StatusForbidden, "forbidden", "this account has no verified email")
			return
		case tc != nil && tc.Role != "":
			// Clerk's Backend API is unreachable. The verified token's role
			// claim (from public_metadata, which users cannot edit) still
			// authorizes; its email claim is not trusted (may be unverified).
			a.log.WarnContext(r.Context(), "clerk lookup failed; authorizing from role claim", logging.Err(err))
			u = User{ID: claims.Subject, Role: tc.Role}
		default:
			a.log.ErrorContext(r.Context(), "clerk lookup failed", logging.Err(err))
			httpapi.Error(w, http.StatusBadGateway, "auth_unavailable", "could not verify the user with Clerk")
			return
		}
		if !a.allowed(u) {
			a.log.WarnContext(r.Context(), "admin access forbidden", slog.String("user", u.ID))
			httpapi.Error(w, http.StatusForbidden, "forbidden", "this account is not an admin")
			return
		}
		u.Via = "session"
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && strings.HasPrefix(tok, apiKeyPrefix) {
			a.apiKeyAuth(w, r, next, tok)
			return
		}
		session.ServeHTTP(w, r)
	})
}

func (a *Authenticator) partyAllowed(azp string) bool {
	if slices.Contains(a.parties, azp) {
		return true
	}
	// Browser session tokens always carry azp; tokenless-party tokens are
	// only tolerated in development.
	if azp == "" {
		return a.allowLoopback
	}
	if a.allowLoopback {
		if u, err := url.Parse(azp); err == nil {
			h := u.Hostname()
			return h == "localhost" || h == "127.0.0.1" || h == "::1"
		}
	}
	return false
}

func (a *Authenticator) allowed(u User) bool {
	if slices.Contains(a.userIDs, u.ID) {
		return true
	}
	if a.role != "" && u.Role == a.role {
		return true
	}
	return u.Email != "" && slices.Contains(a.emails, strings.ToLower(u.Email))
}

// lookup fetches the Clerk profile, cached in memory for 5 minutes.
func (a *Authenticator) lookup(ctx context.Context, id string) (User, error) {
	a.mu.Lock()
	if c, ok := a.cache[id]; ok && a.now().Before(c.exp) {
		a.mu.Unlock()
		return c.u, nil
	}
	a.mu.Unlock()

	cu, err := a.fetchUser(ctx, id)
	if err != nil {
		return User{}, err
	}
	u := User{ID: cu.ID}
	if cu.Banned || cu.Locked {
		return u, nil // never allowed: empty email and role
	}
	u.Role = metadataRole(cu.PublicMetadata)
	if cu.ImageURL != nil {
		u.ImageURL = *cu.ImageURL
	}
	var names []string
	if cu.FirstName != nil {
		names = append(names, *cu.FirstName)
	}
	if cu.LastName != nil {
		names = append(names, *cu.LastName)
	}
	u.Name = strings.Join(names, " ")
	for _, e := range cu.EmailAddresses {
		if e == nil || e.Verification == nil || e.Verification.Status != "verified" {
			continue
		}
		if u.Email == "" || (cu.PrimaryEmailAddressID != nil && e.ID == *cu.PrimaryEmailAddressID) {
			u.Email = e.EmailAddress
		}
	}
	if u.Email == "" && u.Role == "" && len(a.userIDs) == 0 {
		return u, errNoVerifiedEmail
	}
	a.mu.Lock()
	a.cache[id] = cached{u: u, exp: a.now().Add(5 * time.Minute)}
	a.mu.Unlock()
	return u, nil
}

func metadataRole(raw json.RawMessage) string {
	var m struct {
		Role string `json:"role"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &m) == nil {
		return m.Role
	}
	return ""
}

// MeHandler serves GET /api/admin/me.
func MeHandler(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	httpapi.JSON(w, http.StatusOK, u)
}
