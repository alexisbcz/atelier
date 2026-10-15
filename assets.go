package atelier

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/alexisbcz/components/ui/runtime"
	h "github.com/alexisbcz/libhtml"
)

//go:generate go -C gen run . -root ..

// AssetsPrefix is the URL path Assets is served under.
const AssetsPrefix = "/_atelier/"

//go:embed static/atelier.css
var static embed.FS

//go:embed static/atelier.js
var script string

// The editor runtime is a module of the components runtime: it is served
// and loaded with it (runtime.Scripts).
func init() { runtime.Module("atelier", script) }

// Assets serves the editor's stylesheet under AssetsPrefix:
//
//	http.Handle(atelier.AssetsPrefix, atelier.Assets())
//
// The stylesheet holds every class the editor and its components use, so
// pages need no Tailwind build of their own.
func Assets() http.Handler {
	sub, _ := fs.Sub(static, "static")
	return http.StripPrefix(AssetsPrefix, http.FileServer(http.FS(sub)))
}

// Stylesheet is the <link> to the editor's stylesheet. Put it in <head>.
func Stylesheet() h.Node {
	return h.Link(h.Rel("stylesheet"), h.Href(AssetsPrefix+"atelier.css"))
}
