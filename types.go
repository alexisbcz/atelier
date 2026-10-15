package atelier

import (
	"strings"

	"github.com/alexisbcz/icons/lucide"
	h "github.com/alexisbcz/libhtml"
)

// LayerType defines a kind of layer: how to create one and how to render
// it.
type LayerType struct {
	// Type is the identifier stored in Layer.Type ("text").
	Type string
	// Name is shown in the UI ("Text").
	Name string
	// Icon is shown in the toolbar and the layer tree.
	Icon func(children ...any) *h.Element
	// Defaults is the layer a new layer of this type starts from (its id
	// and type are set by the editor).
	Defaults Layer
	// Render returns the layer's content, placed inside the positioned
	// layer box. Container types render nothing: their children are
	// placed in the box.
	Render func(l Layer) h.Node
	// Container types hold child layers.
	Container bool
	// Key is the keyboard shortcut that adds a layer of this type ("t").
	Key string
}

// Frame is a box holding other layers.
var Frame = LayerType{
	Type: "frame", Name: "Frame", Icon: lucide.Frame, Container: true, Key: "f",
	Defaults: Layer{Name: "Frame", Vars: map[string]string{
		VarWidth: "400px", VarHeight: "300px", VarFill: "#e5e5e5",
	}},
}

// Text is a block of text.
var Text = LayerType{
	Type: "text", Name: "Text", Icon: lucide.Type, Key: "t",
	Defaults: Layer{Name: "Text", Value: "Text", Vars: map[string]string{
		VarWidth: "320px", VarHeight: "96px", VarFontSize: "64px", VarFontWeight: "400",
		VarColor: "#000000", VarLineHeight: "1.2", VarTextAlign: "center", VarAlignItems: "center",
	}},
	Render: func(l Layer) h.Node {
		return h.Div(h.Data("slot", "atelier-text"), l.Value)
	},
}

// Image shows a picture.
var Image = LayerType{
	Type: "image", Name: "Image", Icon: lucide.Image, Key: "i",
	Defaults: Layer{Name: "Image", Vars: map[string]string{
		VarWidth: "400px", VarHeight: "300px", VarObjectFit: "cover",
	}},
	Render: func(l Layer) h.Node {
		if l.Value == "" {
			return h.Div(h.Data("slot", "atelier-image-empty"), lucide.Image())
		}
		return h.Img(h.Data("slot", "atelier-image"), h.Src(l.Value), h.Alt(l.Name), h.Attr("draggable", "false"))
	},
}

// DefaultTypes are the built-in layer types.
var DefaultTypes = []LayerType{Frame, Text, Image}

// Types is a registry of layer types by name.
type Types []LayerType

// Get returns the layer type named name.
func (ts Types) Get(name string) (LayerType, bool) {
	for _, t := range ts {
		if t.Type == name {
			return t, true
		}
	}
	return LayerType{}, false
}

// New returns a new layer of type name, from its defaults.
func (ts Types) New(name, id string) (Layer, bool) {
	t, ok := ts.Get(name)
	if !ok {
		return Layer{}, false
	}
	l := t.Defaults.With(nil)
	l.ID, l.Type = id, t.Type
	if l.Name == "" {
		l.Name = t.Name
	}
	return l, true
}

// RenderLayer renders one layer (and, for containers, its children) as
// positioned boxes. Hidden layers render nothing.
func (ts Types) RenderLayer(d Document, l Layer) h.Node {
	if l.Hidden {
		return h.Group()
	}
	t, _ := ts.Get(l.Type)
	box := h.Div(
		h.Data("slot", "atelier-layer"),
		h.Data("layer-id", l.ID),
		h.Data("layer-type", l.Type),
		h.Class("atelier-layer atelier-layer-"+cssIdent(l.Type)),
	)
	if style := l.Style(); style != "" {
		box.Children(h.Style(style))
	}
	if l.Locked {
		box.Children(h.Data("locked", "true"))
	}
	if t.Render != nil {
		box.Children(t.Render(l))
	}
	for _, c := range d.Children(l.ID) {
		box.Children(ts.RenderLayer(d, c))
	}
	return box
}

// RenderLayers renders the root layers of a document, in paint order.
func (ts Types) RenderLayers(d Document) h.Node {
	var out []any
	for _, l := range d.Children("") {
		out = append(out, ts.RenderLayer(d, l))
	}
	return h.Group(out...)
}

// cssIdent keeps letters, digits, - and _ (layer type names go into a
// class name).
func cssIdent(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}
