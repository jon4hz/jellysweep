package cache

import (
	"crypto/md5"
	"fmt"
	"image"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/disintegration/imaging"
)

type ImageCache struct {
	cacheDir  string
	client    *http.Client
	maxWidth  int // Maximum width for scaled images
	maxHeight int // Maximum height for scaled images
	quality   int // JPEG quality (1-100)
}

// NewImageCache creates a new image cache manager with scaling options
func NewImageCache(cacheDir string) *ImageCache {
	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		log.Errorf("Failed to create cache directory: %v", err)
	}

	return &ImageCache{
		cacheDir:  cacheDir,
		maxWidth:  340, // Default max width: 340px
		maxHeight: 500, // Default max height: 500px
		quality:   85,  // Default JPEG quality: 85%
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewImageCacheWithOptions creates a new image cache manager with custom scaling options
func NewImageCacheWithOptions(cacheDir string, maxWidth, maxHeight, quality int) *ImageCache {
	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		log.Errorf("Failed to create cache directory: %v", err)
	}

	return &ImageCache{
		cacheDir:  cacheDir,
		maxWidth:  maxWidth,
		maxHeight: maxHeight,
		quality:   quality,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// getCacheKey generates a cache key from the image URL
func (ic *ImageCache) getCacheKey(imageURL string) string {
	hash := md5.Sum([]byte(imageURL))
	return fmt.Sprintf("%x", hash)
}

// getCacheFilePath returns the file path for a cached image
func (ic *ImageCache) getCacheFilePath(imageURL string) string {
	key := ic.getCacheKey(imageURL)
	// Try to preserve the original file extension
	ext := filepath.Ext(imageURL)
	if ext == "" || len(ext) > 10 { // Fallback for URLs without extension or very long extensions
		ext = ".jpg"
	}
	return filepath.Join(ic.cacheDir, key+ext)
}

// GetCachedImagePath returns the local path for an image, downloading it if necessary
func (ic *ImageCache) GetCachedImagePath(imageURL string) (string, error) {
	if imageURL == "" {
		return "", nil
	}

	cacheFilePath := ic.getCacheFilePath(imageURL)

	// Check if file already exists
	if _, err := os.Stat(cacheFilePath); err == nil {
		log.Debugf("Using cached image: %s", cacheFilePath)
		return cacheFilePath, nil
	}

	// Download and cache the image
	log.Debugf("Downloading image from: %s", imageURL)
	return ic.downloadAndCache(imageURL, cacheFilePath)
}

// downloadAndCache downloads an image, scales it, and saves it to the cache
func (ic *ImageCache) downloadAndCache(imageURL, cacheFilePath string) (string, error) {
	// Create a temporary file first - keep the original extension but add a prefix
	dir := filepath.Dir(cacheFilePath)
	base := filepath.Base(cacheFilePath)
	tempFilePath := filepath.Join(dir, "tmp_"+base)
	defer os.Remove(tempFilePath) // Clean up temp file if something goes wrong

	// Download the image
	resp, err := ic.client.Get(imageURL)
	if err != nil {
		return "", fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download image: HTTP %d", resp.StatusCode)
	}

	// Validate content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		return "", fmt.Errorf("invalid content type: %s", contentType)
	}

	// Decode the image
	img, format, err := image.Decode(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to decode image: %w", err)
	}

	// Get original dimensions
	bounds := img.Bounds()
	originalWidth := bounds.Dx()
	originalHeight := bounds.Dy()

	// Check if resizing is needed
	needsResize := originalWidth > ic.maxWidth || originalHeight > ic.maxHeight

	var processedImg image.Image = img
	if needsResize {
		// Calculate new dimensions while maintaining aspect ratio
		newWidth, newHeight := ic.calculateScaledDimensions(originalWidth, originalHeight)

		// Resize the image using high-quality Lanczos resampling
		processedImg = imaging.Resize(img, newWidth, newHeight, imaging.Lanczos)

		log.Debugf("Resized image from %dx%d to %dx%d for: %s",
			originalWidth, originalHeight, newWidth, newHeight, imageURL)
	}

	// Create the temporary file
	tempFile, err := os.Create(tempFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tempFile.Close()

	// Save the processed image
	err = ic.saveImage(processedImg, tempFile, format, cacheFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to save processed image: %w", err)
	}

	// Move the temporary file to the final location (atomic operation)
	err = os.Rename(tempFilePath, cacheFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to move temp file: %w", err)
	}

	if needsResize {
		log.Infof("Cached and resized image: %s -> %s (original: %dx%d, cached: %dx%d)",
			imageURL, cacheFilePath, originalWidth, originalHeight,
			processedImg.Bounds().Dx(), processedImg.Bounds().Dy())
	} else {
		log.Infof("Cached image: %s -> %s", imageURL, cacheFilePath)
	}

	return cacheFilePath, nil
}

// GetCachedImageURL returns a local URL for an image, downloading it if necessary
func (ic *ImageCache) GetCachedImageURL(imageURL string) string {
	if imageURL == "" {
		return ""
	}

	// Return a URL that points to our cache endpoint
	key := ic.getCacheKey(imageURL)
	return fmt.Sprintf("/api/images/%s", key)
}

// ServeImage serves a cached image or downloads it if not cached
func (ic *ImageCache) ServeImage(imageURL string, w http.ResponseWriter, r *http.Request) error {
	if imageURL == "" {
		http.NotFound(w, r)
		return nil
	}

	cacheFilePath, err := ic.GetCachedImagePath(imageURL)
	if err != nil {
		log.Errorf("Failed to get cached image: %v", err)
		http.Error(w, "Failed to get image", http.StatusInternalServerError)
		return err
	}

	// Open the cached file
	file, err := os.Open(cacheFilePath)
	if err != nil {
		log.Errorf("Failed to open cached image: %v", err)
		http.Error(w, "Failed to open image", http.StatusInternalServerError)
		return err
	}
	defer file.Close()

	// Get file info to set appropriate headers
	fileInfo, err := file.Stat()
	if err != nil {
		log.Errorf("Failed to get file info: %v", err)
		http.Error(w, "Failed to get file info", http.StatusInternalServerError)
		return err
	}

	// Set appropriate headers
	ext := filepath.Ext(cacheFilePath)
	switch ext {
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".webp":
		w.Header().Set("Content-Type", "image/webp")
	case ".gif":
		w.Header().Set("Content-Type", "image/gif")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	// Set cache headers (cache for 24 hours)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Last-Modified", fileInfo.ModTime().Format(http.TimeFormat))

	// Check if client has cached version
	if modifiedSince := r.Header.Get("If-Modified-Since"); modifiedSince != "" {
		if t, err := time.Parse(http.TimeFormat, modifiedSince); err == nil {
			if fileInfo.ModTime().Before(t.Add(1 * time.Second)) {
				w.WriteHeader(http.StatusNotModified)
				return nil
			}
		}
	}

	// Serve the file
	http.ServeContent(w, r, fileInfo.Name(), fileInfo.ModTime(), file)
	return nil
}

// CleanupOldImages removes cached images older than the specified duration
func (ic *ImageCache) CleanupOldImages(maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge)

	return filepath.Walk(ic.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && info.ModTime().Before(cutoff) {
			log.Debugf("Removing old cached image: %s", path)
			return os.Remove(path)
		}

		return nil
	})
}

// calculateScaledDimensions calculates new dimensions while maintaining aspect ratio
func (ic *ImageCache) calculateScaledDimensions(originalWidth, originalHeight int) (int, int) {
	// If both dimensions are within limits, don't scale
	if originalWidth <= ic.maxWidth && originalHeight <= ic.maxHeight {
		return originalWidth, originalHeight
	}

	// Calculate scaling factors for width and height
	widthRatio := float64(ic.maxWidth) / float64(originalWidth)
	heightRatio := float64(ic.maxHeight) / float64(originalHeight)

	// Use the smaller ratio to ensure both dimensions fit within limits
	ratio := widthRatio
	if heightRatio < widthRatio {
		ratio = heightRatio
	}

	// Calculate new dimensions
	newWidth := int(float64(originalWidth) * ratio)
	newHeight := int(float64(originalHeight) * ratio)

	return newWidth, newHeight
}

// saveImage saves the processed image to the file with appropriate format and quality
func (ic *ImageCache) saveImage(img image.Image, file *os.File, originalFormat, filePath string) error {
	// Determine output format based on file extension and original format
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".jpg", ".jpeg":
		// Save as JPEG with specified quality
		return imaging.Save(img, file.Name(), imaging.JPEGQuality(ic.quality))
	case ".png":
		// Save as PNG (lossless)
		return imaging.Save(img, file.Name(), imaging.PNGCompressionLevel(6))
	case ".webp":
		// Try to save as WebP, fallback to JPEG if not supported
		if err := imaging.Save(img, file.Name()); err != nil {
			log.Debugf("WebP save failed, falling back to JPEG: %v", err)
			return imaging.Save(img, file.Name(), imaging.JPEGQuality(ic.quality))
		}
		return nil
	default:
		// For unknown formats, save as JPEG
		log.Debugf("Unknown format %s, saving as JPEG", ext)
		return imaging.Save(img, file.Name(), imaging.JPEGQuality(ic.quality))
	}
}
