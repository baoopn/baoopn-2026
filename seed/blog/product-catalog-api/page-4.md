## Part 4: Media Management System

### Introduction

In this part, I'll explain our media management system for the Express.js product catalog API. Managing product images effectively is crucial for any e-commerce application, as it directly impacts the user experience and site performance.

### Choosing Cloudflare R2 for Storage

Our application uses Cloudflare R2 as the storage solution for product media. We chose R2 because it offers:

1. Cost-effective object storage with no egress fees
2. Compatibility with the S3 API
3. Global edge network for fast content delivery
4. Simple integration with our Express.js application

### Media Upload Architecture

Our media upload system consists of several key components:

#### 1. Upload Middleware

We use Multer to handle file uploads, with specific configurations for different file types:

```javascript
// Set up multer to handle images uploads
const upload_images = multer({
  storage: multer.memoryStorage(),
  fileFilter: imageFileFilter,
  limits: { fileSize: MAX_IMAGE_UPLOAD_SIZE },
});
export const uploadImages = upload_images.array('files', MAX_UPLOAD_FILES);
```

This middleware:
- Uses memory storage to prevent temporary files on disk
- Applies image-specific validation through a custom filter
- Enforces size limits and number of files to prevent oversized uploads

#### 2. Image Processing

Before storage, we process images to ensure consistent quality and format:

```javascript
export const processImage = async (buffer) => {
  const processedImage = await sharp(buffer)
    .rotate() // Rotate based on EXIF orientation
      .resize(1200, 900, {
        fit: sharp.fit.cover,
        position: sharp.strategy.entropy
      })
      .toFormat('jpeg')
      .jpeg({ quality: 90 })
      .toBuffer();

  return processedImage;
};
```

This processing step:
- Resizes images to a standard 1200x900 size
- Optimizes image quality for web display
- Preserves image orientation from EXIF data
- Excludes sensitive metadata such as location and device info

#### 3. Storage Organization

We organize files in the R2 storage using a consistent path structure:

```javascript
// Generate file name based on the product ID and timestamp
const timestamp = Date.now();
const fileName = `${id}_${timestamp}.jpg`;
const fileKey = `${PRODUCT_IMAGE_PREFIX}/${id}/${fileName}`;
```

This approach:
- Groups images by product ID for easy management
- Uses timestamps to ensure unique filenames
- Follows a consistent naming pattern for easy identification

### Media Management Features

Our system provides several key features for handling product media:

#### 1. Product Images

Products can have multiple images, which are managed through dedicated endpoints:

```javascript
// Upload product images
router.post('/product/:id/upload', authenticateJwt, isVerified, uploadImages, handleUploadErrors, uploadProductMedia);

// Delete product images
router.delete('/product/:id/delete', authenticateJwt, isVerified, deleteProductMedia);
```

Each product maintains an array of image references in the database, with the actual files stored in Cloudflare R2.

#### 2. Product Thumbnails

Products can have a single thumbnail image, used for listings and search results:

```javascript
// Upload product thumbnail
router.post('/product/:id/thumbnail', authenticateJwt, isVerified, uploadImages, handleUploadErrors, uploadThumbnail);

// Delete product thumbnail
router.delete('/product/:id/thumbnail', authenticateJwt, isVerified, deleteThumbnail);
```

#### 3. Page Media

For static content pages, we provide separate endpoints:

```javascript
// Upload page media
router.post('/page', authenticateJwt, isVerified, isAdmin, uploadFiles, handleUploadErrors, uploadPageMedia);

// List page media
router.get('/page', authenticateJwt, isVerified, listPageMedia);

// Delete page media
router.delete('/page', authenticateJwt, isVerified, isAdmin, deletePageMedia);
```

### Automatic Cleanup Process

To prevent storage bloat, we implemented an automatic cleanup system:

```javascript
// Schedule the cleanup job to run at 2 AM every day
cron.schedule('0 2 * * *', async () => {
  console.log('Running scheduled cleanup job...');
  await cleanUpFiles();
});
```

The cleanup process:
1. Lists all files in the R2 bucket
2. Retrieves all file references from the database
3. Identifies orphaned files (those in storage but not in the database)
4. Deletes orphaned files from R2 storage

This ensures our storage remains optimized and prevents costs from accumulating due to unused files.

### Security Considerations

Our media management system implements several security measures:

1. **Authentication & Authorization**: All media operations require authentication and appropriate permissions
2. **File Type Validation**: We restrict uploads to specific image formats
3. **Size Limits**: Files are limited to prevent DOS attacks
4. **Error Handling**: Detailed error handling prevents information leakage

### Conclusion

Our media management system provides a robust solution for handling product images in the e-commerce API. By combining Multer for uploads, Sharp for image processing, and Cloudflare R2 for storage, we've created a system that's both efficient and maintainable.

This architecture ensures that product images are consistently formatted, properly stored, and efficiently served to users, which is essential for a high-performing e-commerce platform.