## Part 6: Logging Strategy

### Introduction

In this part, I'll explain our comprehensive logging strategy for the Express.js product catalog API. Proper logging is essential for debugging issues, monitoring performance, and auditing system activities in a production environment.

### Multi-level Logging Architecture

Our logging system consists of two primary components:

1. **Request Logging**: Captures HTTP request/response details
2. **System Logging**: Records internal application events and errors

Each type of log serves a distinct purpose while following a consistent format and storage pattern.

### MongoDB-Based Logging

We chose MongoDB for our logging infrastructure because it offers:

1. Flexible schema for different log types
2. Indexing for efficient queries
3. Built-in TTL (Time To Live) functionality
4. Scalability for high-volume logging

### Request Logging Implementation

Our request logger captures detailed information about each API call:

```javascript
export const requestLogger = async (req, res, next) => {
  const startTime = Date.now();
  
  // Store the original end function
  const originalEnd = res.end;
  
  // Override the end function
  res.end = async function(...args) {
    const responseTime = Date.now() - startTime;
    
    try {
      const db = client.db(DATABASE_NAME);
      const collection = db.collection('RequestLogs');
      
      // Clean request body if it exists and method is not GET
      const censoredBody = req.method !== 'GET' ? 
        censorSensitiveFields(req.body) : undefined;
      
      const logEntry = {
        timestamp: new Date(),
        method: req.method,
        protocol: req.protocol,
        host: req.get('host'),
        url: req.url,
        originalUrl: req.originalUrl,
        params: req.params,
        query: req.query,
        body: censoredBody,
        status: res.statusCode,
        responseTime,
        ip: req.ip,
        userAgent: req.get('user-agent'),
        userId: req.user?.id,
        userRole: req.user?.role
      };

      // Async insert without waiting
      collection.insertOne(logEntry).catch(err => 
        console.error('Error logging request:', err)
      );
      
      // Call the original end function
      originalEnd.apply(res, args);
    } catch (error) {
      console.error('Error in request logger:', error);
      originalEnd.apply(res, args);
    }
  };
  
  next();
};
```

Key features of our request logger:

1. **Response Time Tracking**: Measures API performance
2. **Sensitive Data Protection**: Automatically censors passwords and secrets
3. **User Context**: Associates requests with authenticated users
4. **Non-blocking Operation**: Uses asynchronous logging to avoid impacting response time

### System Logger Implementation

For internal application events, we use a dedicated system logger:

```javascript
export const LogLevel = {
  INFO: 'info',
  WARNING: 'warning',
  ERROR: 'error',
  DEBUG: 'debug'
};

export const LogCategory = {
  DATABASE: 'database',
  AUTH: 'auth',
  MEDIA: 'media',
  SYSTEM: 'system',
  CLEANUP: 'cleanup',
  SECURITY: 'security',
  EMAIL: 'email',
};

export const systemLogger = {
  log: async (level, category, message, metadata = {}) => {
    try {
      const db = client.db(DATABASE_NAME);
      const collection = db.collection('SystemLogs');
      
      const logEntry = {
        timestamp: new Date(),
        level,
        category,
        message,
        metadata,
      };

      await collection.insertOne(logEntry);
    } catch (error) {
      console.error('Error writing system log:', error);
    }
  },

  info: (category, message, metadata) => 
    systemLogger.log(LogLevel.INFO, category, message, metadata),
  
  warning: (category, message, metadata) => 
    systemLogger.log(LogLevel.WARNING, category, message, metadata),
  
  error: (category, message, metadata) => 
    systemLogger.log(LogLevel.ERROR, category, message, metadata),
  
  debug: (category, message, metadata) => 
    systemLogger.log(LogLevel.DEBUG, category, message, metadata)
};
```

The system logger provides:

1. **Categorized Events**: Organizes logs by system component
2. **Severity Levels**: Distinguishes between different types of events
3. **Structured Metadata**: Includes relevant context with each log entry
4. **Consistent API**: Simple methods for different log levels

### Log Indexing and Expiration

To maintain performance and manage storage efficiently, we created optimized indexes and TTL expiration:

```javascript
export const createLogIndexes = async () => {
  try {
    const db = client.db(DATABASE_NAME);
    
    // Create indexes for request logs
    await db.collection(COLLECTIONS.REQUEST_LOGS).createIndexes([
      { 
        key: { timestamp: -1 }, 
        name: "request_logs_timestamp_idx",
        expireAfterSeconds: LOG_TTL_SECONDS // TTL index
      },
      { key: { method: 1 }, name: "request_logs_method_idx" },
      { key: { status: 1 }, name: "request_logs_status_idx" },
      { key: { ip: 1 }, name: "request_logs_ip_idx" }
    ]);

    // Create indexes for system logs
    await db.collection(COLLECTIONS.SYSTEM_LOGS).createIndexes([
      { 
        key: { timestamp: -1 }, 
        name: "system_logs_timestamp_idx",
        expireAfterSeconds: LOG_TTL_SECONDS // TTL index
      },
      { key: { level: 1 }, name: "system_logs_level_idx" },
      { key: { category: 1 }, name: "system_logs_category_idx" }
    ]);

    console.log('Logging indexes created successfully');
  } catch (error) {
    console.error('Error creating logging indexes:', error);
  }
};
```

Our indexing strategy provides:

1. **TTL Indexes**: Automatically expire logs after a configurable period (default 7 days)
2. **Query Optimization**: Fast searches by common fields like timestamp, status, category
3. **Resource Management**: Prevents unbounded growth of log collections

### Security Considerations

Our logging system implements several security measures:

1. **Data Sanitization**: Sensitive information like passwords and tokens are automatically redacted
2. **Decoupled Storage**: Logs are stored separately from application data
3. **Minimal Overhead**: Non-blocking implementation prevents performance degradation

### Practical Application Examples

Here are some examples of how we use logging throughout the application:

#### Authentication Events

```javascript
// In login controller
systemLogger.info(LogCategory.AUTH, 'User login successful', { 
  userId: user.id, 
  email: user.email 
});

// In failed login attempt
systemLogger.warning(LogCategory.SECURITY, 'Failed login attempt', { 
  email, 
  ip: req.ip 
});
```

#### Rate Limiting

```javascript
// In rate limiter handler
systemLogger.warning(LogCategory.SECURITY, 'Rate limit exceeded', {
  ip: req.ip,
  path: req.path,
  method: req.method,
  userAgent: req.get('user-agent'),
  limit: max,
  windowMs
});
```

#### Media Operations

```javascript
// After successful file cleanup
systemLogger.info(LogCategory.CLEANUP, 'Files cleanup completed', { 
  orphanedFilesCount: deletedCount 
});
```

### Conclusion

Our comprehensive logging strategy provides visibility into the API's operations without compromising performance or security. By leveraging MongoDB's flexible document model and TTL capabilities, we've created a system that scales with our application while maintaining a reasonable storage footprint.

These logs serve multiple purposes: they help with debugging issues in production, provide insights for optimizing performance, create an audit trail for security analysis, and offer valuable metrics for understanding API usage patterns.