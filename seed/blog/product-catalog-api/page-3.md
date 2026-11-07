## Part 3: Caching Strategy

### Introduction

In this part, I'll explain how we implemented Redis caching in our Express.js product catalog API. Proper caching is essential for any API that needs to handle significant traffic while maintaining fast response times.

### Redis as a Caching Solution

We chose Redis for our caching needs because it provides:

1. In-memory data storage for ultra-fast access
2. Automatic key expiration
3. Minimal overhead for serialization/deserialization
4. Simple implementation and management

Our caching strategy focuses on frequently accessed data that doesn't change often but is expensive to retrieve from the database.

### Redis Configuration

We set up dedicated Redis instances for different purposes:

```javascript
// Redis client for general purposes
const redis = new Redis({
  host: REDIS_HOST,
  port: REDIS_PORT,
  password: REDIS_PASSWORD,
  db: REDIS_DB // Default database
});

// Redis client for authentication
const authRedis = new Redis({
  host: REDIS_HOST,
  port: REDIS_PORT,
  password: REDIS_PASSWORD,
  db: REDIS_AUTH_DB // Authentication database
});
```

We also configured memory limits to prevent Redis from consuming too much resources:

```javascript
// Set max memory and eviction policy for Redis
redis.on('connect', async () => {
  try {
    await redis.config('SET', 'maxmemory', REDIS_MAXMEMORY);
    await redis.config('SET', 'maxmemory-policy', REDIS_MAXMEMORY_POLICY);
  } catch (err) {
    console.error('Error setting Redis configuration:', err);
  }
});
```

### Individual Resource Caching

For single resources like products or categories, we implemented a straightforward cache pattern:

```javascript
export const getCategoryById = async (req, res) => {
  const { id } = req.params;
  const { cache } = req.query;

  // Generate a unique cache key based on the category ID
  const cacheKey = `category_${id}`;

  try {
    // Check if data is cached in Redis
    const cachedData = await redis.get(cacheKey);
    if (cachedData && cache !== 'false') {
      return res.json(JSON.parse(cachedData));
    }

    // Fetch category by ID if not in cache
    const category = await prisma.category.findUnique({
      where: { id: id }
    });

    // Cache the response data with TTL
    const response = {
      message: "Category retrieved successfully",
      data: category
    };
    await redis.set(cacheKey, JSON.stringify(response), 'EX', CACHE_TTL);

    res.json(response);
  } catch (error) {
    res.status(500).json({
      message: "Error retrieving category",
      error: error.message
    });
  }
};
```

### Collection Caching

For collection endpoints with filtering and sorting, we generate dynamic cache keys based on query parameters:

```javascript
export const advancedGetProducts = async (req, res) => {
  const {
    limit = DEFAULT_LIMIT,
    offset = DEFAULT_OFFSET,
    categories,
    price_min,
    price_max,
    search,
    sort,
    order = DEFAULT_SORT_ORDER,
    cache
  } = req.query;

  // Generate a unique cache key based on query parameters
  const cacheKey = `products_${limit}_${offset}_${categories}_${price_min}_${price_max}_${search}_${sort}_${order}`;

  try {
    // Check if data is cached
    const cachedData = await redis.get(cacheKey);
    if (cachedData && cache !== 'false') {
      return res.json(JSON.parse(cachedData));
    }
    
    // Complex database query here...
    
    // Cache the result
    await redis.set(cacheKey, JSON.stringify(response), 'EX', CACHE_TTL);
    
    res.status(200).json(response);
  } catch (error) {
    // Error handling...
  }
};
```

### Cache Invalidation

For effective caching, we need to know when to invalidate the cache. We use a simple but effective approach - whenever data is modified, we clear the related caches:

```javascript
export const updateProductById = async (req, res) => {
  // Code to update the product...
  
  try {
    // Update product
    const updatedProduct = await prisma.product.update({
      where: { id },
      data: product,
    });

    // Clear the cache
    redis.flushdb(); // Flush cache for this database

    // Send response
    res.json({
      message: "Product updated successfully",
      data: updatedProduct
    });
  } catch (error) {
    // Error handling...
  }
};
```

### Cache Bypass Mechanism

Sometimes, we need the most up-to-date data. We implemented a cache bypass mechanism with an optional query parameter:

```javascript
// Check if data is cached in Redis
const cachedData = await redis.get(cacheKey);
if (cachedData && cache !== 'false') {
  return res.json(JSON.parse(cachedData));
}
```

For authenticated users, we added a middleware to allow cache bypassing:

```javascript
export const requireTokenToBypassCache = (req, res, next) => {
  const { cache } = req.query;

  if (!cache || cache !== 'false') {
    return next();
  }

  return authenticateJwt(req, res, next);
};
```

### Performance Improvements

Our Redis caching strategy dramatically improved API performance:
- Response time for popular category listings: 200ms &rarr; 15ms
- Response time for product detail pages: 150ms &rarr; 10ms
- Response time for filtered product searches: 500ms &rarr; 25ms

This translates to a 10-20x improvement in response times for cached endpoints.

### Implementation Challenges and Solutions

1. **Cache Key Management**: We standardized cache key formats to avoid conflicts
2. **Cache Invalidation**: We decided on a simple "flush on change" approach for simplicity
3. **TTL Selection**: We chose a 1-hour TTL for most data as a balance between freshness and performance

### Future Cache Invalidation Improvements

While our current "flush on change" approach works well, we have plans to implement more targeted cache invalidation:

1. **Pattern-Based Invalidation**: Using Redis's SCAN and DEL commands to remove specific pattern-matched keys
2. **Selective Cache Updates**: Updating only affected cache entries rather than flushing entire databases
3. **Event-Based Invalidation**: Using a pub/sub model to notify all API instances when specific data changes
4. **Hierarchical Cache Keys**: Organizing cache keys in a hierarchy to enable more granular invalidation
5. **Cache Tags**: Implementing a tagging system to invalidate related cache entries together

These improvements will further optimize our caching strategy as the product catalog grows, maintaining the performance benefits while reducing unnecessary cache misses.

### Conclusion

Redis caching significantly improved our API performance with minimal additional code complexity. The pattern we established is easy to maintain and extend as the API grows.

In the next part, we'll explore our media management system that handles product images using Cloudflare R2 storage.