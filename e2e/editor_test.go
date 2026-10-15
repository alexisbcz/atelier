// Package e2e drives the editor in Chrome (chromedp). It is its own module,
// so the atelier package keeps no test dependencies:
//
//	cd e2e && go test ./...
//
// Tests skip when no Chrome is found; set CHROME to its path otherwise.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/alexisbcz/atelier"
	"github.com/alexisbcz/components/ui/runtime"
	h "github.com/alexisbcz/libhtml"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

var sample = atelier.Document{Width: 1080, Height: 1080, Layers: []atelier.Layer{
	{ID: "bg", Name: "Background", Type: "frame", Vars: map[string]string{
		atelier.VarWidth: "1080px", atelier.VarHeight: "1080px", atelier.VarFill: "#fde68a",
	}},
	{ID: "title", Name: "Title", Type: "text", Value: "Hello", Vars: map[string]string{
		atelier.VarWidth: "800px", atelier.VarHeight: "200px", atelier.VarX: "140px", atelier.VarY: "100px",
		atelier.VarFontSize: "120px",
	}},
	{ID: "note", Name: "Note", Type: "text", Value: "Note", Vars: map[string]string{
		atelier.VarWidth: "400px", atelier.VarHeight: "100px", atelier.VarX: "340px", atelier.VarY: "800px",
	}},
}}

// server serves an editor over a fresh copy of the sample document.
func server(t *testing.T, readOnly bool) (*httptest.Server, *atelier.MemoryStore) {
	t.Helper()
	store := atelier.NewMemoryStore()
	_ = store.Save(context.Background(), "doc", sample)
	mux := http.NewServeMux()
	mux.Handle(runtime.Prefix, runtime.Handler())
	mux.Handle(atelier.AssetsPrefix, atelier.Assets())
	mux.Handle("/designs/", http.StripPrefix("/designs/", atelier.Handler(store, atelier.HandlerOptions{
		Media: atelier.NewMemoryMedia(), Generator: fakeGenerator{},
	})))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		doc, _ := store.Load(r.Context(), "doc")
		editor := atelier.Designer(atelier.Config{Document: doc, Class: "h-svh", ReadOnly: readOnly,
			SaveURL: "/designs/doc", UploadURL: "/designs/upload", GenerateURL: "/designs/generate"},
			atelier.Header(atelier.MainMenu(), atelier.ExportMenu()),
			atelier.Content(
				atelier.Panel(atelier.SideLeft, atelier.PageSelector(), atelier.PaneLayers(), atelier.PaneGenerate()),
				atelier.Canvas(atelier.Frame(), atelier.CanvasMenu()),
				atelier.Panel(atelier.SideRight,
					atelier.Pane("Layer", nil, atelier.ActionAlign(), atelier.ActionPosition(), atelier.ActionSize()),
					atelier.Pane("Text", []string{"text"}, atelier.ActionFontSize(), atelier.ActionColor()),
				),
			),
		)
		page := h.Html(h.Head(atelier.Stylesheet(), runtime.Scripts(r)), h.Body(editor))
		_ = h.WriteHTTP(w, http.StatusOK, page)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, store
}

func chromePath() string {
	if p := os.Getenv("CHROME"); p != "" {
		return p
	}
	// Playwright's headless shell, when installed.
	home, _ := os.UserHomeDir()
	for _, glob := range []string{
		filepath.Join(home, "Library/Caches/ms-playwright/chromium_headless_shell-*/*/chrome-headless-shell"),
		filepath.Join(home, ".cache/ms-playwright/chromium_headless_shell-*/*/chrome-headless-shell"),
	} {
		if m, _ := filepath.Glob(glob); len(m) > 0 {
			return m[len(m)-1]
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// browser opens the editor and returns a chromedp context.
func browser(t *testing.T, url string) context.Context {
	t.Helper()
	path := chromePath()
	if path == "" {
		t.Skip("no Chrome found (set CHROME)")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(path), chromedp.WindowSize(1440, 900))
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancelCtx := chromedp.NewContext(alloc)
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(func() { cancelTimeout(); cancelCtx(); cancelAlloc() })
	if err := chromedp.Run(ctx, chromedp.Navigate(url), chromedp.WaitVisible(`[data-slot="atelier-frame"]`),
		chromedp.Poll(`!!document.querySelector('[data-slot="atelier"]').atelier`, nil)); err != nil {
		t.Fatal(err)
	}
	return ctx
}

// eval evaluates a JavaScript expression (the editor is `ed`).
func eval[T any](t *testing.T, ctx context.Context, expr string) T {
	t.Helper()
	var out T
	js := `(() => { const ed = document.querySelector('[data-slot="atelier"]').atelier; return ` + expr + ` })()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &out)); err != nil {
		t.Fatalf("%s: %v", expr, err)
	}
	return out
}

// center returns the center of an element, in page coordinates.
func center(t *testing.T, ctx context.Context, sel string) (float64, float64) {
	t.Helper()
	var r struct{ X, Y, W, H float64 }
	js := `(() => { const r = document.querySelector(` + jsString(sel) + `).getBoundingClientRect(); return {X: r.x, Y: r.y, W: r.width, H: r.height} })()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &r)); err != nil {
		t.Fatal(err)
	}
	return r.X + r.W/2, r.Y + r.H/2
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func drag(ctx context.Context, x0, y0, x1, y1 float64) error {
	return chromedp.Run(ctx,
		chromedp.MouseEvent(input.MousePressed, x0, y0, chromedp.ButtonLeft),
		chromedp.MouseEvent(input.MouseMoved, (x0+x1)/2, (y0+y1)/2, chromedp.ButtonLeft),
		chromedp.MouseEvent(input.MouseMoved, x1, y1, chromedp.ButtonLeft),
		chromedp.MouseEvent(input.MouseReleased, x1, y1, chromedp.ButtonLeft),
	)
}

func key(ctx context.Context, k string, mods ...input.Modifier) error {
	var m input.Modifier
	for _, x := range mods {
		m |= x
	}
	return chromedp.Run(ctx, chromedp.KeyEvent(k, chromedp.KeyModifiers(m)))
}

// modKey is the editor's "mod": ⌘ on macOS, Ctrl elsewhere.
func modKey() input.Modifier {
	if goruntime.GOOS == "darwin" {
		return input.ModifierMeta
	}
	return input.ModifierCtrl
}

func TestSelectMoveUndo(t *testing.T) {
	srv, _ := server(t, false)
	ctx := browser(t, srv.URL)
	x, y := center(t, ctx, `[data-slot="atelier-layer"][data-layer-id="title"]`)
	if err := drag(ctx, x, y, x+50, y+40); err != nil {
		t.Fatal(err)
	}
	if got := eval[[]string](t, ctx, `ed.selection`); len(got) != 1 || got[0] != "title" {
		t.Fatalf("selection = %v", got)
	}
	if moved := eval[string](t, ctx, `ed.layer("title").vars["--at-x"]`); moved == "140px" {
		t.Fatal("title did not move")
	}
	if err := key(ctx, "z", modKey()); err != nil {
		t.Fatal(err)
	}
	if got := eval[string](t, ctx, `ed.layer("title").vars["--at-x"]`); got != "140px" {
		t.Errorf("after undo x = %s", got)
	}
}

func TestInspectorAndSave(t *testing.T) {
	srv, store := server(t, false)
	ctx := browser(t, srv.URL)
	x, y := center(t, ctx, `[data-slot="atelier-layer"][data-layer-id="note"]`)
	if err := chromedp.Run(ctx, chromedp.MouseClickXY(x, y),
		chromedp.SetValue(`input[data-atelier-var="--at-font-size"]`, "72", chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('input[data-atelier-var="--at-font-size"]').dispatchEvent(new Event("input", {bubbles: true}))`, nil),
		chromedp.Evaluate(`document.querySelector('input[data-atelier-var="--at-font-size"]').dispatchEvent(new Event("change", {bubbles: true}))`, nil),
		chromedp.Click(`[data-atelier-edge="center"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	if got := eval[string](t, ctx, `ed.layer("note").vars["--at-font-size"]`); got != "72px" {
		t.Errorf("font size = %s", got)
	}
	if got := eval[string](t, ctx, `ed.layer("note").vars["--at-x"]`); got != "340px" {
		t.Errorf("centered x = %s, want 340px", got)
	}
	// The editor saves after a pause.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d, _ := store.Load(context.Background(), "doc")
		if l, _ := d.Find("note"); l.Vars[atelier.VarFontSize] == "72px" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("document not saved")
}

func TestAddGroupTreeAndPages(t *testing.T) {
	srv, _ := server(t, false)
	ctx := browser(t, srv.URL)
	if err := chromedp.Run(ctx, chromedp.Focus(`[data-slot="atelier-canvas"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if err := key(ctx, "t"); err != nil {
		t.Fatal(err)
	}
	if n := eval[int](t, ctx, `ed.doc.layers.length`); n != 4 {
		t.Fatalf("after t: %d layers", n)
	}
	if rows := eval[int](t, ctx, `document.querySelectorAll("[data-layer-row]").length`); rows != 4 {
		t.Errorf("tree rows = %d", rows)
	}
	eval[any](t, ctx, `ed.select(["title", "note"])`)
	if err := key(ctx, "g", modKey()); err != nil {
		t.Fatal(err)
	}
	if got := eval[string](t, ctx, `ed.layer("title").parent ?? ""`); got == "" {
		t.Error("title not grouped")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`[data-atelier-command="add-page"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if n := eval[int](t, ctx, `document.querySelectorAll('[data-slot="atelier-frame"] [data-slot="atelier-layer"]').length`); n != 0 {
		t.Errorf("new page shows %d layers", n)
	}
}

func TestReadOnly(t *testing.T) {
	srv, _ := server(t, true)
	ctx := browser(t, srv.URL)
	before := eval[string](t, ctx, `JSON.stringify(ed.doc)`)
	x, y := center(t, ctx, `[data-slot="atelier-layer"][data-layer-id="title"]`)
	if err := drag(ctx, x, y, x+80, y+80); err != nil {
		t.Fatal(err)
	}
	_ = key(ctx, "t")
	_ = key(ctx, "Delete")
	if after := eval[string](t, ctx, `JSON.stringify(ed.doc)`); after != before {
		t.Error("read-only document changed")
	}
}

// fakeGenerator draws a solid image of the requested size.
type fakeGenerator struct{}

func (fakeGenerator) Generate(_ context.Context, req atelier.ImageRequest) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, req.Width, req.Height))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{37, 99, 235, 255}}, image.Point{}, draw.Src)
	var b bytes.Buffer
	err := png.Encode(&b, img)
	return b.Bytes(), err
}

func TestGenerate(t *testing.T) {
	srv, _ := server(t, false)
	ctx := browser(t, srv.URL)
	if err := chromedp.Run(ctx,
		chromedp.SendKeys(`[data-atelier-gen-prompt]`, "a blue square", chromedp.ByQuery),
		chromedp.Click(`[data-atelier-command="generate"]`, chromedp.ByQuery),
		chromedp.Poll(`/Done/.test(document.querySelector("[data-atelier-gen-status]").textContent)`, nil),
	); err != nil {
		t.Fatal(err)
	}
	got := eval[string](t, ctx, `(() => { const l = ed.doc.layers.at(-1); return l.type + " " + l.value })()`)
	if !strings.HasPrefix(got, "image /designs/media/") {
		t.Errorf("generated layer = %q", got)
	}
	// The image loads from the media store.
	if !eval[bool](t, ctx, `document.querySelector('[data-slot="atelier-frame"] img[src^="/designs/media/"]') !== null`) {
		t.Error("no generated image on the canvas")
	}
}
