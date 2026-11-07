## Part 2: Authentication and Authorization System

### Introduction

In this part, I'll explain how I built the authentication and authorization system for our Express.js product catalog API. Security is critical for any application, and this system ensures that only the right users can access specific parts of our API.

### Core Authentication Components

Our authentication system uses several key technologies:

- **JWT (JSON Web Tokens)**: For secure, stateless authentication
- **HTTP-only cookies**: For secure token storage
- **Redis**: For token management and rate limiting
- **bcrypt**: For password hashing

### Authentication Flow

Here's how a user moves through our authentication flow:

1. **Registration**: New users are created by admins or through a special registration endpoint
2. **Login**: Users provide credentials and receive JWT tokens
3. **Request Authentication**: Each protected endpoint verifies the user's token
4. **Token Refresh**: Long-running sessions use refresh tokens to maintain access

### JWT Implementation

We use signed JWTs stored in HTTP-only cookies for secure authentication:

```javascript
// JWT authentication middleware
const authenticateJwt = async (req, res, next) => {
  // Read signed JWT cookie
  const token = req.signedCookies.jwt;

  if (!token) {
    return res.status(401).json({ message: 'Unauthorized' });
  }

  jwt.verify(token, JWT_SECRET, async (err, decoded) => {
    if (err) {
      return res.status(401).json({ message: 'Unauthorized' });
    }

    // Check if the user exists in the database
    const user = await prisma.user.findUnique({
      where: { id: decoded.id }
    });

    if (!user) {
      return res.status(401).json({ message: 'Unauthorized' });
    }

    // Attach user information to req.user
    req.user = user;
    req.metadata = decoded;

    next();
  });
};
```
In our authentication middleware, we're using Prisma ORM to verify that a user exists in the database:
```javascript
// Check if the user exists in the database
const user = await prisma.user.findUnique({
  where: { id: decoded.id }
});
```
This code uses Prisma's clean API to query the database. Prisma abstracts the database operations, making the code more readable and type-safe while handling the underlying SQL operations.
If your project uses different technologies, you could achieve similar functionality with MongoDB queries, raw SQL, or other ORM tools that fit your stack.

### Role-Based Access Control

We implement a tiered permission system with three user roles:

1. **Admin**: Full access to all API features
2. **Manager**: Manages products and categories
3. **Staff**: Basic viewing permissions

The role checks are implemented as middleware:

```javascript
// Admin role check
export const isAdmin = (req, res, next) => {
  if (req.user && req.user.role === ADMIN_ROLE) {
    next();
  } else {
    res.status(403).json({ message: 'Forbidden: Admins only' });
  }
};
```

### Email Verification System

To ensure user authenticity, we implemented email verification:

1. When a user is registered, a verification token is generated
2. The token is stored in Redis with an expiration time
3. A verification link is sent to the user's email including the token as a query parameter.
4. When clicked, the token is verified and the user's account is marked as verified

### Security Features

Our authentication system includes several additional security measures:

#### 1. Brute Force Protection

We use Redis to track and limit login attempts:

```javascript
// Check if the user is blocked due to too many attempts
const attempts = await authRedis.get(loginAttemptsKey);
if (attempts && attempts >= LOGIN_ATTEMPTS_MAX) {
  return res.status(429).json({
    message: "Too many failed login attempts. Please try again later."
  });
}
```

#### 2. Password Requirements

We enforce strong password requirements:

```javascript
// Password strength regex
const passwordRegex = /^(?=.*\d)(?=.*[a-z])(?=.*[A-Z])(?=.*[^a-zA-Z0-9]).{8,}$/;
```

Password must be at least 8 characters, containing at least one uppercase letter, one lowercase letter, one number, and one special character.

#### 3. Token Expiration and Refresh

Access tokens expire after 1 hour, while refresh tokens last 7 days:

```javascript
// Generate token
const token = jwt.sign({ id: user.id }, JWT_SECRET, {
  expiresIn: "1h"
});
const refreshToken = jwt.sign({ id: user.id }, JWT_REFRESH_SECRET, {
  expiresIn: "7d"
});
```

#### 4. Secure Cookie Configuration

Tokens are stored in secure, HTTP-only cookies:

```javascript
// Set signed cookies
const COOKIE_OPTIONS = {
	httpOnly: true,
	secure: IS_PROD,
	sameSite: 'strict', 
	maxAge: JWT_ACCESS_TOKEN_EXPIRATION_MS,
	signed: true,
};
```

Key security aspects:
- `httpOnly: true` prevents JavaScript from accessing cookies, protecting against XSS attacks
- `secure: NODE_ENV !== 'development'` ensures cookies are sent only via HTTPS in production
- `sameSite: 'strict'` prevents cookies from being sent in cross-site requests, reducing CSRF risks
- `maxAge` sets time to live for access token cookie
- `signed: true` ensures cookie integrity through cryptographic signatures

### Password Reset Flow

We implemented a secure password reset flow that:

1. Generates a unique token for password resets
2. Stores the token in Redis with expiration
3. Sends an email with a reset link including the token as query parameter
4. When clicked, the token is verified then allows users to set a new password
5. Includes rate limiting to prevent abuse

### Conclusion

Our authentication system provides a robust security framework for our API. By combining JWTs, secure cookies, role-based access control, and Redis for session management, we've created a system that's both secure and scalable.

In the next part, we'll explore the caching strategy that keeps our API fast and responsive.