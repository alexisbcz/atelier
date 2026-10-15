# atelier

A design editor for Go web applications, built from parts. Compose a canvas,
panels and a toolbar, edit frames, text and images, and add your own layer
types. The editor renders on the server with
[libhtml](https://github.com/alexisbcz/libhtml) and
[components](https://github.com/alexisbcz/components), and a small JavaScript
runtime makes it interactive. It needs no Node.js and no build step.

![The atelier demo editor](docs/screenshot.png)

> Work in progress: expect the API to change.

## Features

- Select, move, resize and rotate layers, with snapping guides and marquee selection.
- Edit text in place, and drop or paste images onto the canvas.
- An inspector for position, size, radius, borders, shadows, fills, filters and typography.
- A layer tree, undo and redo, copy and paste, layer order, alignment and keyboard shortcuts.
- Export to PNG, PDF and JSON.
- Custom layer types written in Go: the server renders them, in the editor too.
- Saving through an HTTP handler, with memory and file stores and document validation.
- One embedded stylesheet for the editor and its components: no Tailwind setup needed.

## Install

```sh
go get github.com/alexisbcz/atelier
go mod tidy
```

## Quick start

```go
package main

import (
	"net/http"

	"github.com/alexisbcz/atelier"
	"github.com/alexisbcz/components/ui/runtime"
	h "github.com/alexisbcz/libhtml"
)

var doc = atelier.Document{Width: 1080, Height: 1080, Layers: []atelier.Layer{
	{ID: "title", Name: "Title", Type: "text", Value: "Hello", Vars: map[string]string{
		atelier.VarWidth: "800px", atelier.VarHeight: "200px",
		atelier.VarX: "140px", atelier.VarY: "440px", atelier.VarFontSize: "120px",
	}},
}}

func main() {
	http.Handle(runtime.Prefix, runtime.Handler())
	http.Handle(atelier.AssetsPrefix, atelier.Assets())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		editor := atelier.Designer(atelier.Config{Document: doc, Class: "h-svh"},
			atelier.Header(atelier.MainMenu(), h.Div(h.Class("flex-1")), atelier.ExportMenu()),
			atelier.Content(
				atelier.Panel(atelier.SideLeft, atelier.PaneLayers()),
				atelier.Canvas(atelier.Frame(), atelier.CanvasMenu()),
				atelier.Panel(atelier.SideRight,
					atelier.Pane("Layer", nil, atelier.ActionPosition(), atelier.ActionSize()),
					atelier.Pane("Text", []string{"text"}, atelier.ActionFontSize(), atelier.ActionColor()),
				),
			),
		)
		page := h.Html(h.Head(atelier.Stylesheet(), runtime.Scripts(r)), h.Body(editor))
		h.WriteHTTP(w, http.StatusOK, page)
	})
	http.ListenAndServe(":8080", nil)
}
```

Run the full demo with `go run github.com/alexisbcz/atelier/cmd/demo`.

## Documents and layers

A `Document` holds a frame size and a list of layers. Each `Layer` stores an
id, a name, a type, a value (the text of a text layer, the address of an
image) and CSS custom properties for its box and styles:

```go
atelier.Layer{ID: "logo", Type: "image", Value: "/logo.png", Vars: map[string]string{
	atelier.VarWidth: "320px", atelier.VarHeight: "120px", atelier.VarRadius: "12px",
}}
```

The stylesheet maps these properties to the rendering, so the server and
the browser draw a layer the same way. `StaticFrame` renders a document
read-only, for previews.

## Custom layer types

A layer type renders in Go. The editor fetches the rendering from the
handler, so custom types need no JavaScript:

```go
var badge = atelier.LayerType{
	Type: "badge", Name: "Badge", Key: "b",
	Defaults: atelier.Layer{Value: "New", Vars: map[string]string{
		atelier.VarWidth: "240px", atelier.VarHeight: "88px", atelier.VarFill: "#18181b",
	}},
	Render: func(l atelier.Layer) h.Node { return h.Span("✦ ", l.Value) },
}
```

Pass the types to `Config.Types` and to `Handler`, and set
`Config.RenderURL` to the handler's `render` path.

## Saving

`Handler` serves documents from a `Store` and validates what it saves:

```go
store := atelier.FileStore{Dir: "designs"}
types := atelier.Types{atelier.FrameType, atelier.TextType, atelier.ImageType, badge}
http.Handle("/designs/", http.StripPrefix("/designs/", atelier.Handler(store, types, atelier.Limits{})))
```

Set `Config.SaveURL` to `/designs/<id>`: the editor posts the document after
each change. Protect the handler like any write endpoint, with a session
check and CSRF protection.

## Actions and commands

Actions, the inspector controls, each edit one layer property. The package ships
actions for layout, styles, transforms, text and images, and the `Number`,
`Color`, `Select`, `Slider` and `Toggle` helpers build your own. Any element
with `atelier.Command(...)` runs an editor command when clicked, such as
undo, add a layer or export.

## Keyboard shortcuts

| Keys | Action |
| --- | --- |
| ⌘ Z, ⇧ ⌘ Z | Undo, redo |
| ⌘ C, ⌘ X, ⌘ V, ⌘ D | Copy, cut, paste, duplicate |
| ⌫ | Delete |
| Arrows, ⇧ arrows | Nudge by 1 or 10 pixels |
| ⌘ ], ⌘ [ | Bring forward, send backward |
| ⌘ +, ⌘ −, ⌘ 0 | Zoom in, zoom out, zoom to fit |
| F, T, I | Add a frame, a text or an image |
| Space + drag | Pan |

On Windows and Linux, use the Control key in place of ⌘.

## License

MIT. See [LICENSE](LICENSE).
