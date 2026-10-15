package atelier

import (
	"github.com/alexisbcz/icons/lucide"
	h "github.com/alexisbcz/libhtml"
)

// PaneGenerate generates images from a prompt (Config.GenerateURL) and
// inserts them as image layers. Earlier results stay listed: click one to
// insert it again.
func PaneGenerate() h.Node {
	size := func(v, label string) h.Node { return h.Option(h.Value(v), label) }
	return h.Div(h.Data("slot", "atelier-generate"), h.Class("flex flex-col gap-3 p-3"),
		h.Textarea(h.Class("min-h-24 w-full resize-y rounded-md bg-muted px-2 py-1.5 text-xs outline-none focus:ring-2 focus:ring-ring/50"),
			h.Data("atelier-gen-prompt", ""), h.Aria("label", "Prompt"), h.Attr("rows", "4"),
			h.Attr("placeholder", "A lighthouse at dusk, soft film photo")),
		h.Div(h.Class("flex items-center gap-1"),
			h.Label(h.Class(fieldClass+" relative pr-6"),
				h.Select(h.Class("h-full w-full min-w-0 cursor-pointer appearance-none bg-transparent outline-none"),
					h.Data("atelier-gen-size", ""), h.Aria("label", "Size"),
					size("512x512", "Square"), size("512x640", "Portrait"), size("512x896", "Story"), size("768x512", "Landscape")),
				lucide.ChevronDown(h.Class("pointer-events-none absolute right-2 size-3.5 text-muted-foreground"))),
			h.Button(h.Type("button"), Command("generate"),
				h.Class("inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-60 [&_svg]:size-3.5 data-[busy=true]:[&_[data-icon=spin]]:inline data-[busy=true]:[&_[data-icon=idle]]:hidden"),
				lucide.Sparkles(h.Data("icon", "idle")),
				lucide.LoaderCircle(h.Data("icon", "spin"), h.Class("hidden animate-spin")),
				"Generate"),
		),
		h.P(h.Data("atelier-gen-status", ""), h.Class("min-h-4 text-xs text-muted-foreground"), h.Aria("live", "polite")),
		h.Div(h.Data("atelier-gen-results", ""), h.Class("grid grid-cols-2 gap-2")),
	)
}

// ActionImageEdit edits the selected image with a prompt (Config.EditURL):
// strength sets how far the result may move away from the image.
func ActionImageEdit() h.Node {
	return h.Div(h.Data("slot", "atelier-image-edit"), h.Class("flex flex-col gap-2"),
		h.Textarea(h.Class("min-h-14 w-full resize-y rounded-md bg-muted px-2 py-1.5 text-xs outline-none focus:ring-2 focus:ring-ring/50"),
			h.Data("atelier-edit-prompt", ""), h.Aria("label", "Edit prompt"), h.Attr("rows", "2"),
			h.Attr("placeholder", "Make it a watercolor painting")),
		h.Div(h.Class("flex items-center gap-2"),
			h.Span(h.Class("w-16 shrink-0 text-xs text-muted-foreground"), "Strength"),
			h.Input(h.Type("range"), h.Class("h-1 min-w-0 flex-1 cursor-pointer accent-foreground"),
				h.Attr("min", "0.1"), h.Attr("max", "0.9"), h.Attr("step", "0.05"), h.Value("0.45"),
				h.Data("atelier-edit-strength", ""), h.Aria("label", "Strength")),
			h.Button(h.Type("button"), Command("edit-image"),
				h.Class("inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-60 [&_svg]:size-3.5"),
				lucide.WandSparkles(), "Apply"),
		),
		h.P(h.Data("atelier-edit-status", ""), h.Class("min-h-4 text-xs text-muted-foreground"), h.Aria("live", "polite")),
	)
}
