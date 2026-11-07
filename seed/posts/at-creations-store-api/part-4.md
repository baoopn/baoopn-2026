Most traffic to a shop is reads: the home page lists, a category page, a product page. Those reads change when staff edit something, and at no other time. This part covers how I cache them in Redis, and how product photos get from an upload form into Cloudflare R2.

The old series listed response times before and after caching. I have no benchmark in the repo to back those numbers, so I left them out.

## One Redis server, two databases

The app opens two ioredis clients to the same Redis server. Each uses a different database number:

```ts
const AUTH_DB = 0;
const CACHE_DB = 1;

export const authRedis = new Redis({
  host: config.REDIS_HOST,
  port: config.REDIS_PORT,
  password: config.REDIS_PASSWORD,
  db: AUTH_DB,
  retryStrategy: (times: number) => {
    const delay = Math.min(times * 50, 2000);
    // ...log the retry
    return delay;
  },
  maxRetriesPerRequest: 3,
});
```

Database 0 holds rate-limit counters, login lockouts, and email and password-reset tokens. Database 1 holds cached API data. The split lets me flush the cache without signing anyone out or lifting a lockout.

The retry strategy waits a bit longer after each failed connect, up to two seconds. `maxRetriesPerRequest: 3` makes a command fail after three tries, so a request gets an error and does not hang while Redis is down.

## Short keys and fixed lifetimes

All cache keys follow a short prefix scheme:

```ts
export const REDIS_CACHE_KEYS = {
  CAT: { DET: "c:d:", LST: "c:l:", ALL: "c:*" },
  PRD: { DET: "p:d:", LST: "p:l:", ALL: "p:*" },
  STORE: { INFO: "s:i:" },
  // ...
};
```

`p:d:` is one product. `p:l:` is any product list. The prefixes keep keys small and let me clear a whole group with one pattern.

Each kind of data gets a lifetime that fits how often it changes:

| Data | Lifetime |
|---|---|
| One product or category | 30 minutes |
| Featured list | 30 minutes |
| Search results, category pages, popular list | 5 minutes |
| Trending list | 2 minutes |
| All product IDs (admin) | 1 hour |

Trending is the shortest because the hourly view job keeps changing it.

## The read path

Every cached route follows the same pattern. Here is the product page:

```ts
export const getProductById = asyncHandler(
  async (req: Request, res: Response) => {
    const { id } = req.params;
    const cacheKey = `${REDIS_CACHE_KEYS.PRD.DET}${id}`;

    let product;
    if (!req.bypassCache) {
      product = await cacheRedisUtils.get(cacheKey);
    }

    if (!product) {
      product = await ProductService.getProductById(id);
      if (product && !req.bypassCache) {
        await cacheRedisUtils.set(cacheKey, product, CACHE_EXPIRY.MEDIUM);
      }
    }

    return res
      .status(200)
      .json(successResponse("Product retrieved successfully", product));
  }
);
```

For search, the key holds every query value: the search text, price range, categories, flags, sort, order, page and page size. Two requests share a cache entry when they ask for the same thing.

## Letting staff skip the cache

After staff edit a product, they want to see the change at once. Any public read route accepts `?cache=false`, but the flag works for staff and no one else. A `cacheBypass` middleware checks the session cookie and sets `req.bypassCache`:

```ts
const isStaffUser =
  req.user && ["staff", "manager", "admin"].includes(req.user.role);

req.bypassCache = shouldBypassCache && isStaffUser;
```

If a shopper adds `?cache=false`, nothing changes. They still get the cached copy. This stops anyone from using the flag to send every request to MongoDB.

A bypass read also skips the cache write. Staff see fresh data, and the cached copy stays as it is until it expires or an edit clears it.

## Clearing the cache on writes

Each write clears what it touched. A product update deletes that product's key and every product list:

```ts
await cacheRedisUtils.del(`${REDIS_CACHE_KEYS.PRD.DET}${id}`);
await cacheRedisUtils.delByPattern(REDIS_CACHE_KEYS.PRD.LST + "*");
```

Creating a product clears all `p:*` keys. Category edits clear the category's ID key, its slug key, and all category lists. Photo uploads and deletes clear the same keys as a product edit, since the thumbnail shows up in lists.

I chose to clear every list, not to pick out the ones that hold the product. Working out which cached searches include a given product is hard. Clearing them all is simple and correct, and lists rebuild on the next read.

`delByPattern` uses the Redis `KEYS` command. That works for a small cache, but `KEYS` blocks Redis while it walks the whole database. Part 5 has more on this.

## Photos: from form to R2

R2 is Cloudflare's object store. It speaks the S3 API, so I use the AWS SDK with a custom endpoint:

```ts
const r2Client = new S3Client({
  region: "auto",
  endpoint: config.R2_URL,
  credentials: {
    accessKeyId: config.R2_ACCESS_KEY_ID as string,
    secretAccessKey: config.R2_SECRET_ACCESS_KEY as string,
  },
});
```

Uploads go through Multer. It writes files to a temp folder on disk, with these limits:

```ts
export const MAX_IMAGES_PER_UPLOAD = 6;
export const MAX_IMAGE_SIZE = 5 * 1024 * 1024; // 5MB
export const IMAGE_MIME_TYPES = ["image/jpeg", "image/png", "image/webp"];
```

The old series showed a step that resized every image with Sharp. The current code has no such step. It uploads each file as it came in.

Staff can send up to six photos at once. If one of them is named `thumb.jpg` (or `thumb.png`, and so on), it becomes the product thumbnail. The rest are added to the product's images. A new thumbnail replaces the old one, and the old file is deleted from R2.

Each file gets a new name, so a re-upload never overwrites a cached copy on the CDN:

```ts
const generateFileName = (id: string, extension: string) => {
  const timestamp = Date.now();
  const randomHexStr = crypto.randomBytes(4).toString("hex");
  return `${id}_${timestamp}_${randomHexStr}.${extension}`;
};
```

Files land under a fixed key layout:

```plaintext
img/product/<productId>/<file>
img/product/thumbnail/<productId>/<file>
img/category/thumbnail/<categoryId>/<file>
data/<file>                       (page media, admin only)
```

## Store keys, not URLs

The database stores the R2 key, such as `img/product/<id>/<file>.webp`, not a full URL. The site adds the CDN base URL when it renders an image.

This has two benefits. If the CDN domain changes, no product rows need to change. The key layout also lets the cleanup job in Part 5 list every file of a given type by prefix and compare it with what the database holds.

After a file is in R2, the temp copy is deleted. If a request fails halfway, the temp file stays. An hourly job removes anything in the temp folder older than an hour.
