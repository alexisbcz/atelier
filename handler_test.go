package atelier

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	h "github.com/alexisbcz/libhtml"
)

func TestValidate(t *testing.T) {
	ts := Types(DefaultTypes)
	ok := Document{Layers: []Layer{{ID: "f", Type: "frame"}, {ID: "t", Type: "text", Parent: "f"}}}
	if err := ts.Validate(ok, Limits{}); err != nil {
		t.Fatal(err)
	}
	for name, d := range map[string]Document{
		"duplicate":      {Layers: []Layer{{ID: "a", Type: "text"}, {ID: "a", Type: "text"}}},
		"unknown type":   {Layers: []Layer{{ID: "a", Type: "video"}}},
		"bad property":   {Layers: []Layer{{ID: "a", Type: "text", Vars: map[string]string{"--a;b": "1"}}}},
		"missing parent": {Layers: []Layer{{ID: "a", Type: "text", Parent: "nope"}}},
		"not container":  {Layers: []Layer{{ID: "a", Type: "text"}, {ID: "b", Type: "text", Parent: "a"}}},
		"cycle":          {Layers: []Layer{{ID: "a", Type: "frame", Parent: "b"}, {ID: "b", Type: "frame", Parent: "a"}}},
	} {
		if err := ts.Validate(d, Limits{}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if err := ts.Validate(Document{Layers: make([]Layer, 3)}, Limits{MaxLayers: 2}); err == nil {
		t.Error("layer limit not enforced")
	}
}

func TestHandlerRoundTrip(t *testing.T) {
	store := NewMemoryStore()
	srv := httptest.NewServer(Handler(store, HandlerOptions{}))
	defer srv.Close()
	post := func(path, body string) *http.Response {
		res, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := post("/poster", `{"width":100,"height":100,"layers":[{"id":"a","name":"A","type":"text","value":"hi"}]}`); res.StatusCode != http.StatusNoContent {
		t.Fatalf("save: %d", res.StatusCode)
	}
	d, err := store.Load(context.Background(), "poster")
	if err != nil || len(d.Layers) != 1 || d.Layers[0].Value != "hi" {
		t.Fatalf("stored %+v %v", d, err)
	}
	res, _ := http.Get(srv.URL + "/poster")
	if res.StatusCode != 200 {
		t.Fatalf("load: %d", res.StatusCode)
	}
	if res := post("/poster", `{"layers":[{"id":"a","type":"bogus"}]}`); res.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid document: %d", res.StatusCode)
	}
	if res := post("/../etc", `{}`); res.StatusCode != http.StatusNotFound {
		t.Errorf("bad id: %d", res.StatusCode)
	}
	if res, _ := http.Get(srv.URL + "/missing"); res.StatusCode != http.StatusNotFound {
		t.Errorf("missing: %d", res.StatusCode)
	}
}

func TestRenderEndpoint(t *testing.T) {
	badge := LayerType{Type: "badge", Name: "Badge", Render: func(l Layer) h.Node {
		return h.Span(h.Class("badge"), l.Value)
	}}
	srv := httptest.NewServer(Handler(NewMemoryStore(), HandlerOptions{Types: Types{FrameType, badge}}))
	defer srv.Close()
	res, err := http.Post(srv.URL+"/render", "application/json", strings.NewReader(`{"layer":{"id":"x","type":"badge","value":"<New>"}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || string(b) != `<span class="badge">&lt;New&gt;</span>` {
		t.Errorf("render: %d %q", res.StatusCode, b)
	}
}

func TestFileStore(t *testing.T) {
	s := FileStore{Dir: t.TempDir()}
	ctx := context.Background()
	if err := s.Save(ctx, "a", Document{Width: 10}); err != nil {
		t.Fatal(err)
	}
	if d, err := s.Load(ctx, "a"); err != nil || d.Width != 10 {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := s.Load(ctx, "../x"); err == nil {
		t.Error("path traversal accepted")
	}
	if _, err := s.Load(ctx, "nope"); err != ErrNotFound {
		t.Errorf("missing: %v", err)
	}
}

// fakeGen returns a 1x1 PNG.
type fakeGen struct{ got ImageRequest }

var onePixelPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\xcf\xc0\xf0\x1f\x00\x05\x00\x01\xff\x89\x99=\x1d\x00\x00\x00\x00IEND\xaeB`\x82")

func (f *fakeGen) Generate(_ context.Context, req ImageRequest) ([]byte, error) {
	f.got = req
	return onePixelPNG, nil
}

func TestGenerateAndMedia(t *testing.T) {
	gen := &fakeGen{}
	srv := httptest.NewServer(Handler(NewMemoryStore(), HandlerOptions{Media: NewMemoryMedia(), Generator: gen}))
	defer srv.Close()
	res, err := http.Post(srv.URL+"/generate", "application/json", strings.NewReader(`{"prompt":"a red fox","width":512,"height":768}`))
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ URL string }
	_ = json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != 200 || !strings.HasPrefix(out.URL, "media/") {
		t.Fatalf("generate: %d %q", res.StatusCode, out.URL)
	}
	if gen.got.Prompt != "a red fox" || gen.got.Height != 768 {
		t.Errorf("request = %+v", gen.got)
	}
	img, _ := http.Get(srv.URL + "/" + out.URL)
	if img.StatusCode != 200 || img.Header.Get("Content-Type") != "image/png" {
		t.Errorf("media: %d %s", img.StatusCode, img.Header.Get("Content-Type"))
	}
	for _, bad := range []string{`{"prompt":""}`, `{"prompt":"x","width":500}`, `{"prompt":"x","width":4096}`} {
		if res, _ := http.Post(srv.URL+"/generate", "application/json", strings.NewReader(bad)); res.StatusCode != 400 {
			t.Errorf("%s: %d", bad, res.StatusCode)
		}
	}
}

func TestUpload(t *testing.T) {
	srv := httptest.NewServer(Handler(NewMemoryStore(), HandlerOptions{Media: NewMemoryMedia()}))
	defer srv.Close()
	post := func(data []byte) *http.Response {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("file", "x.png")
		_, _ = fw.Write(data)
		_ = mw.Close()
		res, err := http.Post(srv.URL+"/upload", mw.FormDataContentType(), &body)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := post(onePixelPNG); res.StatusCode != 200 {
		t.Errorf("png upload: %d", res.StatusCode)
	}
	if res := post([]byte("<svg onload=alert(1)>")); res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("svg upload: %d", res.StatusCode)
	}
	if res, _ := http.Get(srv.URL + "/media/../../etc/passwd"); res.StatusCode != 404 {
		t.Errorf("traversal: %d", res.StatusCode)
	}
}
