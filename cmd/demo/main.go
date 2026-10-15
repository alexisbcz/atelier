// Command demo serves an example editor.
//
//	go run github.com/alexisbcz/atelier/cmd/demo   # http://localhost:8090
package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"github.com/alexisbcz/atelier"
	"github.com/alexisbcz/components/ui/runtime"
	"github.com/alexisbcz/components/ui/tabs"
	"github.com/alexisbcz/icons/lucide"
	h "github.com/alexisbcz/libhtml"
)

func main() {
	addr := flag.String("addr", "localhost:8090", "listen address")
	sd := flag.String("sd", "", "stable-diffusion.cpp server URL for image generation (http://127.0.0.1:7860)")
	flag.Parse()
	http.Handle(runtime.Prefix, runtime.Handler())
	http.Handle(atelier.AssetsPrefix, atelier.Assets())
	// Documents are kept in memory; the editor saves after each change.
	store := atelier.NewMemoryStore()
	_ = store.Save(context.Background(), "sample", sample)
	opts := atelier.HandlerOptions{Types: types, Media: atelier.NewMemoryMedia()}
	if *sd != "" {
		opts.Generator = atelier.SDServer{URL: *sd}
		generate = "/designs/generate"
	}
	http.Handle("/designs/", http.StripPrefix("/designs/", atelier.Handler(store, opts)))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		doc, err := store.Load(r.Context(), "sample")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		page := h.Html(h.Lang("en"),
			h.Head(
				h.Meta(h.Charset("utf-8")),
				h.Meta(h.Name("viewport"), h.Content("width=device-width, initial-scale=1")),
				h.Title("atelier"),
				atelier.Stylesheet(),
				runtime.Scripts(r),
			),
			h.Body(editor(doc, r.URL.Query().Has("readonly"))),
		)
		_ = h.WriteHTTP(w, http.StatusOK, page)
	})
	log.Printf("atelier demo on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

// sample is a poster: a frame with a gradient fill and two text layers.
var sample = atelier.Document{Width: 1080, Height: 1350, Layers: []atelier.Layer{
	{ID: "bg", Name: "Background", Type: "frame", Vars: map[string]string{
		atelier.VarWidth: "1080px", atelier.VarHeight: "1350px",
		atelier.VarFill: "linear-gradient(160deg, #fde68a 0%, #f97316 55%, #7c2d12 100%)",
	}},
	{ID: "title", Name: "Title", Type: "text", Value: "Golden hour", Vars: map[string]string{
		atelier.VarWidth: "960px", atelier.VarHeight: "220px", atelier.VarX: "60px", atelier.VarY: "120px",
		atelier.VarFontSize: "160px", atelier.VarFontWeight: "800", atelier.VarColor: "#1c1917",
		atelier.VarLineHeight: "1", atelier.VarTextAlign: "center", atelier.VarLetterSpace: "-4px",
	}},
	{ID: "subtitle", Name: "Subtitle", Type: "text", Value: "A design editor for Go", Vars: map[string]string{
		atelier.VarWidth: "960px", atelier.VarHeight: "80px", atelier.VarX: "60px", atelier.VarY: "1160px",
		atelier.VarFontSize: "48px", atelier.VarFontWeight: "500", atelier.VarColor: "#fff7ed",
		atelier.VarTextAlign: "center",
	}},
}}

// badge is a custom layer type, rendered by Go on the server and in the
// editor (Config.RenderURL).
var badge = atelier.LayerType{
	Type: "badge", Name: "Badge", Icon: lucide.Sparkles, Key: "b",
	Defaults: atelier.Layer{Value: "New", Vars: map[string]string{
		atelier.VarWidth: "240px", atelier.VarHeight: "88px", atelier.VarFill: "#18181b",
		atelier.VarRadius: "44px", atelier.VarColor: "#ffffff", atelier.VarAlignItems: "center",
		atelier.VarJustify: "center",
	}},
	Render: func(l atelier.Layer) h.Node {
		return h.Span(h.Style("font: 600 40px/1 system-ui, sans-serif; letter-spacing: -0.5px"),
			"✦ ", l.Value)
	},
}

// generate is the generation endpoint ("" without a generator).
var generate string

// library holds the templates of the Library tab.
var library = []atelier.Template{
	{Name: "Heading", Layers: []atelier.Layer{{ID: "h", Name: "Heading", Type: "text", Value: "Big idea", Vars: map[string]string{
		atelier.VarWidth: "800px", atelier.VarHeight: "160px", atelier.VarFontSize: "128px", atelier.VarFontWeight: "800",
		atelier.VarLetterSpace: "-3px", atelier.VarTextAlign: "center", atelier.VarColor: "#18181b",
	}}}},
	{Name: "Card", Layers: []atelier.Layer{
		{ID: "c", Name: "Card", Type: "frame", Vars: map[string]string{
			atelier.VarWidth: "640px", atelier.VarHeight: "360px", atelier.VarFill: "#ffffff", atelier.VarRadius: "32px",
			atelier.VarShadow: "0 24px 48px #0000002e",
		}},
		{ID: "ct", Name: "Card title", Type: "text", Parent: "c", Value: "Card title", Vars: map[string]string{
			atelier.VarWidth: "560px", atelier.VarHeight: "80px", atelier.VarX: "40px", atelier.VarY: "48px",
			atelier.VarFontSize: "56px", atelier.VarFontWeight: "700", atelier.VarColor: "#18181b",
		}},
		{ID: "cb", Name: "Card body", Type: "text", Parent: "c", Value: "Write something short and clear here.", Vars: map[string]string{
			atelier.VarWidth: "560px", atelier.VarHeight: "120px", atelier.VarX: "40px", atelier.VarY: "150px",
			atelier.VarFontSize: "36px", atelier.VarColor: "#52525b", atelier.VarAlignItems: "flex-start",
		}},
	}},
	{Name: "Badge", Layers: []atelier.Layer{{ID: "b", Name: "Badge", Type: "badge", Value: "New", Vars: badge.Defaults.Vars}}},
	{Name: "Photo", Layers: []atelier.Layer{{ID: "p", Name: "Photo", Type: "image", Vars: map[string]string{
		atelier.VarWidth: "600px", atelier.VarHeight: "400px", atelier.VarRadius: "24px",
	}}}},
}

var types = atelier.Types{atelier.FrameType, atelier.TextType, atelier.ImageType, badge}

func editor(doc atelier.Document, readOnly bool) h.Node {
	return atelier.Designer(atelier.Config{Document: doc, Types: types, Class: "h-svh", ReadOnly: readOnly,
		SaveURL: "/designs/sample", RenderURL: "/designs/render", UploadURL: "/designs/upload", GenerateURL: generate},
		atelier.Header(
			atelier.PanelToggle(atelier.SideLeft, "Layers", lucide.Layers()),
			atelier.MainMenu(), h.Div(h.Class("flex-1")), atelier.ExportMenu(),
			atelier.PanelToggle(atelier.SideRight, "Inspector", lucide.SlidersHorizontal()),
		),
		atelier.Content(
			atelier.Panel(atelier.SideLeft,
				tabs.Tabs(tabs.Value("layers"), h.Class("gap-0"),
					h.Div(h.Class("p-2 pb-0"), tabs.List(h.Class("w-full"),
						tabs.Trigger(tabs.Value("layers"), "Layers"),
						tabs.Trigger(tabs.Value("library"), "Library"),
						tabs.Trigger(tabs.Value("generate"), "Generate"),
					)),
					tabs.Content(tabs.Value("layers"), atelier.PageSelector(), atelier.PaneLayers()),
					tabs.Content(tabs.Value("library"), atelier.PaneLibrary(library...)),
					tabs.Content(tabs.Value("generate"), atelier.PaneGenerate()),
				),
			),
			atelier.Canvas(atelier.Frame(), atelier.CanvasMenu(),
				atelier.Toolbar(
					atelier.ToolbarGroup(
						atelier.ToolbarButton("Undo", lucide.Undo2(), h.Data("atelier-command", "undo")),
						atelier.ToolbarButton("Redo", lucide.Redo2(), h.Data("atelier-command", "redo")),
					),
					atelier.ToolbarSeparator(),
					atelier.ToolbarGroup(
						atelier.ToolbarButton("Frame", lucide.Frame(), h.Data("atelier-command", "add"), h.Data("atelier-type", "frame")),
						atelier.ToolbarButton("Text", lucide.Type(), h.Data("atelier-command", "add"), h.Data("atelier-type", "text")),
						atelier.ToolbarButton("Image", lucide.Image(), h.Data("atelier-command", "add"), h.Data("atelier-type", "image")),
						atelier.ToolbarButton("Badge", lucide.Sparkles(), h.Data("atelier-command", "add"), h.Data("atelier-type", "badge")),
					),
					atelier.ToolbarSeparator(),
					atelier.ToolbarGroup(atelier.ToolbarZoom()),
				),
			),
			atelier.Panel(atelier.SideRight,
				atelier.Pane("Frame", nil, atelier.ActionDocumentSize(), atelier.ActionUnits()),
				atelier.Pane("Layer", nil, atelier.ActionAlign(), atelier.ActionPosition(), atelier.ActionSize()),
				atelier.Pane("Styles", nil, atelier.ActionRadius(), atelier.ActionBorder(), atelier.ActionShadow(), atelier.ActionFill(), atelier.ActionOpacity()),
				atelier.Pane("Transforms", nil, atelier.ActionRotate(), atelier.ActionFlip()),
				atelier.Pane("", []string{"text", "badge"}, atelier.ActionTextValue()),
				atelier.Pane("Text", []string{"text"},
					atelier.ActionFont(), atelier.ActionFontWeight(), atelier.ActionColor(), atelier.ActionFontSize(),
					atelier.ActionLineHeight(), atelier.ActionLetterSpacing(), atelier.ActionTextAlign(), atelier.ActionVerticalAlign(),
					atelier.ActionFontStyle(), atelier.ActionTextDecoration(), atelier.ActionTextTransform(),
					atelier.ActionTextShadow(), atelier.ActionTextStroke()),
				atelier.Pane("Image", []string{"image"}, atelier.ActionImageURL(), atelier.ActionImageFit()),
				atelier.Pane("Filters", []string{"image"}, atelier.ActionFilters()),
			),
		),
	)
}
