package atelier

import (
	"encoding/json"
	"io"
	"math"
	"strconv"

	h "github.com/alexisbcz/libhtml"
)

// Template is a reusable group of layers the Library pane inserts.
// Positions are relative to the template: the editor centers the group in
// the frame and gives the layers new ids.
type Template struct {
	Name   string
	Layers []Layer
}

// bounds is the box of a template's root layers (pixel properties).
func (t Template) bounds() (x0, y0, x1, y1 float64) {
	x0, y0 = math.Inf(1), math.Inf(1)
	x1, y1 = math.Inf(-1), math.Inf(-1)
	for _, l := range t.Layers {
		if l.Parent != "" {
			continue
		}
		x, y := l.Px(VarX), l.Px(VarY)
		x0, y0 = math.Min(x0, x), math.Min(y0, y)
		x1, y1 = math.Max(x1, x+l.Px(VarWidth)), math.Max(y1, y+l.Px(VarHeight))
	}
	if math.IsInf(x0, 1) {
		return 0, 0, 1, 1
	}
	return
}

// PaneLibrary shows templates as live previews; a click inserts one.
func PaneLibrary(templates ...Template) h.Node {
	return h.NodeFunc(func(w io.Writer) error {
		cfg, _ := fromWriter(w)
		types := cfg.types()
		grid := h.Div(h.Data("slot", "atelier-library"), h.Class("grid grid-cols-2 gap-2 p-2"))
		for _, t := range templates {
			x0, y0, x1, y1 := t.bounds()
			bw, bh := math.Max(x1-x0, 1), math.Max(y1-y0, 1)
			// Fit the template in a 104x80 tile.
			scale := math.Min(104/bw, 80/bh)
			b, _ := json.Marshal(t.Layers)
			doc := Document{Layers: t.Layers}
			var layers []any
			for _, l := range doc.Children("") {
				layers = append(layers, types.RenderLayer(doc, l))
			}
			f := func(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }
			grid.Children(h.Button(h.Type("button"), Command("insert"),
				h.Data("atelier-template", string(b)), h.Aria("label", "Insert "+t.Name), h.Title(t.Name),
				h.Class("group flex flex-col gap-1.5 rounded-lg p-1.5 text-left hover:bg-accent"),
				h.Div(h.Class("relative flex h-20 items-center justify-center overflow-hidden rounded-md bg-muted"),
					h.Div(h.Class("relative shrink-0"),
						h.Style("width: "+f(bw*scale)+"px; height: "+f(bh*scale)+"px"),
						h.Div(h.Class("pointer-events-none absolute top-0 left-0 origin-top-left"),
							h.Style("transform: scale("+f(scale)+") translate("+f(-x0)+"px, "+f(-y0)+"px)"),
							h.Group(layers...)))),
				h.Span(h.Class("truncate px-0.5 text-xs text-muted-foreground group-hover:text-foreground"), t.Name),
			))
		}
		return grid.Render(w)
	})
}
