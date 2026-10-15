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
			atelier.Canvas(atelier.Frame()),
			atelier.Panel(atelier.SideRight),
		),
	)
}
