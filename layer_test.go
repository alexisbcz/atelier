package atelier

import (
	"strings"
	"testing"

	h "github.com/alexisbcz/libhtml"
)

func render(t *testing.T, n h.Node) string {
	t.Helper()
	s, err := h.RenderString(n)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStyleSortedAndSanitized(t *testing.T) {
	l := Layer{Vars: map[string]string{
		VarWidth:    "320px",
		VarX:        "10px",
		"--evil":    "red; background: url(x)",
		"--bad}":    "1",
		"not-a-var": "1",
	}}
	got := l.Style()
	want := "--at-width: 320px; --at-x: 10px; --evil: red background: url(x)"
	if got != want {
		t.Errorf("Style() = %q, want %q", got, want)
	}
}

func TestWithAndPx(t *testing.T) {
	l := Layer{Vars: map[string]string{VarWidth: "100px"}}
	m := l.With(map[string]string{VarWidth: "", VarHeight: "50.5px"})
	if _, ok := m.Vars[VarWidth]; ok {
		t.Error("empty value should remove the property")
	}
	if m.Px(VarHeight) != 50.5 {
		t.Errorf("Px = %v", m.Px(VarHeight))
	}
	if l.Vars[VarWidth] != "100px" {
		t.Error("With must not modify the original")
	}
}

func TestRenderDocument(t *testing.T) {
	d := Document{Width: 1080, Height: 1080, Layers: []Layer{
		{ID: "f", Type: "frame", Name: "Card", Vars: map[string]string{VarWidth: "500px"}},
		{ID: "t", Type: "text", Name: "Title", Value: "<b>Hi</b>", Parent: "f"},
		{ID: "i", Type: "image", Name: "Photo", Value: "https://example.com/a.jpg"},
		{ID: "x", Type: "text", Hidden: true, Value: "secret"},
	}}
	s := render(t, Types(DefaultTypes).RenderLayers(d))
	for _, want := range []string{
		`<div class="atelier-layer atelier-layer-frame" data-slot="atelier-layer" data-layer-id="f" data-layer-type="frame" style="--at-width: 500px">`,
		`<div data-slot="atelier-text">&lt;b&gt;Hi&lt;/b&gt;</div>`,
		`src="https://example.com/a.jpg"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	if strings.Contains(s, "secret") {
		t.Error("hidden layer rendered")
	}
	// The text layer is inside the frame.
	if strings.Index(s, `data-layer-id="t"`) < strings.Index(s, `data-layer-id="f"`) {
		t.Error("child rendered before its frame")
	}
}

func TestNewFromDefaults(t *testing.T) {
	l, ok := Types(DefaultTypes).New("text", "abc")
	if !ok || l.ID != "abc" || l.Type != "text" || l.Value != "Text" || l.Vars[VarFontSize] != "64px" {
		t.Errorf("New = %+v", l)
	}
	l.Vars[VarFontSize] = "1px"
	if TextType.Defaults.Vars[VarFontSize] != "64px" {
		t.Error("New must copy the defaults")
	}
}

func TestParseDocument(t *testing.T) {
	d, err := ParseDocument([]byte(`{"width":800,"height":600,"layers":[{"id":"a","name":"A","type":"text","value":"hi","vars":{"--at-width":"10px"}}]}`))
	if err != nil || len(d.Layers) != 1 || d.Layers[0].Px(VarWidth) != 10 {
		t.Fatalf("ParseDocument = %+v, %v", d, err)
	}
}
