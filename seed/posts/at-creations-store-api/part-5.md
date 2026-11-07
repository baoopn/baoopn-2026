The last part covers what keeps the API tidy while it runs: scheduled jobs, logs, health checks and shutdown. Then I go through what I got wrong. Reading the code again for this post, I found more than I expected.

## Three scheduled jobs

The app uses `node-cron` for three jobs. They start after MongoDB and Redis are up:

| Job | When | What it does |
|---|---|---|
| Media cleanup | 3:00 every day | Syncs R2 files with database references |
| Temp file cleanup | Every hour | Deletes upload temp files older than one hour |
| Recent views | Every hour | Rebuilds `recentViews` for "trending" (Part 3) |

Admins can also run the two cleanup jobs from an endpoint, which helps when testing.

### Keeping R2 and MongoDB in sync

Files and database rows can drift apart. A request can fail after the upload but before the database update. A staff member can delete a product and leave its photos behind. The media cleanup job fixes both cases.

It builds two sets of keys: every key the database refers to, and every key in R2 under the product and category prefixes. Then it compares them:

```ts
const dbMediaKeys = await this.getMediaKeysFromDatabase();
const r2MediaKeys = await this.getMediaKeysFromR2();

// In R2, but nothing points at it
const orphanedR2Keys = Array.from(r2MediaKeys).filter(
  (key) => !dbMediaKeys.has(key)
);
await this.deleteOrphanedR2Files(orphanedR2Keys);

// In the database, but the file is gone
const danglingDbKeys = Array.from(dbMediaKeys).filter(
  (key) => !r2MediaKeys.has(key)
);
await this.cleanupDanglingDatabaseReferences(danglingDbKeys);
```

Orphaned files are deleted in batches of 1,000, the most one S3 `DeleteObjects` call accepts. The R2 listing pages through results with a continuation token, so a large bucket is read in full. Dangling references are pulled from product image arrays, or set to `null` for thumbnails.

A flag stops two runs from overlapping. If the admin endpoint and the 3:00 job fire at once, the second one returns at once.

This job works because of the choice in Part 4 to store keys, not URLs. A fixed prefix per file type means one list call per type.

## Logs and health checks

The old series described a request log stored in MongoDB with a TTL index. The current code does not have that. It logs to the console, and Docker's `json-file` driver keeps the output, capped at three files of 10 MB each. That is enough to read recent errors. It cannot answer questions like "which routes are slow?"

There are two health checks. `/health` is public and cheap: database state, memory and uptime. Docker calls it every 30 seconds. `/health/detailed` needs an admin. It pings MongoDB and reports how long the ping took.

## Starting and stopping

Startup runs in a fixed order. Connect to MongoDB. Ping both Redis clients. Start the cron jobs. Then listen. If any step fails, the process exits with code 1, and Docker restarts it.

Shutdown is the reverse. The server tracks open sockets so it can close them:

```ts
const gracefulShutdown = async (signal: string) => {
  server.close(() => console.log("HTTP server closed"));

  Object.values(connections).forEach((connection: any) => {
    connection.end();
  });

  await Promise.all([
    disconnectFromDatabase().catch(/* log */),
    disconnectRedis().catch(/* log */),
  ]);

  process.exit(0);
};

process.on("SIGTERM", () => gracefulShutdown("SIGTERM"));
process.on("SIGINT", () => gracefulShutdown("SIGINT"));
```

`server.close` stops new connections. Ending the open sockets stops keep-alive clients from holding the process open. MongoDB and Redis close side by side. Redis uses `quit`, which lets queued commands finish.

## What I would change

### Middleware order hides the user

The global rate limiter and the CSRF check both run before any route calls `authenticate`. So `req.user` is never set when they run. The rate limiter keys on IP, not user. The CSRF token is bound to the IP, not the user ID as the code intends. A phone that switches from Wi-Fi to mobile data gets a new IP, and its token stops working.

The CSRF check also applies to API key calls. A script must fetch a CSRF token before each write, which makes no sense for a caller with no browser. I would read the session before these checks, and skip CSRF for API key requests.

### Mongoose hooks skip updates

The product save hook builds the normalized Vietnamese text and the embedding. But `updateProduct` uses `findByIdAndUpdate`, and Mongoose does not run `save` hooks for that call. If staff rename a product, its normalized name and embedding keep the old text. A search without tone marks finds it under the old name. The fix is to load the document and call `save()`, or to add a `findOneAndUpdate` hook.

### One cache pattern matches nothing

After a stock change, the code tries to clear lists that filter on stock:

```ts
await cacheRedisUtils.delByPattern(`${REDIS_CACHE_KEYS.PRD.LST}*inStock*`);
```

But the search key holds the value of `inStock` (`true`, `false` or `undefined`), not the word. The pattern never matches. Lists show old stock until they expire, up to 30 minutes for the featured list. I would build keys and patterns from one function, so they cannot drift apart.

### `KEYS` and shared memory limits in Redis

`delByPattern` uses `KEYS`, which blocks Redis while it scans. `SCAN` does the same job in small steps. A version number in the key prefix would be simpler still: bump it on a write, and old keys expire on their own.

Both Redis clients also call `CONFIG SET maxmemory` on ready, one with 256 MB and one with 512 MB. That setting is server-wide, not per database, so whichever client connects last wins. With `allkeys-lru`, memory pressure from the cache could also evict lockout counters or rate-limit keys. Auth data should live on its own Redis instance, or under a policy that does not evict it.

### Indexes that do nothing

The product schema indexes a field named `categories`, but the field is `categoryIds`. Both models also define text indexes, and no query uses them. Search runs on `$regex`. I would fix the first, and drop the text indexes or switch search to use them.

### A cleanup job with no margin

The media job deletes every R2 file the database does not mention. A file uploaded at 3:00, in the gap before the database update, could be deleted. I would skip files newer than an hour, using the `LastModified` date R2 already returns. I would also stop the job if the database returns zero keys, since that points to a bug more than an empty shop.

### No tests

Jest is in `package.json`, but there are no test files. Each bug above is small, and a few focused tests would have caught most of them. If I start this again, I would write tests for the cache keys, the auth middleware and the save hooks first.

## What held up

The parts I would keep: typed errors with one handler, signed and path-scoped cookies, single-use refresh tokens, the staff-only cache bypass, the normalized search fields, and R2 keys in place of URLs. The overall shape held up. Most of what went wrong sits in the gaps between parts, and tests are what would close those gaps.
