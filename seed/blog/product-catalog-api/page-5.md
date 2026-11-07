## Part 5: Database Setup and Hybrid Database Query Approach

### Introduction

In this part, I'll explain our database architecture for the Express.js product catalog API. We've implemented a hybrid approach using both MongoDB and Prisma ORM to balance flexibility and structure in our data storage strategy.

### Why a Hybrid Approach?

Our product catalog API needed both flexibility for unstructured data and strong type safety for critical operations. To achieve this, we implemented:

1. **MongoDB Native Driver** for complex aggregation pipelines and specialized queries
2. **Prisma ORM** for type-safe CRUD operations and simplified data manipulation

This hybrid approach gives us the best of both worlds: MongoDB's powerful query capabilities and Prisma's developer-friendly syntax and type safety.

### Database Configuration

We use MongoDB Atlas as our cloud database solution, connecting via X.509 certificate authentication for enhanced security:

```javascript
import { MongoClient, ServerApiVersion } from 'mongodb';

const credentials = DATABASE_KEY; // Path to the X.509 certificate
const uri = `mongodb+srv://${DATABASE_URL}?authSource=%24external&authMechanism=MONGODB-X509&retryWrites=true&w=majority&appName=${DATABASE_APP_NAME}`;

export const client = new MongoClient(uri, {
  tlsCertificateKeyFile: credentials,
  serverApi: ServerApiVersion.v1,
});

export const connectToDatabase = async () => {
  try {
    await client.connect();
    console.log('Connected to MongoDB');
    
    // Create indexes after successful connection
    await createIndexes();
    systemLogger.info(LogCategory.DATABASE, 'Connected to MongoDB');
  } catch (error) {
    console.error('Error connecting to MongoDB:', error);
    systemLogger.error(LogCategory.DATABASE, 'Error connecting to MongoDB', error);
    process.exit(1); // Exit the process with failure
  }
};
```

### Prisma Schema Design

Our Prisma schema defines the core structure of our data models:

```prisma
model Product {
  id           String   @id @default(auto()) @map("_id") @db.ObjectId
  name         String
  name_vi      String?
  normalized_name_vi String?
  price        Float
  description  String?
  description_vi String?
  code         Int?
  category_ids String[] @db.ObjectId
  images       String[]
  thumbnail    String?
  created_at   DateTime @default(now())
  updated_at   DateTime @updatedAt

  @@index([name])
  @@index([category_ids])
}

model Category {
  id          String   @id @default(auto()) @map("_id") @db.ObjectId
  name        String
  name_vi     String?
  normalized_name_vi String?
  description String?
  description_vi String?
  created_at  DateTime @default(now())
  updated_at  DateTime @updatedAt

  @@index([name])
}
```

### Optimized Indexes

We created specialized indexes to speed up common query patterns:

```javascript
export const createIndexes = async () => {
  try {
    console.log('Creating indexes...');
    const db = client.db(DATABASE_NAME);

    // Product indexes
    await db.collection(PRODUCT_COLLECTION).createIndexes([
      // Text search indexes
      {
        key: {
          name: 1,
          name_vi: 1,
          normalized_name_vi: 1
        },
        name: "product_search_idx"
      },
      // Category lookup index
      {
        key: { category_ids: 1 },
        name: "product_category_idx"
      },
      // Price range queries
      {
        key: { price: 1 },
        name: "product_price_idx"
      }
    ]);

    // Similar indexes for categories, users, etc.
    // ...
  } catch (error) {
    console.error('Error creating indexes:', error);
    throw error;
  }
};
```

### Multilingual Search Support

Our database design includes special fields for supporting Vietnamese content with diacritics:

```javascript
// Add normalized Vietnamese name for search
if (product.name_vi) {
  product.normalized_name_vi = normalizeVietnamese(product.name_vi);
}
```

The normalization removes diacritics from Vietnamese text, allowing for more flexible searching:

```javascript
if (search) {
  const searchWords = search.split(' ').map(word => {
    const normalizedWord = normalizeVietnamese(word);
    return {
      $or: [
        // Original search with diacritics
        { name: { $regex: word, $options: 'i' } },
        { name_vi: { $regex: word, $options: 'i' } },
        // Normalized search without diacritics
        { name: { $regex: normalizedWord, $options: 'i' } },
        { normalized_name_vi: { $regex: normalizedWord, $options: 'i' } }
      ]
    };
  });
  query.$and = searchWords;
}
```

### Transaction Management

For operations that modify multiple documents, we use MongoDB transactions to ensure data consistency:

```javascript
let session;
try {
  // Start a session for transaction
  session = client.startSession();
  
  await session.withTransaction(async () => {
    // Delete category from the database
    const deletedCategory = await categoriesCollection.findOneAndDelete(
      { _id: new ObjectId(id) },
      { session }
    );

    // Update products to remove deleted category ID
    const products = await productsCollection.find(
      { category_ids: { $in: [new ObjectId(id)] } },
      { session }
    ).toArray();

    // Update each product to remove the category ID
    await Promise.all(
      products.map(async (product) => {
        // Remove the category ID from the product
      })
    );
  });
  
  // Cache invalidation after successful transaction
  redis.flushdb();
} catch (error) {
  // Error handling
} finally {
  if (session) {
    session.endSession();
  }
}
```

### Advanced Query Capabilities

For complex searches, we leverage MongoDB's aggregation framework:

```javascript
const result = await productsCollection.aggregate([
  { $match: query },
  {
    $lookup: {
      from: CATEGORY_COLLECTION,
      let: { category_ids: '$category_ids' },
      pipeline: [
        { $match: { $expr: { $in: ['$_id', '$$category_ids'] } } },
        { $addFields: { id: { $toString: '$_id' } } }
      ],
      as: 'categories'
    }
  },
  {
    $facet: {
      metadata: [{ $count: "total" }, { $addFields: { limit: limitInt, offset: offsetInt } }],
      products: [
        { $sort: sortQuery },
        { $skip: offsetInt },
        { $limit: limitInt }
      ]
    }
  }
]).toArray();
```

### Graceful Database Disconnection

To ensure data integrity during application shutdown, we implemented proper database disconnection:

```javascript
const gracefulShutdown = async () => {
  console.log('Shutting down gracefully...');
  await closeDatabaseConnection();
  server.close((err) => {
    if (err) {
      console.error('Error closing server:', err);
      process.exit(1);
    } else {
      console.log('Server closed');
      process.exit(0);
    }
  });
};

process.on('SIGTERM', gracefulShutdown);
process.on('SIGINT', gracefulShutdown);
```

### Performance Considerations

To enhance database performance, we implemented several optimizations:

1. **Strategic Indexing**: Creating indexes for frequent query patterns
2. **Projection**: Requesting only the fields we need
3. **Pagination**: Implementing offset/limit patterns for large result sets
4. **TTL Indexes**: Automatic cleanup of logs using MongoDB's TTL feature

```javascript
// Create TTL index for logs
await db.collection(COLLECTIONS.REQUEST_LOGS).createIndexes([
  { 
    key: { timestamp: -1 }, 
    name: "request_logs_timestamp_idx",
    expireAfterSeconds: LOG_TTL_SECONDS // TTL index
  }
]);
```

### Conclusion

Our hybrid approach to database management combines MongoDB's flexibility with Prisma's structure, giving us a powerful foundation for our product catalog API. This architecture has proven effective for handling multilingual content, complex queries, and maintaining consistent data while providing excellent developer experience.