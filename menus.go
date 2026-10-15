package atelier

import (
	"io"

	"github.com/alexisbcz/components/lib"
	"github.com/alexisbcz/components/ui/button"
	dm "github.com/alexisbcz/components/ui/dropdown-menu"
	"github.com/alexisbcz/icons/lucide"
	h "github.com/alexisbcz/libhtml"
)

// Commands are data-atelier-command values the runtime understands; any
// element (button, menu item) carrying one runs it when clicked.
const (
	CommandUndo       = "undo"
	CommandRedo       = "redo"
	CommandAdd        = "add" // with data-atelier-type
	CommandDelete     = "delete"
	CommandDuplicate  = "duplicate"
	CommandZoomIn     = "zoom-in"
	CommandZoomOut    = "zoom-out"
	CommandZoomFit    = "zoom-fit"
	CommandZoom       = "zoom" // with data-atelier-zoom (percent)
	CommandExportPNG  = "export-png"
	CommandExportPDF  = "export-pdf"
	CommandExportJSON = "export-json"
	CommandShortcuts  = "shortcuts"
)

// Command is the props of an element running a command.
func Command(name string, attrs ...any) h.Prop {
	return func(e *h.Element) {
		e.Data("atelier-command", name)
		for _, a := range attrs {
			if p, ok := a.(h.Prop); ok {
				p(e)
			}
		}
	}
}

func mod() string { return "⌘" }

// MainMenu is the header's menu: insert layers, zoom, keyboard shortcuts.
func MainMenu() h.Node {
	return h.NodeFunc(func(w io.Writer) error {
		cfg, _ := fromWriter(w)
		var insert []any
		for _, t := range cfg.types() {
			item := dm.Item(Command(CommandAdd), h.Data("atelier-type", t.Type), t.Name)
			if t.Key != "" {
				item.Children(dm.Shortcut(stringsToUpper(t.Key)))
			}
			insert = append(insert, item)
		}
		return dm.DropdownMenu(
			dm.Trigger(lib.AsChild, button.Button(button.VariantSecondary, button.SizeSm, h.Class("gap-1"), "Menu", lucide.ChevronDown())),
			dm.Content(dm.AlignStart, h.Class("w-48"),
				dm.Sub(dm.SubTrigger("Insert"), dm.SubContent(h.Group(insert...))),
				dm.Sub(dm.SubTrigger("View"), dm.SubContent(
					dm.Item(Command(CommandZoomIn), "Zoom in", dm.Shortcut(mod()+"+")),
					dm.Item(Command(CommandZoomOut), "Zoom out", dm.Shortcut(mod()+"−")),
					dm.Item(Command(CommandZoomFit), "Zoom to fit", dm.Shortcut(mod()+"0")),
				)),
				dm.Separator(),
				dm.Item(Command(CommandUndo), "Undo", dm.Shortcut(mod()+"Z")),
				dm.Item(Command(CommandRedo), "Redo", dm.Shortcut("⇧"+mod()+"Z")),
				dm.Separator(),
				dm.Item(Command(CommandShortcuts), "Keyboard shortcuts"),
			),
		).Render(w)
	})
}

// ExportMenu exports the document as an image, a PDF or JSON.
func ExportMenu() h.Node {
	return dm.DropdownMenu(
		dm.Trigger(lib.AsChild, button.Button(button.SizeSm, h.Class("gap-1"), "Export", lucide.ChevronDown())),
		dm.Content(dm.AlignEnd, h.Class("w-40"),
			dm.Item(Command(CommandExportPNG), "Image"),
			dm.Item(Command(CommandExportPDF), "PDF"),
			dm.Item(Command(CommandExportJSON), "JSON"),
		),
	)
}

// ToolbarZoom shows the zoom level and changes it.
func ToolbarZoom() h.Node {
	return dm.DropdownMenu(
		dm.Trigger(lib.AsChild, h.Button(h.Type("button"),
			h.Class("inline-flex h-7 items-center gap-1 rounded-md bg-muted px-2 text-xs font-medium tabular-nums hover:bg-accent [&_svg]:size-3"),
			h.Span(h.Data("atelier-zoom-label", ""), "100%"), lucide.ChevronUp())),
		dm.Content(dm.SideTop, dm.AlignCenter, h.Class("w-40"),
			dm.Item(Command(CommandZoomIn), "Zoom in", dm.Shortcut(mod()+"+")),
			dm.Item(Command(CommandZoomOut), "Zoom out", dm.Shortcut(mod()+"−")),
			dm.Item(Command(CommandZoomFit), "Zoom to fit", dm.Shortcut(mod()+"0")),
			dm.Separator(),
			dm.Item(Command(CommandZoom), h.Data("atelier-zoom", "50"), "50%"),
			dm.Item(Command(CommandZoom), h.Data("atelier-zoom", "100"), "100%"),
			dm.Item(Command(CommandZoom), h.Data("atelier-zoom", "200"), "200%"),
		),
	)
}

// Shortcuts is the keyboard shortcuts dialog (opened by CommandShortcuts).
// Designer renders it.
func shortcutsDialog(types Types) h.Node {
	row := func(label, keys string) h.Node {
		return h.Div(h.Class("flex items-center justify-between py-1.5 text-sm"),
			h.Span(label), h.Kbd(h.Class("rounded bg-muted px-1.5 py-0.5 font-mono text-xs"), keys))
	}
	rows := []any{
		row("Undo", mod()+" Z"), row("Redo", "⇧ "+mod()+" Z"),
		row("Duplicate", mod()+" D"), row("Select all", mod()+" A"), row("Delete", "⌫"),
		row("Nudge", "← ↑ → ↓"), row("Nudge by 10", "⇧ ← ↑ → ↓"),
		row("Zoom in", mod()+" +"), row("Zoom out", mod()+" −"), row("Zoom to fit", mod()+" 0"),
		row("Pan", "Space + drag"), row("Deselect", "Esc"),
	}
	for _, t := range types {
		if t.Key != "" {
			rows = append(rows, row("Add "+t.Name, stringsToUpper(t.Key)))
		}
	}
	return h.El("dialog", h.Data("atelier-shortcuts", ""),
		h.Class("m-auto w-96 max-w-[calc(100%-2rem)] rounded-xl border bg-background p-6 text-foreground shadow-lg backdrop:bg-black/50"),
		h.Div(h.Class("mb-3 flex items-center justify-between"),
			h.H2(h.Class("text-base font-semibold"), "Keyboard shortcuts"),
			h.Form(h.Attr("method", "dialog"),
				h.Button(h.Class("inline-flex size-7 items-center justify-center rounded-md hover:bg-accent [&_svg]:size-4"), h.Aria("label", "Close"), lucide.X()))),
		h.Div(h.Class("divide-y"), h.Group(rows...)),
	)
}

func stringsToUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// PaneLayers lists the layers: click to select, double-click to rename,
// toggle visibility and lock.
func PaneLayers() h.Node {
	return h.NodeFunc(func(w io.Writer) error {
		cfg, _ := fromWriter(w)
		var icons []any
		for _, t := range cfg.types() {
			if t.Icon != nil {
				icons = append(icons, h.El("template", h.Data("atelier-icon", t.Type), t.Icon()))
			}
		}
		icons = append(icons,
			h.El("template", h.Data("atelier-icon", "@eye"), lucide.Eye()),
			h.El("template", h.Data("atelier-icon", "@eye-off"), lucide.EyeOff()),
			h.El("template", h.Data("atelier-icon", "@lock"), lucide.Lock()),
			h.El("template", h.Data("atelier-icon", "@unlock"), lucide.LockOpen()),
		)
		return h.Div(h.Data("slot", "atelier-layer-tree"), h.Class("flex flex-col gap-0.5 p-2"),
			h.Group(icons...),
			h.Div(h.Data("atelier-layer-list", ""), h.Class("flex flex-col gap-0.5")),
		).Render(w)
	})
}
