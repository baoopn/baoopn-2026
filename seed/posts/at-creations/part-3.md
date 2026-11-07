The old system had its own passwords, its own JWTs and a sign-up secret. Most of its worst bugs lived there. In the rewrite I removed first-party auth. This part covers Clerk for staff, how roles become permissions, the rest of the security model, and the image upload pipeline.

## No passwords of my own

Shoppers never sign in. Staff sign in through Clerk, a hosted auth service. Clerk owns passwords, resets, email checks and MFA. The API never sees a password and never mints a token.

Sign-up is set to invitation only. An admin invites a new staff member from the dashboard, and the role is set on the invitation. Without an invite ticket, the sign-up page only shows an "invitation only" notice, and Clerk rejects the sign-up anyway.

That one choice removed several rows from the old risk table: reset links built from request input, the self-sign-up secret, weak default JWT secrets, and plain-text tokens.

## From token to permission

The admin app sends a Clerk session token as a bearer token. The API checks it with Clerk's Go SDK against Clerk's public keys. It also checks the `azp` claim (the origin the token was made for), so a token minted for some other site is rejected.

Roles live in the Clerk user's `publicMetadata.role`. Only the backend or the Clerk dashboard can set that field, not the user. I add the role to the session token as a custom claim, so most requests need no extra lookup. When the claim is missing, the API asks Clerk and caches the answer.

There are three roles, mapped to permissions in one table:

```go
var rolePermissions = map[Role][]Permission{
	RoleStaff:   {ProductsWrite, OrdersRead, OrdersWrite},
	RoleManager: {ProductsWrite, ProductsDelete, CategoriesWrite, OrdersRead, OrdersWrite, OrdersShipping},
	RoleAdmin:   {ProductsWrite, ProductsDelete, CategoriesWrite, OrdersRead, OrdersWrite,
		OrdersShipping, StoreWrite, StaffManage, AuditRead},
}
```

Each admin route asks for one permission with `auth.Require`:

```go
func Require(perm Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !PrincipalFrom(r.Context()).Can(perm) {
				httpx.WriteError(w, r, httpx.ErrForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

`GET /v1/admin/me` returns the user's permission list, so the dashboard can hide buttons a user cannot use. That is only for comfort. The API checks every request on its own.

A role change takes effect when the user's current session token expires (about a minute by Clerk's default). A ban revokes all sessions at once, so to cut access fast, you ban first.

### Guard rails on staff changes

Staff management has two rules that the old code lacked. Nobody can change their own role or ban themselves. And the API refuses to remove or demote the last active admin, with a 409 that the dashboard shows as plain text. Without that rule, one click could lock everyone out.

Every admin write also goes into an append-only audit log: who did it, what changed, the request id and the IP. Entries expire after two years.

## No cookies, no CSRF

The API accepts no cookies (`AllowCredentials: false`). Auth is only the `Authorization` header. Since a browser never attaches that header on its own, there is no CSRF attack to defend against.

CORS has two policies, picked by path before routing. Admin routes accept only the admin origin and allow the `Authorization` header. Public routes accept the storefront and admin origins, GET and POST only, and do not allow `Authorization`. So even if someone took over a storefront page, it could not drive the admin API from a browser.

## Config that refuses to start

The old API would boot with `JWT_SECRET="secret"`. The new one validates its whole config at startup. In production it exits with a list of problems if it finds wildcard CORS, Clerk test keys, mail sent to the log, an `http` CDN, a short hash pepper, or no authorized parties for Clerk.

I like this pattern a lot. A bad deploy fails at boot, loudly, instead of running in an unsafe state.

## Money and stock

Online checkout is off by default, because the store is catalogue-only today. But the order code is complete, and I fixed the money bugs in it.

The order body may only hold product ids and quantities. Prices come from the catalogue. Stock is taken inside a MongoDB transaction with a guard, so it can never go below zero:

```go
err := s.products.FindOneAndUpdate(tx,
	bson.M{"_id": id, "status": catalog.StatusPublished, "hasPrice": true, "stock": bson.M{"$gte": q}},
	bson.M{"$inc": bson.M{"stock": -q, "version": 1}},
	options.FindOneAndUpdate().SetReturnDocument(options.Before)).Decode(&p)
if mongox.IsNotFound(err) {
	return nil, httpx.Conflict("product " + id.Hex() + " is unavailable or does not have enough stock")
}
```

The check and the decrement are one atomic step. If two people order the last item at once, one of them gets a 409.

The order access code is stored as an HMAC (a keyed hash) with a server-side pepper and compared in constant time. It is never returned, not even to staff. Order tracking uses POST and returns the same 404 for "wrong code" and "no such order".

## Image uploads

Uploads were the most involved part of the rewrite. The old code trusted the file's MIME type and name, which allows stored XSS and file overwrites. The new flow never trusts the client's file.

```
admin (Uppy, 3 parallel)
  │ POST /v1/admin/products/{id}/uploads   (one file, ≤25 MB)
  ▼
API: header check → reserve slot (≤10/product) → original to private uploads/ → job queued → 202
  │ dispatcher claims job with a lease → gRPC call
  ▼
image worker: decode with limits → fix EXIF orientation → resize → WebP → write → delete original
  ▼
API: attach image + release slot → done
```

### Reserving a slot

A product can have at most 10 images, and uploads still in flight count. The API reserves a slot with one conditional update, so parallel uploads cannot push a product past the limit:

```go
res, err := s.products.UpdateOne(ctx, bson.M{
	"_id": id,
	"$expr": bson.M{"$lt": bson.A{
		bson.M{"$add": bson.A{bson.M{"$size": "$images"}, bson.M{"$ifNull": bson.A{"$pendingImages", 0}}}},
		MaxProductImages,
	}},
}, bson.M{"$inc": bson.M{"pendingImages": 1}})
```

### A separate worker for decoding

The API only checks the file header. It stores the original in a private `uploads/` area and writes a job to the `image_jobs` collection. Decoding happens in a separate Go service, the image worker, which the API calls over gRPC.

I split it out because decoding images is the riskiest and most memory-hungry thing the system does. The worker has no database access. It is only reachable on the internal network, with a shared token. It caps how many images it decodes at once. It rejects images over 50 megapixels or 256 MB once decoded, which catches "decompression bombs" (small files that expand to huge images). The output is always re-encoded WebP, which also strips EXIF data such as GPS.

The worker writes the full image plus smaller copies at 400, 800 and 1600 px. The storefront uses them for `srcset`.

### Retries and cleanup

Jobs are durable. A dispatcher claims each one with a lease. If an API instance dies mid-job, the lease runs out and another picks it up. Errors that might pass (worker busy, storage throttling, timeouts) retry with backoff and jitter, up to six attempts.

Originals are deleted in every outcome: by the worker on success, by the API on cancel or give-up, and by an hourly sweep for anything a crash left behind. A daily sweep deletes product and category images that nothing references. It keeps any image still shown on a past order.

Object keys are random. A client filename never reaches the bucket, so nothing can overwrite anything else.

Next, part 4 moves to the public side: the SvelteKit storefront and how it serves two languages.
