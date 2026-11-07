AT Creations is a small shop that lists its products in English and Vietnamese. I built the API that runs its catalog. The shop's site reads products from it, and staff use it to add products, upload photos and handle orders.

I wrote about an earlier version of this project in a seven-part series. That version was plain JavaScript with Prisma on top of MongoDB. The code I describe here is the rewrite: TypeScript, with Mongoose in place of Prisma, served under `/api/v2`. Much of the old series no longer matches the code, so this post replaces it. It is shorter, and every claim in it comes from the current source.

## What the API does

The API covers five areas:

- **Catalog.** Products and categories, each with English and Vietnamese names and descriptions. Search, filters, sorting and pages.
- **Media.** Product photos, product thumbnails, category thumbnails and page images, stored in Cloudflare R2.
- **Orders.** Guests and signed-in users can place orders. Staff move them through a set of states, from `CREATED` to `COMPLETED`, or to `CANCELLED` or `REFUNDED`.
- **Users and access.** Four roles, cookie sessions for people, API keys for scripts.
- **Store info.** A small block of shop details that admins can edit.

It also tracks product views, so the shop can show "popular" and "trending" lists.

## The stack

| Part | Choice | Why |
|---|---|---|
| Web server | Express 4 with TypeScript | I knew it well, and the middleware model fits auth, rate limits and CSRF. |
| Database | MongoDB through Mongoose | Products have loose, nested data. Mongoose gives me schemas, hooks and indexes in one place. |
| Cache and counters | Redis through ioredis | Fast reads for hot lists, plus rate limits and login lockouts. |
| File storage | Cloudflare R2 through the AWS S3 SDK | R2 speaks the S3 API and does not charge for egress. |
| Email | SendGrid | Order emails, email checks and password resets. |
| Embeddings | Google Gemini `text-embedding-004` | A vector for each product, saved for later use. |

The app runs in Docker. A second container runs Redis. A GitHub Actions workflow deploys on each push to `master`.

## How a request flows

The order of middleware matters, so here is `app.ts` with the noise removed:

```ts
// Health check sits before all middleware, so Docker checks stay cheap
app.get(`${API_ROUTES}${API_VERSION}/health`, getHealthStatus);

app.use(helmet());
app.use(handleCorsPreflightRequests);
app.use(configureCors());

app.use(express.json());
app.use(express.urlencoded({ extended: true }));
app.use(cookieParser(config.COOKIE_SECRET));

app.get(`${API_ROUTES}${API_VERSION}/csrf-token`, generateCsrfTokenHandler);

app.use(`${API_ROUTES}${API_VERSION}`, (req, res, next) => {
  if (req.path === "/health" || req.path === "/csrf-token") {
    return next();
  }
  return rateLimiter(req, res, next);
});

app.use(`${API_ROUTES}${API_VERSION}`, csrfProtection);
app.use(`${API_ROUTES}${API_VERSION}`, apiRoutes);

app.use(notFoundHandler);
app.use(errorMiddleware);
```

A request passes through security headers, CORS, body parsing, a rate limit and a CSRF check. Then it reaches a route. Each route adds its own auth: `authenticate`, `authorize([...roles])`, or nothing for public reads.

I put the health check first on purpose. Docker calls it every 30 seconds. It should not count against the rate limit, and it should not fail because of a missing CSRF cookie.

## How the code is laid out

```plaintext
src/
├── config/       env, MongoDB, Redis, R2 clients
├── middleware/   auth, CORS, CSRF, rate limits, uploads, errors
├── models/       Mongoose schemas
├── controllers/  HTTP in, HTTP out, cache reads and writes
├── services/     business rules and database calls
├── routes/       one router per resource
└── utils/        Redis helpers, text tools, encryption
```

Controllers stay thin. They parse the request, check the cache, call a service, and shape the reply. Services hold the rules. A product service, for example, checks that every category ID exists before it saves a product.

## One error path

Every handler is wrapped in a small `asyncHandler`, so a thrown error reaches Express. Services throw typed errors such as `BadRequestError` (400), `ForbiddenError` (403) or `DatabaseError` (503). One error middleware at the end turns them into a JSON reply. In development it adds the stack trace. In production it does not.

Database calls go through one helper, `DbService.executeDbOperation`. It checks that Mongoose is connected, runs the call, and maps raw MongoDB errors to the typed ones:

```ts
// Duplicate key errors (unique constraint violations)
if (error.name === "MongoServerError" && error.code === 11000) {
  const field = Object.keys(error.keyValue)[0];
  const value = error.keyValue[field];
  throw new BadRequestError(`${field} '${value}' already exists`);
}

// Network errors
if (error.name === "MongoNetworkError") {
  throw new DatabaseError(
    "Database network error: Could not connect to the database"
  );
}
```

This gave me one place to decide what the client sees. A duplicate email is the client's fault, so it gets a 400. A lost database link is not, so it gets a 503.

## What the rest of this post covers

- **Part 2** covers logins, roles, API keys, CSRF tokens and rate limits.
- **Part 3** covers the data model and how search works across two languages.
- **Part 4** covers the Redis cache and how photos get to R2.
- **Part 5** covers the cleanup jobs, shutdown, and the bugs and choices I would change.
