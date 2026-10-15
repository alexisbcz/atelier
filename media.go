package atelier

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// MediaStore keeps the images documents use: uploads and generated
// images. Names are opaque ("3f9a....png").
type MediaStore interface {
	Put(ctx context.Context, data []byte, contentType string) (name string, err error)
	Get(ctx context.Context, name string) (data []byte, contentType string, err error)
}

// imageTypes are the media types accepted, by extension.
var imageTypes = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp",
}

var validMedia = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg|gif|webp)$`)

func newMediaName(contentType string) (string, error) {
	ext, ok := imageTypes[contentType]
	if !ok {
		return "", errors.New("atelier: unsupported image type " + contentType)
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b) + ext, nil
}

func mediaType(name string) string {
	for t, ext := range imageTypes {
		if strings.HasSuffix(name, ext) {
			return t
		}
	}
	return "application/octet-stream"
}

// MemoryMedia keeps images in memory.
type MemoryMedia struct {
	mu    sync.RWMutex
	files map[string][]byte
}

// NewMemoryMedia returns an empty MemoryMedia.
func NewMemoryMedia() *MemoryMedia { return &MemoryMedia{files: map[string][]byte{}} }

// Put implements MediaStore.
func (m *MemoryMedia) Put(_ context.Context, data []byte, contentType string) (string, error) {
	name, err := newMediaName(contentType)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[name] = data
	return name, nil
}

// Get implements MediaStore.
func (m *MemoryMedia) Get(_ context.Context, name string) ([]byte, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.files[name]
	if !ok {
		return nil, "", ErrNotFound
	}
	return b, mediaType(name), nil
}

// FileMedia keeps images as files in a directory.
type FileMedia struct{ Dir string }

// Put implements MediaStore.
func (f FileMedia) Put(_ context.Context, data []byte, contentType string) (string, error) {
	name, err := newMediaName(contentType)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(f.Dir, 0o755); err != nil {
		return "", err
	}
	return name, os.WriteFile(filepath.Join(f.Dir, name), data, 0o644)
}

// Get implements MediaStore.
func (f FileMedia) Get(_ context.Context, name string) ([]byte, string, error) {
	if !validMedia.MatchString(name) {
		return nil, "", ErrNotFound
	}
	b, err := os.ReadFile(filepath.Join(f.Dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", ErrNotFound
	}
	return b, mediaType(name), err
}

// sniffImage returns the image media type of data ("" when it isn't one of
// the accepted image types).
func sniffImage(data []byte) string {
	t := http.DetectContentType(data)
	if _, ok := imageTypes[t]; ok {
		return t
	}
	return ""
}
