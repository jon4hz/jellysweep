package cache

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/database/databasetest"
	"github.com/stretchr/testify/require"
)

// pngServer serves a solid PNG of the given size at /poster.png.
func pngServer(t *testing.T, width, height int) *httptest.Server {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	mux := http.NewServeMux()
	mux.HandleFunc("/poster.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(buf.Bytes())
	})
	mux.HandleFunc("/not-an-image.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>nope</html>"))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func newImageCacheFixture(t *testing.T) (*ImageCache, *database.Client, string) {
	t.Helper()
	db, _ := databasetest.New(t)
	dir := filepath.Join(t.TempDir(), "images")
	return NewImageCache(dir, db), db, dir
}

func createMediaWithPoster(t *testing.T, db *database.Client, posterURL string) uint {
	t.Helper()
	require.NoError(t, db.CreateMediaItems(t.Context(), []database.Media{{
		JellyfinID:      "jf-" + posterURL,
		Title:           "Movie",
		ArrID:           1,
		MediaType:       database.MediaTypeMovie,
		DefaultDeleteAt: time.Now().Add(time.Hour),
		PosterURL:       posterURL,
	}}))
	items, err := db.GetMediaItems(t.Context(), true)
	require.NoError(t, err)
	return items[len(items)-1].ID
}

func serve(ic *ImageCache, mediaID uint, headers ...string) (*httptest.ResponseRecorder, error) {
	req := httptest.NewRequest(http.MethodGet, "/api/images/cache", nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	err := ic.ServeImage(req.Context(), mediaID, w, req)
	return w, err
}

func TestServeImageDownloadsCachesAndResizes(t *testing.T) {
	ic, db, dir := newImageCacheFixture(t)
	server := pngServer(t, 3000, 4500) // larger than any sane poster limit
	mediaID := createMediaWithPoster(t, db, server.URL+"/poster.png")

	rec, err := serve(ic, mediaID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Equal(t, "public, max-age=86400", rec.Header().Get("Cache-Control"))

	decoded, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	require.NoError(t, err)
	require.Less(t, decoded.Bounds().Dx(), 3000, "oversized posters must be scaled down")
	require.InDelta(t, 3000.0/4500.0, float64(decoded.Bounds().Dx())/float64(decoded.Bounds().Dy()), 0.01, "aspect ratio must be preserved")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "exactly one cached file, no temp file left behind")

	// Second request is served from disk: the origin must not be hit again.
	server.Close()
	rec, err = serve(ic, mediaID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestServeImageNotModified(t *testing.T) {
	ic, db, _ := newImageCacheFixture(t)
	server := pngServer(t, 10, 10)
	mediaID := createMediaWithPoster(t, db, server.URL+"/poster.png")

	rec, err := serve(ic, mediaID)
	require.NoError(t, err)
	lastModified := rec.Header().Get("Last-Modified")
	require.NotEmpty(t, lastModified)

	rec, err = serve(ic, mediaID, "If-Modified-Since", lastModified)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotModified, rec.Code)
	require.Empty(t, rec.Body.Bytes())
}

func TestServeImageNotFoundCases(t *testing.T) {
	ic, db, _ := newImageCacheFixture(t)

	rec, err := serve(ic, 0)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, rec.Code)

	rec, err = serve(ic, 999)
	require.Error(t, err)
	require.Equal(t, http.StatusNotFound, rec.Code)

	withoutPoster := createMediaWithPoster(t, db, "")
	rec, err = serve(ic, withoutPoster)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestServeImageDownloadFailures(t *testing.T) {
	ic, db, dir := newImageCacheFixture(t)
	server := pngServer(t, 10, 10)

	notImage := createMediaWithPoster(t, db, server.URL+"/not-an-image.png")
	rec, err := serve(ic, notImage)
	require.Error(t, err)
	require.Equal(t, http.StatusInternalServerError, rec.Code)

	missing := createMediaWithPoster(t, db, server.URL+"/missing.png")
	rec, err = serve(ic, missing)
	require.Error(t, err)
	require.Equal(t, http.StatusInternalServerError, rec.Code)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "failed downloads must not leave files behind")
}

func TestImageCacheClear(t *testing.T) {
	ic, db, dir := newImageCacheFixture(t)
	server := pngServer(t, 10, 10)
	mediaID := createMediaWithPoster(t, db, server.URL+"/poster.png")
	_, err := serve(ic, mediaID)
	require.NoError(t, err)

	require.NoError(t, ic.Clear(t.Context()))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
	_, err = os.Stat(dir)
	require.NoError(t, err, "the cache directory itself must survive")
}

func TestGetCachedImageURL(t *testing.T) {
	ic, _, _ := newImageCacheFixture(t)
	require.Empty(t, ic.GetCachedImageURL(""))
	url := ic.GetCachedImageURL("https://example.com/poster.jpg")
	require.Contains(t, url, "/api/images/")
	require.Equal(t, url, ic.GetCachedImageURL("https://example.com/poster.jpg"), "cache keys are deterministic")
}
