package cache

import (
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

type ImageCache struct {
	cacheDir string
	client   *http.Client
}

// NewImageCache creates a new image cache manager
func NewImageCache(cacheDir string) *ImageCache {
	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		log.Errorf("Failed to create cache directory: %v", err)
	}

	return &ImageCache{
		cacheDir: cacheDir,
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

// downloadAndCache downloads an image and saves it to the cache
func (ic *ImageCache) downloadAndCache(imageURL, cacheFilePath string) (string, error) {
	// Create a temporary file first
	tempFilePath := cacheFilePath + ".tmp"
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

	// Create the temporary file
	tempFile, err := os.Create(tempFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tempFile.Close()

	// Copy the image data to the temporary file
	_, err = io.Copy(tempFile, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to save image: %w", err)
	}

	// Move the temporary file to the final location (atomic operation)
	err = os.Rename(tempFilePath, cacheFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to move temp file: %w", err)
	}

	log.Infof("Cached image: %s -> %s", imageURL, cacheFilePath)
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
