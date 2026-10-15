package atelier

import (
	"github.com/alexisbcz/components/ui/separator"
	"github.com/alexisbcz/components/ui/tooltip"
	h "github.com/alexisbcz/libhtml"
)

// Toolbar floats at the bottom of the canvas.
func Toolbar(children ...any) h.Node {
	return h.Div(h.Data("slot", "atelier-toolbar"),
		h.Class("pointer-events-none absolute inset-x-0 bottom-4 z-10 flex justify-center"),
		h.Div(h.Class("pointer-events-auto flex h-10 items-center gap-1 rounded-xl border bg-background px-1.5 shadow-md"),
			h.Group(children...)))
}

// ToolbarGroup groups toolbar buttons.
func ToolbarGroup(children ...any) h.Node {
	return h.Div(h.Data("slot", "atelier-toolbar-group"), h.Class("flex items-center gap-0.5"), h.Group(children...))
}

// ToolbarSeparator separates toolbar groups.
func ToolbarSeparator() h.Node {
	return separator.Separator(separator.Vertical, h.Class("mx-1 h-5!"))
}

// ToolbarButton is an icon button with a tooltip. Props (h.Data("atelier-command",
// "undo"), ...) wire it to the editor.
func ToolbarButton(label string, icon h.Node, props ...any) h.Node {
	btn := h.Button(h.Type("button"), h.Data("slot", "atelier-toolbar-button"), h.Aria("label", label),
		h.Class("inline-flex size-7 items-center justify-center rounded-md text-foreground hover:bg-accent disabled:pointer-events-none disabled:opacity-40 data-[active=true]:bg-accent [&_svg]:size-4"),
		icon, h.Group(props...))
	return tooltip.Tooltip(
		tooltip.Trigger(btn).AsChild(),
		tooltip.Content(label),
	)
}
