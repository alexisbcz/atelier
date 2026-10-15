package atelier

import (
	"strconv"

	"github.com/alexisbcz/icons/lucide"
	h "github.com/alexisbcz/libhtml"
)

// Actions edit the selected layers. Each control is bound to one custom
// property with data attributes the runtime reads:
//
//	data-atelier-var="--at-width"   the property
//	data-atelier-unit="px"       appended to the number typed ("" unitless)
//	data-atelier-set="center"    buttons: the value they set (toggles)
//
// Controls are plain markup: compose your own actions from the Field,
// Number, Color, Select, Slider and Toggle helpers.

const fieldClass = "flex h-7 min-w-0 flex-1 items-center gap-1.5 rounded-md bg-muted px-2 text-xs text-foreground focus-within:ring-2 focus-within:ring-ring/50"
const inputClass = "h-full w-full min-w-0 bg-transparent outline-none tabular-nums placeholder:text-muted-foreground"
const iconButtonClass = "inline-flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-foreground hover:bg-accent data-[active=true]:bg-foreground data-[active=true]:text-background [&_svg]:size-3.5"

// Action is a labeled row of controls.
func Action(label string, controls ...any) h.Node {
	return h.Div(h.Data("slot", "atelier-action"), h.Class("flex items-center gap-2"),
		h.Div(h.Data("slot", "atelier-action-label"), h.Class("w-16 shrink-0 truncate text-xs text-muted-foreground"), label),
		h.Div(h.Data("slot", "atelier-action-controls"), h.Class("flex min-w-0 flex-1 items-center gap-1"), h.Group(controls...)),
	)
}

// Number is a numeric field bound to a property. Prefix is a short label
// shown inside the field ("X"); unit is appended to the stored value.
func Number(prop, prefix, unit string, attrs ...any) h.Node {
	f := h.Label(h.Class(fieldClass))
	if prefix != "" {
		f.Children(h.Span(h.Class("shrink-0 text-muted-foreground"), prefix))
	}
	return f.Children(h.Input(h.Type("text"), h.Attr("inputmode", "decimal"), h.Class(inputClass),
		h.Data("atelier-var", prop), h.Data("atelier-unit", unit), h.Aria("label", prefix+" "+prop),
		h.Attr("autocomplete", "off"), h.Group(attrs...)))
}

// Text is a free-form field bound to a property ("0 4px 12px #0003").
func Text(prop, placeholder string) h.Node {
	return h.Label(h.Class(fieldClass),
		h.Input(h.Type("text"), h.Class(inputClass), h.Data("atelier-var", prop), h.Data("atelier-unit", ""),
			h.Attr("placeholder", placeholder), h.Aria("label", prop), h.Attr("autocomplete", "off")))
}

// Color is a color field: the value as text and a swatch opening the
// browser's color picker. Any CSS color or gradient can be typed.
func Color(prop, placeholder string) h.Node {
	return h.Label(h.Class(fieldClass),
		h.Input(h.Type("text"), h.Class(inputClass), h.Data("atelier-var", prop), h.Data("atelier-unit", ""),
			h.Attr("placeholder", placeholder), h.Aria("label", prop), h.Attr("autocomplete", "off")),
		h.Span(h.Class("relative size-4 shrink-0 overflow-hidden rounded-sm border border-foreground/10"), h.Data("atelier-swatch", prop),
			h.Input(h.Type("color"), h.Class("absolute inset-0 size-full cursor-pointer opacity-0"),
				h.Data("atelier-var", prop), h.Data("atelier-unit", ""), h.Aria("label", prop+" picker"))),
	)
}

// Clear is a button removing properties ("--at-fill").
func Clear(props ...string) h.Node {
	b := h.Button(h.Type("button"), h.Class(iconButtonClass), h.Aria("label", "Clear"),
		h.Data("atelier-command", "clear"), lucide.X())
	list := ""
	for i, p := range props {
		if i > 0 {
			list += " "
		}
		list += p
	}
	return b.Children(h.Data("atelier-props", list))
}

// Option is a choice of a Select or a Toggle.
type Option struct {
	Value string
	Label string
	Icon  func(children ...any) *h.Element
}

// Select is a drop-down bound to a property.
func Select(prop string, options []Option) h.Node {
	sel := h.Select(h.Class("h-full w-full min-w-0 cursor-pointer appearance-none bg-transparent outline-none"),
		h.Data("atelier-var", prop), h.Data("atelier-unit", ""), h.Aria("label", prop))
	for _, o := range options {
		sel.Children(h.Option(h.Value(o.Value), o.Label))
	}
	return h.Label(h.Class(fieldClass+" relative pr-6"), sel,
		lucide.ChevronDown(h.Class("pointer-events-none absolute right-2 size-3.5 text-muted-foreground")))
}

// Toggle is a row of buttons, each setting the property to its value (the
// active one is highlighted).
func Toggle(prop string, options []Option) h.Node {
	row := h.Div(h.Class("flex items-center gap-1"))
	for _, o := range options {
		b := h.Button(h.Type("button"), h.Class(iconButtonClass), h.Aria("label", o.Label), h.Title(o.Label),
			h.Data("atelier-var", prop), h.Data("atelier-set", o.Value))
		if o.Icon != nil {
			b.Children(o.Icon())
		} else {
			b.Children(h.Span(h.Class("text-[11px] font-medium"), o.Label))
		}
		row.Children(b)
	}
	return row
}

// Slider is a range bound to a property, with its number beside it.
func Slider(prop, unit string, min, max, step float64) h.Node {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return h.Div(h.Class("flex min-w-0 flex-1 items-center gap-2"),
		h.Div(h.Class("w-14 shrink-0"), Number(prop, "", unit)),
		h.Input(h.Type("range"), h.Class("h-1 min-w-0 flex-1 cursor-pointer accent-foreground"),
			h.Attr("min", f(min)), h.Attr("max", f(max)), h.Attr("step", f(step)),
			h.Data("atelier-var", prop), h.Data("atelier-unit", unit), h.Aria("label", prop)),
	)
}

// CommandButton is an icon button running an editor command.
func CommandButton(label, command string, icon h.Node, attrs ...any) h.Node {
	return h.Button(h.Type("button"), h.Class(iconButtonClass), h.Aria("label", label), h.Title(label),
		h.Data("atelier-command", command), icon, h.Group(attrs...))
}

// --- layout ------------------------------------------------------------------

// ActionPosition edits X and Y.
func ActionPosition() h.Node {
	return Action("Position", Number(VarX, "X", "px"), Number(VarY, "Y", "px"))
}

// ActionSize edits the width and height.
func ActionSize() h.Node {
	return Action("Size", Number(VarWidth, "W", "px"), Number(VarHeight, "H", "px"))
}

// ActionPadding edits the inner spacing.
func ActionPadding() h.Node { return Action("Padding", Number(VarPadding, "", "px")) }

// ActionRotate edits the rotation, with quarter turns.
func ActionRotate() h.Node {
	return Action("Rotate", Number(VarRotate, "", "deg"),
		CommandButton("Rotate left", "rotate", lucide.RotateCcw(), h.Data("atelier-degrees", "-90")),
		CommandButton("Rotate right", "rotate", lucide.RotateCw(), h.Data("atelier-degrees", "90")))
}

// ActionFlip flips horizontally and vertically.
func ActionFlip() h.Node {
	return Action("Flip",
		CommandButton("Flip horizontally", "flip", lucide.FlipHorizontal2(), h.Data("atelier-axis", "x")),
		CommandButton("Flip vertically", "flip", lucide.FlipVertical2(), h.Data("atelier-axis", "y")))
}

// ActionOpacity edits the opacity.
func ActionOpacity() h.Node { return Action("Opacity", Slider(VarOpacity, "", 0, 1, 0.01)) }

// --- styles ------------------------------------------------------------------

// ActionRadius edits the corner radius.
func ActionRadius() h.Node { return Action("Radius", Number(VarRadius, "", "px")) }

// ActionBorder edits the border width and color.
func ActionBorder() h.Node {
	return Action("Border", h.Div(h.Class("w-12 shrink-0"), Number(VarBorderWidth, "", "px")),
		Color(VarBorderColor, "Color"), Clear(VarBorderWidth, VarBorderColor))
}

// ActionShadow edits the box shadow (any CSS box-shadow).
func ActionShadow() h.Node {
	return Action("Shadow", Text(VarShadow, "0 8px 24px #0003"), Clear(VarShadow))
}

// ActionFill edits the background: a color or a gradient.
func ActionFill() h.Node {
	return Action("Fill", Color(VarFill, "Add..."), Clear(VarFill))
}

// --- text --------------------------------------------------------------------

// ActionTextValue edits a text layer's text.
func ActionTextValue() h.Node {
	return h.Textarea(h.Class("min-h-16 w-full resize-y rounded-md bg-muted px-2 py-1.5 text-xs outline-none focus:ring-2 focus:ring-ring/50"),
		h.Data("atelier-value", ""), h.Aria("label", "Text"), h.Attr("rows", "3"))
}

// Fonts are the families ActionFont offers (system stacks by default).
var Fonts = []Option{
	{Value: "", Label: "System"},
	{Value: "ui-serif, Georgia, serif", Label: "Serif"},
	{Value: "ui-monospace, SFMono-Regular, Menlo, monospace", Label: "Mono"},
	{Value: "'Helvetica Neue', Arial, sans-serif", Label: "Helvetica"},
	{Value: "Impact, 'Arial Narrow Bold', sans-serif", Label: "Impact"},
}

// ActionFont edits the font family.
func ActionFont() h.Node { return Action("Font", Select(VarFontFamily, Fonts)) }

// ActionFontWeight edits the font weight.
func ActionFontWeight() h.Node {
	return Action("Weight", Select(VarFontWeight, []Option{
		{"100", "Thin", nil}, {"300", "Light", nil}, {"400", "Regular", nil}, {"500", "Medium", nil},
		{"600", "Semibold", nil}, {"700", "Bold", nil}, {"800", "Extrabold", nil}, {"900", "Black", nil},
	}))
}

// ActionColor edits the text color.
func ActionColor() h.Node { return Action("Color", Color(VarColor, "#000000")) }

// ActionFontSize edits the font size.
func ActionFontSize() h.Node { return Action("Size", Number(VarFontSize, "", "px")) }

// ActionLineHeight edits the line height (unitless, relative to the size).
func ActionLineHeight() h.Node { return Action("Line", Number(VarLineHeight, "", "")) }

// ActionLetterSpacing edits the letter spacing.
func ActionLetterSpacing() h.Node { return Action("Spacing", Number(VarLetterSpace, "", "px")) }

// ActionTextAlign edits the horizontal alignment.
func ActionTextAlign() h.Node {
	return Action("Align", Toggle(VarTextAlign, []Option{
		{"left", "Left", lucide.AlignLeft}, {"center", "Center", lucide.AlignCenter},
		{"right", "Right", lucide.AlignRight}, {"justify", "Justify", lucide.AlignJustify},
	}))
}

// ActionVerticalAlign edits the vertical alignment of the text in its box.
func ActionVerticalAlign() h.Node {
	return Action("Vertical", Toggle(VarAlignItems, []Option{
		{"flex-start", "Top", lucide.AlignVerticalJustifyStart}, {"center", "Middle", lucide.AlignVerticalJustifyCenter},
		{"flex-end", "Bottom", lucide.AlignVerticalJustifyEnd},
	}))
}

// ActionFontStyle toggles italics.
func ActionFontStyle() h.Node {
	return Action("Style", Toggle(VarFontStyle, []Option{{"normal", "Normal", lucide.Type}, {"italic", "Italic", lucide.Italic}}))
}

// ActionTextDecoration edits underline and strike-through.
func ActionTextDecoration() h.Node {
	return Action("Decoration", Toggle(VarDecoration, []Option{
		{"none", "None", lucide.Minus}, {"underline", "Underline", lucide.Underline}, {"line-through", "Strike", lucide.Strikethrough},
	}))
}

// ActionTextTransform edits the case.
func ActionTextTransform() h.Node {
	return Action("Case", Toggle(VarTransform, []Option{
		{"none", "As typed", lucide.Minus}, {"uppercase", "Uppercase", lucide.CaseUpper},
		{"lowercase", "Lowercase", lucide.CaseLower}, {"capitalize", "Capitalize", lucide.CaseSensitive},
	}))
}

// ActionTextShadow edits the text shadow (any CSS text-shadow).
func ActionTextShadow() h.Node {
	return Action("Shadow", Text(VarTextShadow, "0 2px 8px #0006"), Clear(VarTextShadow))
}

// ActionTextStroke edits the text outline.
func ActionTextStroke() h.Node {
	return Action("Stroke", h.Div(h.Class("w-12 shrink-0"), Number(VarStrokeWidth, "", "px")),
		Color(VarStrokeColor, "Color"), Clear(VarStrokeWidth, VarStrokeColor))
}

// --- image -------------------------------------------------------------------

// ActionImageURL edits an image layer's source.
func ActionImageURL() h.Node {
	return Action("Source", h.Label(h.Class(fieldClass),
		h.Input(h.Type("url"), h.Class(inputClass), h.Data("atelier-value", ""), h.Attr("placeholder", "https://..."),
			h.Aria("label", "Image URL"), h.Attr("autocomplete", "off"))))
}

// ActionImageFit edits how the image fills its box.
func ActionImageFit() h.Node {
	return Action("Fit", Select(VarObjectFit, []Option{
		{"cover", "Cover", nil}, {"contain", "Contain", nil}, {"fill", "Stretch", nil}, {"none", "Original", nil},
	}))
}

// ActionFilters edits the image filters.
func ActionFilters() h.Node {
	return h.Group(
		Action("Blur", Slider(VarBlur, "px", 0, 40, 1)),
		Action("Brightness", Slider(VarBrightness, "%", 0, 200, 1)),
		Action("Contrast", Slider(VarContrast, "%", 0, 200, 1)),
		Action("Grayscale", Slider(VarGrayscale, "%", 0, 100, 1)),
		Action("Hue", Slider(VarHueRotate, "deg", 0, 360, 1)),
		Action("Invert", Slider(VarInvert, "%", 0, 100, 1)),
		Action("Saturate", Slider(VarSaturate, "%", 0, 200, 1)),
		Action("Sepia", Slider(VarSepia, "%", 0, 100, 1)),
	)
}

// --- document ----------------------------------------------------------------

// FrameSizes are the presets ActionDocumentSize offers.
var FrameSizes = []struct {
	Label         string
	Width, Height int
}{
	{"Square post", 1080, 1080}, {"Portrait post", 1080, 1350}, {"Story", 1080, 1920},
	{"Landscape", 1920, 1080}, {"Slide", 1280, 720}, {"A4 (96 dpi)", 794, 1123}, {"Letter (96 dpi)", 816, 1056},
}

// ActionDocumentSize edits the frame size, with presets.
func ActionDocumentSize() h.Node {
	num := func(field, prefix string) h.Node {
		return h.Label(h.Class(fieldClass), h.Span(h.Class("shrink-0 text-muted-foreground"), prefix),
			h.Input(h.Type("text"), h.Attr("inputmode", "numeric"), h.Class(inputClass), h.Data("atelier-doc", field),
				h.Aria("label", "Frame "+field), h.Attr("autocomplete", "off")))
	}
	sel := h.Select(h.Class("h-full w-full min-w-0 cursor-pointer appearance-none bg-transparent outline-none"),
		h.Data("atelier-doc-preset", ""), h.Aria("label", "Frame size preset"),
		h.Option(h.Value(""), "Custom"))
	for _, p := range FrameSizes {
		v := strconv.Itoa(p.Width) + "x" + strconv.Itoa(p.Height)
		sel.Children(h.Option(h.Value(v), p.Label+" · "+strconv.Itoa(p.Width)+"×"+strconv.Itoa(p.Height)))
	}
	return h.Group(
		Action("Preset", h.Label(h.Class(fieldClass+" relative pr-6"), sel,
			lucide.ChevronDown(h.Class("pointer-events-none absolute right-2 size-3.5 text-muted-foreground")))),
		Action("Size", num("width", "W"), num("height", "H")),
	)
}
