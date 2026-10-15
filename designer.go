package atelier

import (
	"encoding/json"
	"io"
	"strconv"

	"github.com/alexisbcz/components/lib"
	h "github.com/alexisbcz/libhtml"
)

// Config configures a Designer.
type Config struct {
	// Document is the document to edit.
	Document Document
	// Types are the layer types (DefaultTypes when nil).
	Types Types
	// SaveURL, when set, receives the document as JSON (POST) after each
	// change, debounced. See Handler.
	SaveURL string
	// RenderURL renders layers of custom types (types other than frame,
	// text and image) in the browser: Handler serves it as {prefix}render.
	RenderURL string
	// UploadURL receives image files dropped on the canvas (multipart
	// field "file") and answers {"url": "..."}. Without it, dropped images
	// are stored inline as data URLs.
	UploadURL string
	// ReadOnly shows the document without editing.
	ReadOnly bool
	// Class adds classes to the root element ("h-svh").
	Class string
}

func (c Config) types() Types {
	if c.Types == nil {
		return DefaultTypes
	}
	return c.Types
}

type designerKey struct{}

// fromWriter returns the configuration of the enclosing Designer.
func fromWriter(w io.Writer) (Config, bool) {
	v, ok := h.Lookup(w, designerKey{})
	if !ok {
		return Config{}, false
	}
	return v.(Config), true
}

// Designer is the editor's root. Compose its parts as children:
//
//	atelier.Designer(cfg,
//		atelier.Header(...),
//		atelier.Content(
//			atelier.Panel(atelier.SideLeft, ...),
//			atelier.Canvas(atelier.Frame()),
//			atelier.Panel(atelier.SideRight, ...),
//		),
//		atelier.Toolbar(...),
//	)
//
// The document travels to the browser as JSON next to the markup.
func Designer(cfg Config, children ...any) h.Node {
	doc, _ := json.Marshal(cfg.Document) // json escapes <, > and &: safe in <script>
	var types []map[string]any
	for _, t := range cfg.types() {
		types = append(types, map[string]any{
			"type": t.Type, "name": t.Name, "key": t.Key, "container": t.Container, "defaults": t.Defaults,
		})
	}
	meta, _ := json.Marshal(map[string]any{"types": types, "saveURL": cfg.SaveURL, "renderURL": cfg.RenderURL, "uploadURL": cfg.UploadURL, "readOnly": cfg.ReadOnly})
	return h.WithValue(designerKey{}, cfg,
		h.Div(
			h.Data("slot", "atelier"),
			h.Data("read-only", strconv.FormatBool(cfg.ReadOnly)),
			h.Class(lib.Cn("relative flex flex-col overflow-hidden bg-background text-foreground text-sm", cfg.Class)),
			h.Script(h.Type("application/json"), h.Data("atelier-document", ""), h.UnsafeJS(doc)),
			h.Script(h.Type("application/json"), h.Data("atelier-config", ""), h.UnsafeJS(meta)),
			h.Group(children...),
			shortcutsDialog(cfg.types()),
		),
	)
}

// Header is the bar above the workspace.
func Header(children ...any) h.Node {
	return h.Div(h.Data("slot", "atelier-header"),
		h.Class("flex h-12 shrink-0 items-center gap-2 border-b px-4"), h.Group(children...))
}

// Content is the workspace: panels around the canvas.
func Content(children ...any) h.Node {
	return h.Div(h.Data("slot", "atelier-content"),
		h.Class("relative flex min-h-0 flex-1"), h.Group(children...))
}

// Side is where a Panel goes.
type Side string

// Sides.
const (
	SideLeft  Side = "left"
	SideRight Side = "right"
)

// Panel is a column of panes beside the canvas.
func Panel(side Side, children ...any) h.Node {
	border := "border-r"
	if side == SideRight {
		border = "border-l"
	}
	return h.Div(h.Data("slot", "atelier-panel"), h.Data("side", string(side)),
		h.Class("flex w-64 shrink-0 flex-col overflow-y-auto bg-background "+border), h.Group(children...))
}

// Pane groups controls in a panel. ForTypes, when given, shows the pane only
// while the selection has one of those layer types.
func Pane(title string, forTypes []string, children ...any) h.Node {
	el := h.Div(h.Data("slot", "atelier-pane"), h.Class("flex flex-col gap-3 border-b p-4 data-[hidden=true]:hidden"))
	if len(forTypes) > 0 {
		b, _ := json.Marshal(forTypes)
		el.Children(h.Data("for-types", string(b)), h.Data("hidden", "true"))
	}
	if title != "" {
		el.Children(h.Div(h.Data("slot", "atelier-pane-title"), h.Class("text-[13px] font-medium"), title))
	}
	return el.Children(h.Div(h.Data("slot", "atelier-pane-content"), h.Class("flex flex-col gap-2"), h.Group(children...)))
}

// Canvas is the pannable, zoomable surface holding the frame. Toolbars
// among its children float over it, outside the zoomed viewport:
//
//	atelier.Canvas(atelier.Frame(), atelier.Toolbar(...))
func Canvas(children ...any) h.Node {
	var inner, chrome []any
	for _, c := range children {
		if el, ok := c.(*h.Element); ok {
			if slot, _ := el.GetAttr("data-slot"); slot == "atelier-toolbar" {
				chrome = append(chrome, c)
				continue
			}
		}
		inner = append(inner, c)
	}
	return h.Div(h.Data("slot", "atelier-canvas"),
		h.Class("relative min-w-0 flex-1 touch-none overflow-hidden bg-muted outline-none select-none"),
		h.Attr("tabindex", "0"),
		h.Div(h.Data("slot", "atelier-viewport"), h.Class("absolute top-0 left-0 origin-top-left"), h.Group(inner...)),
		h.Div(h.Data("slot", "atelier-overlay"), h.Class("pointer-events-none absolute inset-0 overflow-hidden")),
		h.Group(chrome...),
	)
}

// Frame is the artboard: it renders the document's layers.
func Frame() h.Node {
	return h.NodeFunc(func(w io.Writer) error {
		cfg, _ := fromWriter(w)
		d := cfg.Document
		width, height := d.Width, d.Height
		if width == 0 {
			width = 1024
		}
		if height == 0 {
			height = 1024
		}
		return h.Div(h.Data("slot", "atelier-frame"),
			h.Class("relative overflow-hidden bg-white shadow-sm"),
			h.Style("width: "+px(width)+"; height: "+px(height)),
			cfg.types().RenderLayers(d),
		).Render(w)
	})
}

// StaticFrame renders a document read-only, outside any Designer: for
// previews and exports.
func StaticFrame(d Document, types Types) h.Node {
	if types == nil {
		types = DefaultTypes
	}
	return h.Div(h.Data("slot", "atelier-static-frame"),
		h.Class("relative overflow-hidden bg-white"),
		h.Style("width: "+px(d.Width)+"; height: "+px(d.Height)),
		types.RenderLayers(d))
}

func px(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "px" }
