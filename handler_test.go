package atelier

import (
	"context"
	"io"
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
	srv := httptest.NewServer(Handler(store, nil, Limits{}))
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
	srv := httptest.NewServer(Handler(NewMemoryStore(), Types{FrameType, badge}, Limits{}))
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
