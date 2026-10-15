// Command gen builds atelier's stylesheet (static/atelier.css) with Tailwind
// CSS v4 in Go: the utility classes used by atelier and by the components
// it renders, the shadcn/ui neutral theme, and the layer styles
// (static/layers.css).
//
//	go generate github.com/alexisbcz/atelier
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alexisbcz/tailwind"
)

func main() {
	root := flag.String("root", "..", "the atelier module directory")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		log.Fatal(err)
	}
	// The components module, wherever the module cache holds it.
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/alexisbcz/components")
	cmd.Dir = abs
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("locating github.com/alexisbcz/components: %v", err)
	}
	components := strings.TrimSpace(string(out))
	theme, err := os.ReadFile(filepath.Join(abs, "gen", "theme.css"))
	if err != nil {
		log.Fatal(err)
	}
	input := fmt.Sprintf(`@import "tailwindcss" source(none);
@source %q;
@source %q;
@import %q;
%s`,
		filepath.Join(abs, "*.go"),
		filepath.Join(components, "**", "*.go"),
		filepath.Join(abs, "static", "layers.css"),
		theme)
	res, err := tailwind.Build(tailwind.Options{CSS: input, Dir: abs, Minify: true})
	if err != nil {
		log.Fatal(err)
	}
	dst := filepath.Join(abs, "static", "atelier.css")
	if err := os.WriteFile(dst, []byte(res.CSS), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s: %d bytes\n", dst, len(res.CSS))
}
