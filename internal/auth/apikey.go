package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/apikey"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
)

const (
	// apiKeyPrefix marks a Clerk API key secret; session JWTs start with "eyJ".
	apiKeyPrefix = "ak_"
	// keyCacheTTL bounds how long a revoked key keeps working.
	keyCacheTTL = 60 * time.Second
	keyCacheMax = 256
)

type keyVerifier func(ctx context.Context, secret string) (*clerk.APIKey, error)

type userFetcher func(ctx context.Context, id string) (*clerk.User, error)

// keyCached is a verified key, stored under the SHA-256 of its secret.
type keyCached struct {
	userID, keyID string
	exp           time.Time
}

func verifyAPIKey(ctx context.Context, secret string) (*clerk.APIKey, error) {
	return apikey.Verify(ctx, &apikey.VerifyParams{Secret: secret})
}

// apiKeyAuth authenticates a request that carries a Clerk user API key. It
// never logs the secret, the header or a raw error message.
//
// A cached valid key is never limited. Otherwise a client over
// keyFailuresPerMinute gets 429; a key of the wrong shape, or one Clerk
// refused within rejectTTL, is refused without asking Clerk. Clerk failures
// (401, 403, 429, 5xx, network) are 502 and neither cached nor counted.
func (a *Authenticator) apiKeyAuth(w http.ResponseWriter, r *http.Request, next http.Handler, secret string) {
	ctx := r.Context()
	userID, keyID, ok := a.cachedKey(secret)
	if !ok {
		client, now := httpapi.ClientBucket(r), a.now()
		if a.keyFails.blocked(client, now) {
			w.Header().Set("Retry-After", "60")
			httpapi.Error(w, http.StatusTooManyRequests, "rate_limited", "too many invalid API keys")
			return
		}
		if !apiKeyShape.MatchString(secret) || a.keyRefused(secret) {
			a.keyFails.fail(client, now)
			httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid API key")
			return
		}
		k, err := a.verifyKey(ctx, secret)
		if err != nil {
			var apiErr *clerk.APIErrorResponse
			if errors.As(err, &apiErr) && keyRejected(apiErr.HTTPStatusCode) {
				a.log.WarnContext(ctx, "clerk api key rejected",
					slog.Int("status", apiErr.HTTPStatusCode), slog.String("clerk_trace_id", apiErr.TraceID))
				a.refuse(secret, client, now)
				httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid API key")
				return
			}
			attrs := []any{slog.String("err_type", fmt.Sprintf("%T", err))}
			if apiErr != nil {
				attrs = append(attrs, slog.Int("status", apiErr.HTTPStatusCode),
					slog.String("clerk_trace_id", apiErr.TraceID))
			}
			a.log.ErrorContext(ctx, "clerk api key verification failed", attrs...)
			httpapi.Error(w, http.StatusBadGateway, "auth_unavailable", "could not verify the user with Clerk")
			return
		}
		exp, valid := a.keyUsable(k)
		if !valid {
			a.log.WarnContext(ctx, "clerk api key not usable", slog.String("key_id", k.ID))
			a.refuse(secret, client, now)
			httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid API key")
			return
		}
		userID, keyID = k.Subject, k.ID
		a.storeKey(secret, keyCached{userID: userID, keyID: keyID, exp: exp})
	}

	u, err := a.lookup(ctx, userID)
	switch {
	case errors.Is(err, errNoVerifiedEmail):
		httpapi.Error(w, http.StatusForbidden, "forbidden", "this account has no verified email")
		return
	case err != nil:
		a.log.ErrorContext(ctx, "clerk lookup failed", slog.String("err_type", fmt.Sprintf("%T", err)),
			slog.String("key_id", keyID))
		httpapi.Error(w, http.StatusBadGateway, "auth_unavailable", "could not verify the user with Clerk")
		return
	}
	if !a.allowed(u) {
		a.log.WarnContext(ctx, "admin access forbidden", slog.String("user", u.ID), slog.String("key_id", keyID))
		httpapi.Error(w, http.StatusForbidden, "forbidden", "this account is not an admin")
		return
	}
	u.Via = "api_key"
	a.log.DebugContext(ctx, "admin api key accepted", slog.String("user", u.ID), slog.String("key_id", keyID))
	next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, ctxKey{}, u)))
}

// refuse remembers secret as refused and counts one failure for client.
func (a *Authenticator) refuse(secret, client string, now time.Time) {
	a.storeRefused(secret)
	a.keyFails.fail(client, now)
}

// keyRejected reports whether a Clerk error status means the key itself is
// bad. 401 and 403 mean our own secret key is wrong and 429 is throttling,
// so those are Clerk failures, not bad keys.
func keyRejected(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return false
	}
	return status >= 400 && status < 500
}

// keyUsable reports whether a verified key may authenticate a user, and
// until when the result may be cached. Claims are never trusted.
func (a *Authenticator) keyUsable(k *clerk.APIKey) (time.Time, bool) {
	now := a.now()
	exp := now.Add(keyCacheTTL)
	if k == nil || k.Revoked || k.Expired {
		return time.Time{}, false
	}
	if k.Type != "" && k.Type != "api_key" {
		return time.Time{}, false
	}
	if !strings.HasPrefix(k.Subject, "user_") {
		return time.Time{}, false
	}
	if k.Expiration != nil {
		v := *k.Expiration
		var at time.Time
		if v > 1e12 {
			at = time.UnixMilli(v)
		} else {
			at = time.Unix(v, 0)
		}
		if !now.Before(at) {
			return time.Time{}, false
		}
		if at.Before(exp) {
			exp = at
		}
	}
	return exp, true
}

func (a *Authenticator) cachedKey(secret string) (userID, keyID string, ok bool) {
	h := sha256.Sum256([]byte(secret))
	a.mu.Lock()
	defer a.mu.Unlock()
	c, hit := a.keyCache[h]
	if !hit {
		return "", "", false
	}
	if !a.now().Before(c.exp) {
		delete(a.keyCache, h)
		return "", "", false
	}
	return c.userID, c.keyID, true
}

func (a *Authenticator) storeKey(secret string, c keyCached) {
	h := sha256.Sum256([]byte(secret))
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.keyCache) >= keyCacheMax {
		now := a.now()
		for k, v := range a.keyCache {
			if !now.Before(v.exp) {
				delete(a.keyCache, k)
			}
		}
		if len(a.keyCache) >= keyCacheMax {
			clear(a.keyCache)
		}
	}
	a.keyCache[h] = c
}
