// Package atelier is a composable design editor for Go web applications:
// a canvas of layers (frames, text, images and your own types), panels of
// actions that edit them, a toolbar, history, zoom and export. The editor
// shell renders on the server with libhtml; a small JavaScript runtime makes
// it interactive.
//
// A document is a list of layers. Each layer is plain data: an id, a name,
// a type, a value (the text of a text layer, the URL of an image layer) and
// CSS custom properties that describe its box and styles. The same
// properties drive the server rendering and the browser editing, so a
// document renders identically in both.
package atelier

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Layer is one element of a document.
type Layer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	// Value is the layer's content: the text of a text layer, the URL of an
	// image layer. Layer types decide what it means.
	Value string `json:"value,omitempty"`
	// Vars are the layer's CSS custom properties ("--at-width": "320px").
	// See the Var* constants for the properties the stylesheet maps.
	Vars map[string]string `json:"vars,omitempty"`
	// Parent is the id of the frame holding the layer ("" for the root).
	Parent string `json:"parent,omitempty"`
	// Page is the id of the page the layer is on ("" for the first page;
	// children follow their frame).
	Page string `json:"page,omitempty"`
	// Hidden layers stay in the document but don't render.
	Hidden bool `json:"hidden,omitempty"`
	// Locked layers can't be selected on the canvas.
	Locked bool `json:"locked,omitempty"`
	// Meta holds application data the editor carries along untouched.
	Meta map[string]any `json:"meta,omitempty"`
}

// The CSS custom properties the stylesheet maps to a layer's box and
// styles. Lengths carry their unit ("120px"), angles theirs ("15deg").
const (
	VarWidth       = "--at-width"
	VarHeight      = "--at-height"
	VarX           = "--at-x"
	VarY           = "--at-y"
	VarRotate      = "--at-rotate"
	VarScaleX      = "--at-scale-x" // -1 flips horizontally
	VarScaleY      = "--at-scale-y" // -1 flips vertically
	VarZIndex      = "--at-z"
	VarOpacity     = "--at-opacity"
	VarPadding     = "--at-padding"
	VarRadius      = "--at-radius"
	VarFill        = "--at-fill"
	VarBorderWidth = "--at-border-width"
	VarBorderColor = "--at-border-color"
	VarShadow      = "--at-shadow"
	VarFlexDir     = "--at-direction"
	VarAlignItems  = "--at-align"
	VarJustify     = "--at-justify"
	VarGap         = "--at-gap"
	VarBlur        = "--at-blur"
	VarBrightness  = "--at-brightness"
	VarContrast    = "--at-contrast"
	VarGrayscale   = "--at-grayscale"
	VarHueRotate   = "--at-hue-rotate"
	VarInvert      = "--at-invert"
	VarSaturate    = "--at-saturate"
	VarSepia       = "--at-sepia"
	VarFontFamily  = "--at-font-family"
	VarFontSize    = "--at-font-size"
	VarFontWeight  = "--at-font-weight"
	VarColor       = "--at-color"
	VarLineHeight  = "--at-line-height"
	VarLetterSpace = "--at-letter-spacing"
	VarTextAlign   = "--at-text-align"
	VarFontStyle   = "--at-font-style"
	VarDecoration  = "--at-decoration"
	VarTransform   = "--at-case"
	VarTextShadow  = "--at-text-shadow"
	VarStrokeWidth = "--at-stroke-width"
	VarStrokeColor = "--at-stroke-color"
	VarObjectFit   = "--at-fit"
	VarObjectPosX  = "--at-fit-x"
	VarObjectPosY  = "--at-fit-y"
)

// Var returns a custom property of the layer ("" when unset).
func (l Layer) Var(name string) string { return l.Vars[name] }

// Px returns a length property in pixels (0 when unset or not in px).
func (l Layer) Px(name string) float64 {
	v, ok := strings.CutSuffix(strings.TrimSpace(l.Vars[name]), "px")
	if !ok {
		return 0
	}
	f, _ := strconv.ParseFloat(v, 64)
	return f
}

// With returns a copy of the layer with the given properties set (an empty
// value removes the property).
func (l Layer) With(vars map[string]string) Layer {
	out := l
	out.Vars = make(map[string]string, len(l.Vars)+len(vars))
	for k, v := range l.Vars {
		out.Vars[k] = v
	}
	for k, v := range vars {
		if v == "" {
			delete(out.Vars, k)
		} else {
			out.Vars[k] = v
		}
	}
	return out
}

// Style is the layer's style attribute: its custom properties, sorted.
func (l Layer) Style() string {
	keys := make([]string, 0, len(l.Vars))
	for k := range l.Vars {
		if strings.HasPrefix(k, "--") && validVar(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(sanitizeValue(l.Vars[k]))
	}
	return b.String()
}

// validVar accepts custom property names made of letters, digits and dashes.
func validVar(k string) bool {
	for _, r := range k[2:] {
		if !(r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return len(k) > 2
}

// sanitizeValue keeps a property value inside its declaration: no ; { } or
// line breaks, which could end it and inject other declarations.
func sanitizeValue(v string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ';', '{', '}', '\n', '\r', '\\', '<':
			return -1
		}
		return r
	}, v)
}

// Document is what the editor edits: a frame size, its pages and its
// layers. Every page has the frame size.
type Document struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	// Pages are the document's pages, in order. A document without pages
	// has a single implicit page (id "").
	Pages  []Page  `json:"pages,omitempty"`
	Layers []Layer `json:"layers"`
}

// Page is one page of a document.
type Page struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PageIDs returns the document's page ids, in order ([""] without pages).
func (d Document) PageIDs() []string {
	if len(d.Pages) == 0 {
		return []string{""}
	}
	ids := make([]string, len(d.Pages))
	for i, p := range d.Pages {
		ids[i] = p.ID
	}
	return ids
}

// OnPage returns the root layers of a page, in paint order. Layers without
// a page are on the first page.
func (d Document) OnPage(page string) []Layer {
	first := d.PageIDs()[0]
	var out []Layer
	for _, l := range d.Layers {
		if l.Parent != "" {
			continue
		}
		p := l.Page
		if p == "" {
			p = first
		}
		if p == page || (page == "" && p == first) {
			out = append(out, l)
		}
	}
	return out
}

// ParseDocument decodes a document saved as JSON.
func ParseDocument(b []byte) (Document, error) {
	var d Document
	if err := json.Unmarshal(b, &d); err != nil {
		return d, fmt.Errorf("atelier: %w", err)
	}
	return d, nil
}

// Children returns the layers whose parent is id, in document order (the
// first is painted first).
func (d Document) Children(id string) []Layer {
	var out []Layer
	for _, l := range d.Layers {
		if l.Parent == id {
			out = append(out, l)
		}
	}
	return out
}

// Find returns the layer with the given id.
func (d Document) Find(id string) (Layer, bool) {
	for _, l := range d.Layers {
		if l.ID == id {
			return l, true
		}
	}
	return Layer{}, false
}
