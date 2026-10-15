package atelier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	h "github.com/alexisbcz/libhtml"
)

// Store loads and saves documents by id.
type Store interface {
	Load(ctx context.Context, id string) (Document, error)
	Save(ctx context.Context, id string, d Document) error
}

// ErrNotFound is returned by a Store for an unknown document.
var ErrNotFound = errors.New("atelier: document not found")

// MemoryStore keeps documents in memory.
type MemoryStore struct {
	mu   sync.RWMutex
	docs map[string]Document
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore { return &MemoryStore{docs: map[string]Document{}} }

// Load implements Store.
func (s *MemoryStore) Load(_ context.Context, id string) (Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.docs[id]
	if !ok {
		return Document{}, ErrNotFound
	}
	return d, nil
}

// Save implements Store.
func (s *MemoryStore) Save(_ context.Context, id string, d Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs[id] = d
	return nil
}

// FileStore keeps each document as a JSON file in a directory.
type FileStore struct{ Dir string }

var validID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (s FileStore) path(id string) (string, error) {
	if !validID.MatchString(id) {
		return "", fmt.Errorf("atelier: invalid document id %q", id)
	}
	return filepath.Join(s.Dir, id+".json"), nil
}

// Load implements Store.
func (s FileStore) Load(_ context.Context, id string) (Document, error) {
	p, err := s.path(id)
	if err != nil {
		return Document{}, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, err
	}
	return ParseDocument(b)
}

// Save implements Store (atomically: write, then rename).
func (s FileStore) Save(_ context.Context, id string, d Document) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Limits bound what Handler accepts.
type Limits struct {
	MaxBytes       int64 // document request body (default 2 MiB)
	MaxLayers      int   // layers per document (default 1000)
	MaxUploadBytes int64 // uploaded image (default 10 MiB)
}

func (l Limits) withDefaults() Limits {
	if l.MaxBytes == 0 {
		l.MaxBytes = 2 << 20
	}
	if l.MaxLayers == 0 {
		l.MaxLayers = 1000
	}
	if l.MaxUploadBytes == 0 {
		l.MaxUploadBytes = 10 << 20
	}
	return l
}

// HandlerOptions configures Handler.
type HandlerOptions struct {
	// Types are the layer types documents may use (DefaultTypes when nil).
	Types  Types
	Limits Limits
	// Media stores uploaded and generated images (uploads and generation
	// are off without it).
	Media MediaStore
	// Generator generates images from prompts (generation is off without
	// it).
	Generator ImageGenerator
}

// Validate checks a document: sizes, unique ids, known types, parents that
// exist and are containers, no cycles.
func (ts Types) Validate(d Document, lim Limits) error {
	lim = lim.withDefaults()
	if d.Width < 0 || d.Height < 0 || d.Width > 100000 || d.Height > 100000 {
		return fmt.Errorf("atelier: invalid frame size %vx%v", d.Width, d.Height)
	}
	if len(d.Layers) > lim.MaxLayers {
		return fmt.Errorf("atelier: %d layers (max %d)", len(d.Layers), lim.MaxLayers)
	}
	if _, ok := Units[d.Unit]; d.Unit != "" && d.Unit != "px" && !ok {
		return fmt.Errorf("atelier: unknown unit %q", d.Unit)
	}
	if d.DPI < 0 || d.DPI > 10000 {
		return fmt.Errorf("atelier: invalid dpi %v", d.DPI)
	}
	pages := map[string]bool{}
	for _, p := range d.Pages {
		if p.ID == "" || len(p.ID) > 64 || pages[p.ID] {
			return fmt.Errorf("atelier: invalid or duplicate page id %q", p.ID)
		}
		pages[p.ID] = true
	}
	byID := map[string]Layer{}
	for _, l := range d.Layers {
		if l.Page != "" && !pages[l.Page] {
			return fmt.Errorf("atelier: layer %q: unknown page %q", l.ID, l.Page)
		}
		if l.ID == "" || len(l.ID) > 64 {
			return fmt.Errorf("atelier: invalid layer id %q", l.ID)
		}
		if _, dup := byID[l.ID]; dup {
			return fmt.Errorf("atelier: duplicate layer id %q", l.ID)
		}
		if _, ok := ts.Get(l.Type); !ok {
			return fmt.Errorf("atelier: layer %q: unknown type %q", l.ID, l.Type)
		}
		for k := range l.Vars {
			if !strings.HasPrefix(k, "--") || !validVar(k) {
				return fmt.Errorf("atelier: layer %q: invalid property %q", l.ID, k)
			}
		}
		byID[l.ID] = l
	}
	for _, l := range d.Layers {
		seen := map[string]bool{l.ID: true}
		for p := l.Parent; p != ""; p = byID[p].Parent {
			parent, ok := byID[p]
			if !ok {
				return fmt.Errorf("atelier: layer %q: unknown parent %q", l.ID, p)
			}
			if t, _ := ts.Get(parent.Type); !t.Container {
				return fmt.Errorf("atelier: layer %q: parent %q is not a container", l.ID, p)
			}
			if seen[p] {
				return fmt.Errorf("atelier: layer %q: parent cycle", l.ID)
			}
			seen[p] = true
		}
	}
	return nil
}

// Handler serves the editor's backend for the documents of a Store:
//
//	GET  {prefix}{id}           the document as JSON
//	POST {prefix}{id}           saves the document (Config.SaveURL)
//	POST {prefix}render         renders one layer (Config.RenderURL)
//	POST {prefix}upload         stores an image (Config.UploadURL)
//	POST {prefix}generate       generates an image (Config.GenerateURL)
//	GET  {prefix}media/{name}   serves a stored image
//
// Mount it under a prefix and protect it like any write endpoint (session
// check, CSRF):
//
//	mux.Handle("/designs/", http.StripPrefix("/designs/", atelier.Handler(store, atelier.HandlerOptions{})))
func Handler(store Store, opts HandlerOptions) http.Handler {
	types := opts.Types
	if types == nil {
		types = DefaultTypes
	}
	lim := opts.Limits.withDefaults()
	// One generation at a time: they share the GPU.
	gpu := make(chan struct{}, 1)
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	store2 := func(w http.ResponseWriter, r *http.Request, data []byte) {
		t := sniffImage(data)
		if t == "" {
			http.Error(w, "not a PNG, JPEG, GIF or WebP image", http.StatusUnsupportedMediaType)
			return
		}
		name, err := opts.Media.Put(r.Context(), data, t)
		if err != nil {
			http.Error(w, "store failed", http.StatusInternalServerError)
			return
		}
		// Relative to the handler: the editor resolves it against the URL
		// it called.
		writeJSON(w, map[string]string{"url": "media/" + name})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.Trim(r.URL.Path, "/")
		switch {
		case r.Method == http.MethodPost && id == "render":
			var req struct {
				Layer Layer `json:"layer"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, lim.MaxBytes)).Decode(&req); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			t, ok := types.Get(req.Layer.Type)
			if !ok {
				http.Error(w, "unknown layer type", http.StatusBadRequest)
				return
			}
			// Only the layer's own content: its box and children are the
			// editor's.
			if t.Render == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_ = h.WriteHTTP(w, http.StatusOK, t.Render(req.Layer))
		case r.Method == http.MethodPost && id == "upload":
			if opts.Media == nil {
				http.NotFound(w, r)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, lim.MaxUploadBytes+1<<20)
			f, _, err := r.FormFile("file")
			if err != nil {
				http.Error(w, "expected a multipart file field \"file\"", http.StatusBadRequest)
				return
			}
			defer f.Close()
			data, err := io.ReadAll(io.LimitReader(f, lim.MaxUploadBytes+1))
			if err != nil || int64(len(data)) > lim.MaxUploadBytes {
				http.Error(w, "image too large", http.StatusRequestEntityTooLarge)
				return
			}
			store2(w, r, data)
		case r.Method == http.MethodPost && id == "generate":
			if opts.Media == nil || opts.Generator == nil {
				http.NotFound(w, r)
				return
			}
			var req ImageRequest
			if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			if err := validateImageRequest(req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), generationTimeout)
			defer cancel()
			select {
			case gpu <- struct{}{}:
			case <-ctx.Done():
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			data, err := opts.Generator.Generate(ctx, req)
			<-gpu
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			store2(w, r, data)
		case r.Method == http.MethodGet && strings.HasPrefix(id, "media/"):
			if opts.Media == nil {
				http.NotFound(w, r)
				return
			}
			data, t, err := opts.Media.Get(r.Context(), strings.TrimPrefix(id, "media/"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", t)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			_, _ = w.Write(data)
		case !validID.MatchString(id):
			http.NotFound(w, r)
		case r.Method == http.MethodGet:
			d, err := store.Load(r.Context(), id)
			if errors.Is(err, ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			if err != nil {
				http.Error(w, "load failed", http.StatusInternalServerError)
				return
			}
			writeJSON(w, d)
		case r.Method == http.MethodPost:
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
				return
			}
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, lim.MaxBytes))
			if err != nil {
				http.Error(w, "document too large", http.StatusRequestEntityTooLarge)
				return
			}
			d, err := ParseDocument(body)
			if err == nil {
				err = types.Validate(d, lim)
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := store.Save(r.Context(), id, d); err != nil {
				http.Error(w, "save failed", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
