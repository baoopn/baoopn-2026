## Part 1: Project Overview and Tech Stack

### Introduction

In this blog post, I'll share my experience building a modern product catalog API using Express.js. This project demonstrates how to create a scalable and maintainable backend service for managing and showcasing products and digital assets.

![Product Catalog API Blog Image](/thumbs/product-catalog-api.png)

### Tech Stack Overview

Our application uses several key technologies:

- **Express.js**: The core framework for building the REST API
- **MongoDB**: The database for storing product and category data
- **Redis**: For caching and rate limiting
- **Cloudflare R2**: For media storage

### Project Structure

Here's how we organized our project:

```plaintext
src/
├── config/         // Configuration files
├── middlewares/    // Custom middleware functions
├── routes/         // API route definitions
├── utils/          // Utility functions
└── app.js          // Main application file
```

### Core Application Setup

Let's look at our main application file:

```javascript
import express from 'express';
import cookieParser from 'cookie-parser';
import cors from 'cors';

const app = express();

// Security-focused middleware setup
app.set('trust proxy', 'loopback, linklocal, uniquelocal');
app.use(cors(corsOptions));
app.use(express.json());
app.use(cookieParser(COOKIE_SECRET));
```

### API Routes Organization

We keep our routes modular and organized:

```javascript
import express from 'express';
import authRoutes from './authRoutes.js';
import productRoutes from './productRoutes.js';
import categoryRoutes from './categoryRoutes.js';
import mediaRoutes from './mediaRoutes.js';

const router = express.Router();

router.get('/', (req, res) => {
  res.status(200).send("Welcome to AT Creations API");
});

router.use('/auth', authRoutes);
router.use('/product', productRoutes);
router.use('/category', categoryRoutes);
router.use('/media', mediaRoutes);

export default router;
```
The main Express application then mounts all these routes under the `/api` path:

```javascript
// In app.js
app.use('/api/', allRoutes);
```

This organization allows each resource to have its own dedicated router file, making the codebase more maintainable as it grows.

### Key Features

1. **Error Handling**: Centralized error handling for consistent responses
2. **Rate Limiting**: Protection against abuse using Redis
3. **Logging System**: Comprehensive logging for debugging and monitoring
4. **Graceful Shutdown**: Proper handling of application shutdown

### Performance Considerations

We implemented several performance optimizations:
- Redis caching for frequently accessed data
- Database connection pooling
- Efficient media handling with Cloudflare R2
- Request logging for performance monitoring

### Security Measures

Security is a priority in our implementation:
- CORS configuration with specific origins
- Signed cookies for security
- Rate limiting to prevent DOS attacks
- Trust proxy settings for secure deployment
---
This project structure provides a solid foundation for building scalable e-commerce APIs. In next pages, we'll dive deeper into specific components like authentication, media handling, and caching strategies.