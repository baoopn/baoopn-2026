The API has two kinds of callers. People use the shop site or the admin panel in a browser. Scripts, such as a bulk import, call it with no browser at all. I gave each its own way in: signed cookies for people, API keys for scripts. Both end in the same place, a user on `req.user` with a role.

## Four roles

The user model has four roles:

```ts
export type UserRole = "user" | "admin" | "manager" | "staff";
```

Routes check them with a small factory:

```ts
export const authorize = (roles: string[]) => {
  return (req: Request, res: Response, next: NextFunction): void => {
    if (!req.user) {
      return next(new UnauthorizedError("Authentication required"));
    }
    if (!roles.includes(req.user.role)) {
      return next(new ForbiddenError("Insufficient permissions"));
    }
    next();
  };
};
```

The split looks like this in the routes:

| Action | Roles |
|---|---|
| Create, edit, delete one product; upload product photos; work on orders | staff, manager, admin |
| Bulk create or bulk delete; edit categories; change shipping info | manager, admin |
| Manage users, page media, store info, cleanup jobs | admin |

Staff can do the daily work. Bulk actions can do more harm, so they need a manager. Anything that touches other accounts needs an admin. The admin user routes also call `requireVerified`, so an admin must confirm their email before they can disable a user or change a role.

There is no open sign-up. The public `/auth/register` route needs a shared secret from the server config. After the first admin exists, admins create other accounts from a separate route.

## Sessions with two tokens

Login returns two JWTs (signed JSON tokens): a short access token and a longer refresh token.

```ts
public static ACCESS_TOKEN_EXPIRATION = "15m" as ms.StringValue;
public static REFRESH_TOKEN_EXPIRATION = "3d" as ms.StringValue;
```

The old series said one hour and seven days. The current code uses 15 minutes and three days.

Both tokens live in signed, HTTP-only cookies with `sameSite: "strict"`. Browser code cannot read them, and the browser will not send them on requests from other sites. I also scope each cookie by path:

```ts
public static JWT_COOKIE_OPTIONS = {
  httpOnly: true,
  secure: config.IS_PROD,
  sameSite: "strict" as "strict",
  maxAge: ms(TokenService.REFRESH_TOKEN_EXPIRATION),
  signed: true,
  path: this.API_PATH,
};

public static REFRESH_COOKIE_OPTIONS = {
  ...this.JWT_COOKIE_OPTIONS,
  path: `${this.API_PATH}/auth`,
};
```

The browser sends the refresh cookie to `/auth` routes and nowhere else. A product request never carries it.

The server keeps each refresh token in a MongoDB collection with a TTL index, so MongoDB drops it when it expires. On refresh, the server checks that the token is in the table, deletes it, and issues a new one. Each refresh token works once. Logout deletes the stored token. A password change deletes all of a user's tokens, which signs them out on every device.

The access token is not stored. On each request, `authenticate` checks its signature and then loads the user from the database. That costs one read per request, but it means a disabled account stops working on its next request, not when its token runs out.

## API keys for scripts

For scripts, a user can create named API keys. A key looks like `at_` plus 64 hex characters. The server shows it once.

The server cannot store the key in plain text, but it also needs to find a key fast when a request comes in. Here is how I solved both:

```ts
const plaintextKey = `at_${crypto.randomBytes(32).toString("hex")}`;
const encryptedKey = EncryptionUtils.encrypt(plaintextKey);

const apiKey: ApiKeyWithPlaintext = {
  key: encryptedKey,
  truncatedKey: plaintextKey.slice(0, TRUNC_KEY_LENGTH),
  name,
  createdAt: new Date(),
  // ...
};
```

The full key is encrypted with AES-256-GCM. The first ten characters stay in clear text as a lookup field. To check a key, the server finds users whose keys share that prefix, decrypts their keys, and compares. Seven random hex characters after `at_` make a clash rare, so most checks decrypt one key.

Routes opt in to API keys. The catalog, category and media routes call `authenticate` with `{ allowApiKey: true }`. Account routes do not, so a leaked key cannot change a password.

Looking back, I would hash the keys (for example with SHA-256) and not encrypt them. The server never needs the key back. It needs to compare, nothing more. A hash removes the decryption step and the risk that comes with a stored key that can be decrypted.

## Locking out password guessing

Failed logins count in Redis, per username and per email:

```ts
export const FAILED_LOGIN_CONFIG = {
  MAX_ATTEMPTS: 5,
  LOCKOUT_TIME: 3600,   // 1 hour
  ATTEMPT_EXPIRY: 1800, // count resets after 30 minutes
};
```

After five misses in 30 minutes, the account locks for an hour. Login counts the miss under the name the user typed and under the account's username and email. Switching from email to username does not reset the count. A good login clears the counters.

## Rate limits

Rate limits use `rate-limiter-flexible` with Redis as the store. There are three tiers:

| Limiter | Limit | Block |
|---|---|---|
| General API | 120 requests per minute (from config) | 1 minute |
| Auth routes | 10 per minute per IP | 2 minutes |
| Sensitive (password change, reset, email check) | 3 per 5 minutes per IP and email | 5 minutes |

A blocked caller gets a 429 with a `Retry-After` header.

## CSRF tokens without server state

The cookies use `sameSite: "strict"`, which blocks most cross-site attacks. I added a CSRF token as a second layer. It uses the double-submit pattern: the server sets a token in a cookie, and the client must copy it into an `X-XSRF-TOKEN` header. A page on another site cannot read the cookie, so it cannot set the header.

The token needs no server storage. It carries its own time stamp and an HMAC (a keyed hash) of its contents:

```ts
const payload = `${timestamp}|${randomBytes}|${sessionIdentifier}`;

const hmac = crypto
  .createHmac("sha256", CSRF_SECRET)
  .update(payload)
  .digest("hex");

const token = Buffer.from(`${payload}|${hmac}`).toString("base64");
```

To check it, the server splits the token, rebuilds the HMAC, and compares the two with `crypto.timingSafeEqual`. Tokens last 24 hours. `GET`, `HEAD` and `OPTIONS` skip the check.

The code tries to bind the token to the user ID. In practice it binds to the IP address, because the CSRF check runs before any route sets `req.user`. I come back to that in Part 5.
