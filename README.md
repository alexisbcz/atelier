# atelier

A design editor for Go web applications, built from parts. Compose a canvas, panels
and a toolbar, edit frames, text and images, and add your own layer types.
The editor renders on the server with libhtml, and a small JavaScript runtime
makes it interactive.

> Work in progress: expect the API to change.

## Install

```sh
go get github.com/alexisbcz/atelier
go mod tidy
```

## Use

```go
package main

import (
	"fmt"

	"github.com/alexisbcz/atelier"
	h "github.com/alexisbcz/libhtml"
)

func main() {
	doc := atelier.Document{Width: 1080, Height: 1080, Layers: []atelier.Layer{
		{ID: "title", Name: "Title", Type: "text", Value: "Hello", Vars: map[string]string{
			atelier.VarFontSize: "96px",
			atelier.VarX:        "80px",
		}},
	}}
	html, _ := h.RenderString(atelier.Types(atelier.DefaultTypes).RenderLayers(doc))
	fmt.Println(html)
}
```

## License

MIT. See [LICENSE](LICENSE).
