// Command demo serves an example editor.
//
//	go run github.com/alexisbcz/atelier/cmd/demo   # http://localhost:8090
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/alexisbcz/atelier"
	"github.com/alexisbcz/components/ui/runtime"
	"github.com/alexisbcz/icons/lucide"
	h "github.com/alexisbcz/libhtml"
)

func main() {
	addr := flag.String("addr", "localhost:8090", "listen address")
	flag.Parse()
	http.Handle(runtime.Prefix, runtime.Handler())
	http.Handle(atelier.AssetsPrefix, atelier.Assets())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		page := h.Html(h.Lang("en"),
			h.Head(
				h.Meta(h.Charset("utf-8")),
				h.Meta(h.Name("viewport"), h.Content("width=device-width, initial-scale=1")),
				h.Title("atelier"),
				atelier.Stylesheet(),
				runtime.Scripts(r),
			),
			h.Body(editor()),
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

func editor() h.Node {
	return atelier.Designer(atelier.Config{Document: sample, Class: "h-svh"},
		atelier.Header(),
		atelier.Content(
			atelier.Panel(atelier.SideLeft),
			atelier.Canvas(atelier.Frame(),
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
					),
				),
			),
			atelier.Panel(atelier.SideRight,
				atelier.Pane("Layer", nil, atelier.ActionPosition(), atelier.ActionSize()),
				atelier.Pane("Styles", nil, atelier.ActionRadius(), atelier.ActionBorder(), atelier.ActionShadow(), atelier.ActionFill(), atelier.ActionOpacity()),
				atelier.Pane("Transforms", nil, atelier.ActionRotate(), atelier.ActionFlip()),
				atelier.Pane("", []string{"text"}, atelier.ActionTextValue()),
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
