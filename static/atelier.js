// atelier.js: the editor runtime. The server renders the shell and the
// layers (atelier.Designer); this module makes them editable.
//
// - The document (layers + frame size) is a plain object; every edit goes
//   through commit(), which records history, re-syncs the DOM and the
//   overlay, and emits "atelier:change" on the root (debounced POST to
//   saveURL when configured).
// - Layers are DOM boxes whose look comes only from CSS custom properties
//   (static/layers.css), exactly like the server rendering.
// - The canvas has a viewport transformed by pan + zoom; the overlay draws
//   the selection in canvas coordinates (rotation-aware), with resize and
//   rotate handles.
import { define } from "./core.js"

const MIN_ZOOM = 0.05
const MAX_ZOOM = 8
const HISTORY = 100

// The effective value of unset properties, as shown by the actions (the
// stylesheet's fallbacks).
const DEFAULTS = {
  "--at-opacity": "1",
  "--at-blur": "0",
  "--at-brightness": "100",
  "--at-contrast": "100",
  "--at-grayscale": "0",
  "--at-hue-rotate": "0",
  "--at-invert": "0",
  "--at-saturate": "100",
  "--at-sepia": "0",
  "--at-padding": "0",
  "--at-radius": "0",
  "--at-letter-spacing": "0",
  "--at-line-height": "1.2",
  "--at-font-weight": "400",
  "--at-text-align": "left",
  "--at-fit": "cover",
}

const num = (v, d = 0) => {
  const n = parseFloat(v)
  return Number.isFinite(n) ? n : d
}
const round = (n) => Math.round(n * 100) / 100
const px = (n) => round(n) + "px"
const clone = (v) => JSON.parse(JSON.stringify(v))
const uid = () => Math.random().toString(36).slice(2, 10) + Date.now().toString(36).slice(-3)
const isMac = /Mac|iPhone|iPad/.test(navigator.platform)

function rotate([x, y], deg) {
  const r = (deg * Math.PI) / 180
  const c = Math.cos(r)
  const s = Math.sin(r)
  return [x * c - y * s, x * s + y * c]
}

// The key of an event, from its physical key where it has one (so
// shift+] is "]" on every layout), lowercased.
function eventKey(e) {
  const c = e.code || ""
  if (/^Key[A-Z]$/.test(c)) return c.slice(3).toLowerCase()
  if (/^Digit\d$/.test(c)) return c.slice(5)
  const named = { BracketLeft: "[", BracketRight: "]", Equal: "=", Minus: "-", Backquote: "`", Slash: "/", Period: ".", Comma: "," }
  return named[c] ?? e.key.toLowerCase()
}

// matchKey reports whether a keydown matches a combo: "mod" is ⌘ on Apple
// platforms and Ctrl elsewhere; modifiers must match exactly.
function matchKey(e, combo) {
  if (!combo) return false
  const parts = combo.toLowerCase().split("+")
  let key = parts.pop()
  if (key === "" && combo.endsWith("+")) key = "+"
  const want = new Set(parts)
  const mod = want.has("mod")
  const meta = want.has("meta") || (mod && isMac)
  const ctrl = want.has("ctrl") || (mod && !isMac)
  if (e.metaKey !== meta || e.ctrlKey !== ctrl || e.shiftKey !== want.has("shift") || e.altKey !== want.has("alt")) return false
  return eventKey(e) === key || e.key.toLowerCase() === key
}

// Types the runtime draws itself; others are rendered by the server
// (config.renderURL).
const BUILTIN = new Set(["frame", "text", "image"])

class Editor {
  constructor(root) {
    this.root = root
    this.doc = JSON.parse(root.querySelector("script[data-atelier-document]")?.textContent || "{}")
    this.doc.layers ??= []
    this.doc.width ||= 1024
    this.doc.height ||= 1024
    this.config = JSON.parse(root.querySelector("script[data-atelier-config]")?.textContent || "{}")
    this.types = new Map((this.config.types ?? []).map((t) => [t.type, t]))
    this.readOnly = !!this.config.readOnly
    this.canvas = root.querySelector('[data-slot="atelier-canvas"]')
    this.viewport = root.querySelector('[data-slot="atelier-viewport"]')
    this.overlay = root.querySelector('[data-slot="atelier-overlay"]')
    this.frame = root.querySelector('[data-slot="atelier-frame"]')
    this.selection = []
    this.page = this.pageIds()[0]
    this.past = []
    this.future = []
    this.zoom = 1
    this.pan = [0, 0]
    this.cleanups = []
    this.listeners = new Set()
  }

  // --- document ---------------------------------------------------------

  layer(id) {
    return this.doc.layers.find((l) => l.id === id)
  }

  selected() {
    return this.selection.map((id) => this.layer(id)).filter(Boolean)
  }

  // --- units --------------------------------------------------------------

  // Pixels per unit of the document's unit (1 for px).
  perUnit() {
    const inch = { in: 1, mm: 1 / 25.4, cm: 1 / 2.54, pt: 1 / 72 }[this.doc.unit]
    return inch ? inch * (this.doc.dpi || 96) : 1
  }

  unitName() {
    return ["in", "mm", "cm", "pt"].includes(this.doc.unit) ? this.doc.unit : "px"
  }

  // --- pages --------------------------------------------------------------

  pageIds() {
    const pages = this.doc.pages ?? []
    return pages.length ? pages.map((p) => p.id) : [""]
  }

  // The page a layer is on: its root frame's page, the first page by
  // default.
  pageOf(l) {
    while (l?.parent) l = this.layer(l.parent)
    return l?.page || this.pageIds()[0]
  }

  onPage(l) {
    return this.pageOf(l) === this.page
  }

  showPage(id) {
    if (!this.pageIds().includes(id)) id = this.pageIds()[0]
    if (id === this.page) return
    this.stopEditing()
    this.page = id
    this.selection = []
    this.sync()
    this.notify()
  }

  addPage() {
    const id = "p" + uid()
    this.commit((d) => {
      d.pages ??= []
      if (!d.pages.length) d.pages.push({ id: "p" + uid(), name: "Page 1" })
      d.pages.push({ id, name: "Page " + (d.pages.length + 1) })
    })
    this.showPage(id)
  }

  deletePage() {
    const ids = this.pageIds()
    if (ids.length < 2) return
    const gone = this.page
    const roots = this.doc.layers.filter((l) => !l.parent && this.pageOf(l) === gone).map((l) => l.id)
    const remove = new Set(this.withDescendants(roots).map((l) => l.id))
    this.commit((d) => {
      d.pages = d.pages.filter((p) => p.id !== gone)
      d.layers = d.layers.filter((l) => !remove.has(l.id))
      // Layers of the first page may have no page: keep them first.
      if (ids[0] === gone) for (const l of d.layers) if (!l.parent && !l.page) l.page = d.pages[0].id
    })
    this.showPage(this.pageIds()[Math.max(0, ids.indexOf(gone) - 1)])
  }

  renamePage(name) {
    if (!name.trim() || !this.doc.pages?.length) return
    this.commit((d) => {
      const p = d.pages.find((x) => x.id === this.page)
      if (p) p.name = name.trim()
    })
  }

  // Applies fn to a copy of the document and records the previous state.
  commit(fn, { record = true } = {}) {
    const before = clone(this.doc)
    fn(this.doc)
    if (record) {
      this.past.push(before)
      if (this.past.length > HISTORY) this.past.shift()
      this.future = []
    }
    this.selection = this.selection.filter((id) => this.layer(id))
    this.sync()
    this.changed()
  }

  // Begins an interactive change (drag, resize, typing): the document
  // changes live without history; end() records one history entry.
  begin() {
    this.pending ??= clone(this.doc)
  }

  end() {
    if (!this.pending) return
    if (JSON.stringify(this.pending) !== JSON.stringify(this.doc)) {
      this.past.push(this.pending)
      if (this.past.length > HISTORY) this.past.shift()
      this.future = []
      this.changed()
    }
    this.pending = null
  }

  undo() {
    const prev = this.past.pop()
    if (!prev) return
    this.future.push(clone(this.doc))
    this.doc = prev
    this.selection = this.selection.filter((id) => this.layer(id))
    this.sync()
    this.changed()
  }

  redo() {
    const next = this.future.pop()
    if (!next) return
    this.past.push(clone(this.doc))
    this.doc = next
    this.selection = this.selection.filter((id) => this.layer(id))
    this.sync()
    this.changed()
  }

  changed() {
    this.root.dispatchEvent(new CustomEvent("atelier:change", { bubbles: true, detail: { document: this.doc } }))
    if (this.config.saveURL) {
      clearTimeout(this.saveTimer)
      this.saveTimer = setTimeout(() => this.save(), 500)
    }
    this.notify()
  }

  async save() {
    try {
      await fetch(this.config.saveURL, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(this.doc),
        credentials: "same-origin",
      })
    } catch (err) {
      console.error("[atelier] save:", err)
    }
  }

  // Panels and actions subscribe to changes of the document, the
  // selection or the zoom.
  subscribe(fn) {
    this.listeners.add(fn)
    fn(this)
    return () => this.listeners.delete(fn)
  }

  notify() {
    for (const fn of this.listeners) fn(this)
    this.root.dispatchEvent(new CustomEvent("atelier:update", { detail: { editor: this } }))
  }

  select(ids) {
    this.selection = ids.filter((id) => {
      const l = this.layer(id)
      return l && !l.locked && !l.hidden
    })
    this.drawOverlay()
    this.notify()
  }

  addLayer(type, init = {}) {
    const t = this.types.get(type)
    if (!t) return null
    const l = clone(t.defaults ?? {})
    l.id = uid()
    l.type = type
    l.name ||= t.name
    l.vars ??= {}
    const { vars, ...rest } = init
    if (this.page) l.page = this.page
    Object.assign(l, rest)
    Object.assign(l.vars, vars ?? {})
    // Centered in the frame.
    const w = num(l.vars["--at-width"], 200)
    const h = num(l.vars["--at-height"], 100)
    l.vars["--at-x"] ??= px((this.doc.width - w) / 2)
    l.vars["--at-y"] ??= px((this.doc.height - h) / 2)
    this.commit((d) => d.layers.push(l))
    this.select([l.id])
    return l
  }

  // The selected layers and everything they hold.
  withDescendants(ids) {
    const all = new Set(ids)
    let grew = true
    while (grew) {
      grew = false
      for (const l of this.doc.layers) {
        if (l.parent && all.has(l.parent) && !all.has(l.id)) {
          all.add(l.id)
          grew = true
        }
      }
    }
    return this.doc.layers.filter((l) => all.has(l.id))
  }

  copy() {
    if (!this.selection.length) return
    const text = JSON.stringify({ atelier: 1, layers: clone(this.withDescendants(this.selection)) })
    this.clip = text
    navigator.clipboard?.writeText(text).catch(() => {})
  }

  cut() {
    this.copy()
    this.removeSelected()
  }

  async paste(text) {
    text ??= await navigator.clipboard?.readText?.().catch(() => null)
    text ||= this.clip
    let data
    try {
      data = JSON.parse(text)
    } catch {
      return
    }
    if (!data || data.atelier !== 1 || !Array.isArray(data.layers)) return
    const ids = new Map(data.layers.map((l) => [l.id, uid()]))
    const roots = []
    const layers = data.layers
      .filter((l) => l && typeof l.type === "string" && this.types.has(l.type))
      .map((l) => {
        const c = clone(l)
        c.id = ids.get(l.id)
        if (c.parent && ids.has(c.parent)) c.parent = ids.get(c.parent)
        else {
          // A pasted root lands at the root, offset from the original.
          delete c.parent
          if (this.page) c.page = this.page
          else delete c.page
          c.vars ??= {}
          c.vars["--at-x"] = px(num(c.vars["--at-x"]) + 20)
          c.vars["--at-y"] = px(num(c.vars["--at-y"]) + 20)
          roots.push(c.id)
        }
        return c
      })
    if (!layers.length) return
    this.commit((d) => d.layers.push(...layers))
    this.select(roots)
    // Pasting again offsets again.
    this.clip = JSON.stringify({ atelier: 1, layers: clone(this.withDescendants(roots)) })
  }

  removeSelected() {
    const ids = new Set(this.selection)
    if (!ids.size) return
    // Removing a frame removes what it holds.
    let grew = true
    while (grew) {
      grew = false
      for (const l of this.doc.layers) {
        if (l.parent && ids.has(l.parent) && !ids.has(l.id)) {
          ids.add(l.id)
          grew = true
        }
      }
    }
    this.commit((d) => (d.layers = d.layers.filter((l) => !ids.has(l.id))))
    this.select([])
  }

  duplicateSelected() {
    const copies = []
    this.commit((d) => {
      for (const l of this.selected()) {
        const c = clone(l)
        c.id = uid()
        c.name = l.name + " copy"
        c.vars["--at-x"] = px(num(l.vars["--at-x"]) + 20)
        c.vars["--at-y"] = px(num(l.vars["--at-y"]) + 20)
        d.layers.push(c)
        copies.push(c.id)
      }
    })
    this.select(copies)
  }

  // Sets custom properties on the selected layers (an empty value removes
  // one). live: during an interaction (no history entry per step).
  setVars(vars, { live = false } = {}) {
    const apply = (d) => {
      for (const id of this.selection) {
        const l = d.layers.find((x) => x.id === id)
        if (!l) continue
        l.vars ??= {}
        for (const [k, v] of Object.entries(vars)) {
          if (v === "" || v == null) delete l.vars[k]
          else l.vars[k] = String(v)
        }
      }
    }
    if (live) {
      this.begin()
      apply(this.doc)
      this.sync()
      this.notify()
    } else this.commit(apply)
  }

  setValue(value, { live = false } = {}) {
    const apply = (d) => {
      for (const id of this.selection) {
        const l = d.layers.find((x) => x.id === id)
        if (l) l.value = value
      }
    }
    if (live) {
      this.begin()
      apply(this.doc)
      this.sync()
      this.notify()
    } else this.commit(apply)
  }

  // --- DOM sync -----------------------------------------------------------

  element(id) {
    return this.frame.querySelector(`[data-layer-id="${CSS.escape(id)}"]`)
  }

  sync() {
    const seen = new Set()
    if (!this.pageIds().includes(this.page)) this.page = this.pageIds()[0]
    const place = (parentId, container) => {
      let prev = null
      for (const l of this.doc.layers) {
        if ((l.parent ?? "") !== parentId) continue
        if (!parentId && !this.onPage(l)) continue
        seen.add(l.id)
        let el = this.element(l.id)
        if (!el) el = this.create(l)
        else this.update(el, l)
        // Keep document order (paint order).
        const want = prev ? prev.nextElementSibling : this.firstLayerChild(container)
        if (el.parentElement !== container || want !== el) {
          if (prev) prev.after(el)
          else container.prepend(el)
        }
        prev = el
        place(l.id, el)
      }
    }
    place("", this.frame)
    for (const el of [...this.frame.querySelectorAll('[data-slot="atelier-layer"]')]) {
      if (!seen.has(el.dataset.layerId)) el.remove()
    }
    this.frame.style.width = this.doc.width + "px"
    this.frame.style.height = this.doc.height + "px"
    this.drawOverlay()
  }

  firstLayerChild(container) {
    return [...container.children].find((c) => c.matches('[data-slot="atelier-layer"]')) ?? null
  }

  create(l) {
    const el = document.createElement("div")
    el.dataset.slot = "atelier-layer"
    el.dataset.layerId = l.id
    el.dataset.layerType = l.type
    el.className = "atelier-layer atelier-layer-" + l.type.replace(/[^\w-]/g, "")
    this.update(el, l)
    return el
  }

  update(el, l) {
    const style = Object.entries(l.vars ?? {})
      .filter(([k]) => /^--[\w-]+$/.test(k))
      .sort(([a], [b]) => (a < b ? -1 : 1))
      .map(([k, v]) => `${k}: ${String(v).replace(/[;{}<\\\n\r]/g, "")}`)
      .join("; ")
    if (el.getAttribute("style") !== style) {
      if (style) el.setAttribute("style", style)
      else el.removeAttribute("style")
    }
    el.hidden = !!l.hidden
    if (l.locked) el.dataset.locked = "true"
    else delete el.dataset.locked
    if (!BUILTIN.has(l.type)) {
      this.renderCustom(el, l)
    } else if (l.type === "text") {
      let t = el.querySelector(':scope > [data-slot="atelier-text"]')
      if (!t) {
        t = document.createElement("div")
        t.dataset.slot = "atelier-text"
        el.prepend(t)
      }
      if (t !== this.editing && t.textContent !== (l.value ?? "")) t.textContent = l.value ?? ""
    } else if (l.type === "image") {
      let img = el.querySelector(':scope > img[data-slot="atelier-image"]')
      const empty = el.querySelector(':scope > [data-slot="atelier-image-empty"]')
      if (l.value) {
        empty?.remove()
        if (!img) {
          img = document.createElement("img")
          img.dataset.slot = "atelier-image"
          img.draggable = false
          el.prepend(img)
        }
        if (img.getAttribute("src") !== l.value) img.setAttribute("src", l.value)
        img.alt = l.name ?? ""
      } else {
        img?.remove()
        if (!empty) {
          const e = document.createElement("div")
          e.dataset.slot = "atelier-image-empty"
          e.innerHTML =
            '<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="18" height="18" x="3" y="3" rx="2" ry="2"></rect><circle cx="9" cy="9" r="2"></circle><path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"></path></svg>'
          el.prepend(e)
        }
      }
    }
  }

  // A custom layer's content comes from its type's Go Render, fetched again
  // when the layer's data changes (debounced).
  renderCustom(el, l) {
    if (!this.config.renderURL) return
    const { id, name, ...data } = l
    const key = JSON.stringify(data)
    if (el.dataset.rendered === key) return
    el.dataset.rendered = key
    clearTimeout(el.renderTimer)
    el.renderTimer = setTimeout(async () => {
      try {
        const res = await fetch(this.config.renderURL, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ layer: l }),
          credentials: "same-origin",
        })
        if (!res.ok || el.dataset.rendered !== key) return
        const html = res.status === 204 ? "" : await res.text()
        // The layer's own content: everything but its child layers.
        for (const c of [...el.childNodes]) {
          if (!(c.nodeType === 1 && c.matches('[data-slot="atelier-layer"]'))) c.remove()
        }
        const t = document.createElement("template")
        t.innerHTML = html
        el.prepend(t.content)
      } catch (err) {
        console.error("[atelier] render:", err)
      }
    }, 120)
  }

  // --- geometry -----------------------------------------------------------

  // The layer's box in frame coordinates: its parents' offsets plus its
  // own translate (rotation of parents is not composed).
  box(l) {
    let x = num(l.vars?.["--at-x"])
    let y = num(l.vars?.["--at-y"])
    let p = l.parent ? this.layer(l.parent) : null
    while (p) {
      x += num(p.vars?.["--at-x"])
      y += num(p.vars?.["--at-y"])
      p = p.parent ? this.layer(p.parent) : null
    }
    const el = this.element(l.id)
    const w = num(l.vars?.["--at-width"], el ? el.offsetWidth : 0)
    const h = num(l.vars?.["--at-height"], el ? el.offsetHeight : 0)
    return { x, y, w, h, r: num(l.vars?.["--at-rotate"]) }
  }

  // Canvas (client) point -> frame coordinates.
  toFrame(clientX, clientY) {
    const r = this.canvas.getBoundingClientRect()
    return [(clientX - r.left - this.pan[0]) / this.zoom, (clientY - r.top - this.pan[1]) / this.zoom]
  }

  applyView() {
    this.viewport.style.transform = `translate(${this.pan[0]}px, ${this.pan[1]}px) scale(${this.zoom})`
    this.drawOverlay()
    this.notify()
  }

  fit() {
    const r = this.canvas.getBoundingClientRect()
    if (!r.width || !r.height) return
    const pad = 64
    const bottom = this.root.querySelector('[data-slot="atelier-toolbar"]') ? 56 : 0
    const z = Math.min((r.width - pad * 2) / this.doc.width, (r.height - pad * 2 - bottom) / this.doc.height)
    this.zoom = Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, z))
    this.pan = [(r.width - this.doc.width * this.zoom) / 2, (r.height - bottom - this.doc.height * this.zoom) / 2]
    this.applyView()
  }

  // Zooms around a canvas point (client coordinates; the center by default).
  zoomTo(z, clientX, clientY) {
    const r = this.canvas.getBoundingClientRect()
    clientX ??= r.left + r.width / 2
    clientY ??= r.top + r.height / 2
    const [fx, fy] = this.toFrame(clientX, clientY)
    this.zoom = Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, z))
    this.pan = [clientX - r.left - fx * this.zoom, clientY - r.top - fy * this.zoom]
    this.applyView()
  }

  // --- overlay ------------------------------------------------------------

  drawOverlay() {
    if (!this.overlay) return
    this.overlay.replaceChildren()
    if (this.readOnly) return
    const layers = this.selected()
    for (const l of layers) {
      const b = this.box(l)
      const box = document.createElement("div")
      box.dataset.slot = "atelier-selection"
      box.dataset.layerId = l.id
      Object.assign(box.style, {
        position: "absolute",
        left: this.pan[0] + b.x * this.zoom + "px",
        top: this.pan[1] + b.y * this.zoom + "px",
        width: b.w * this.zoom + "px",
        height: b.h * this.zoom + "px",
        transform: `rotate(${b.r}deg)`,
        transformOrigin: "center",
        outline: "1.5px solid #3b82f6",
        pointerEvents: "none",
      })
      if (layers.length === 1 && !this.editing) {
        for (const [hx, hy] of [[-1, -1], [0, -1], [1, -1], [1, 0], [1, 1], [0, 1], [-1, 1], [-1, 0]]) {
          const hd = document.createElement("div")
          hd.dataset.handle = `${hx},${hy}`
          Object.assign(hd.style, {
            position: "absolute",
            left: `calc(${(hx + 1) * 50}% - 5px)`,
            top: `calc(${(hy + 1) * 50}% - 5px)`,
            width: "10px",
            height: "10px",
            borderRadius: "9999px",
            background: "#ffffff",
            border: "1.5px solid #3b82f6",
            pointerEvents: "auto",
            cursor: this.cursor(hx, hy, b.r),
          })
          box.append(hd)
        }
        const stem = document.createElement("div")
        Object.assign(stem.style, { position: "absolute", left: "calc(50% - 0.75px)", top: "-28px", width: "1.5px", height: "28px", background: "#3b82f6" })
        const rot = document.createElement("div")
        rot.dataset.handle = "rotate"
        Object.assign(rot.style, {
          position: "absolute",
          left: "calc(50% - 6px)",
          top: "-36px",
          width: "12px",
          height: "12px",
          borderRadius: "9999px",
          background: "#ffffff",
          border: "1.5px solid #3b82f6",
          pointerEvents: "auto",
          cursor: "grab",
        })
        box.append(stem, rot)
        const label = document.createElement("div")
        const k = this.perUnit()
        const fmt = (v) => (k === 1 ? Math.round(v) : round(v / k))
        label.textContent = `${fmt(b.w)} × ${fmt(b.h)} ${this.unitName()}`
        Object.assign(label.style, {
          position: "absolute",
          left: "50%",
          top: "calc(100% + 10px)",
          transform: `translateX(-50%) rotate(${-b.r}deg)`,
          background: "#3b82f6",
          color: "#ffffff",
          font: "500 11px/1 system-ui, sans-serif",
          padding: "4px 6px",
          borderRadius: "4px",
          whiteSpace: "nowrap",
        })
        box.append(label)
      }
      this.overlay.append(box)
    }
  }

  cursor(hx, hy, r) {
    const cursors = ["ew-resize", "nwse-resize", "ns-resize", "nesw-resize"]
    const angle = (Math.atan2(hy, hx) * 180) / Math.PI + r
    const i = Math.round((((angle % 180) + 180) % 180) / 45) % 4
    return cursors[i]
  }

  // --- interaction --------------------------------------------------------

  // The topmost selectable layer under a client point.
  hit(target) {
    let el = target.closest?.('[data-slot="atelier-layer"]')
    while (el && this.frame.contains(el)) {
      const l = this.layer(el.dataset.layerId)
      if (l && !l.locked && !l.hidden) return l
      el = el.parentElement?.closest('[data-slot="atelier-layer"]')
    }
    return null
  }

  mount() {
    this.sync()
    const on = (target, type, fn, opts) => {
      target.addEventListener(type, fn, opts)
      this.cleanups.push(() => target.removeEventListener(type, fn, opts))
    }
    const ro = new ResizeObserver(() => (this.fitted ? this.drawOverlay() : this.fitOnce()))
    ro.observe(this.canvas)
    this.cleanups.push(() => ro.disconnect())
    this.fitOnce()

    on(this.canvas, "wheel", (e) => this.onWheel(e), { passive: false })
    on(this.canvas, "pointerdown", (e) => this.onPointerDown(e))
    on(this.canvas, "dblclick", (e) => this.onDoubleClick(e))
    for (const type of ["pointerup", "pointercancel"]) on(window, type, (e) => this.touches?.size < 2 && this.touches.delete(e.pointerId))
    on(document, "paste", (e) => {
      if (this.readOnly || !this.root.isConnected) return
      const t = e.target
      if (t instanceof HTMLInputElement || t instanceof HTMLTextAreaElement || t?.isContentEditable) return
      if (!this.root.contains(document.activeElement) && document.activeElement !== document.body) return
      const files = [...(e.clipboardData?.files ?? [])].filter((f) => f.type.startsWith("image/"))
      e.preventDefault()
      if (files.length) {
        const r = this.canvas.getBoundingClientRect()
        return this.dropFiles(files, r.left + r.width / 2, r.top + r.height / 2)
      }
      this.paste(e.clipboardData?.getData("text/plain") || this.clip)
    })
    // A right click selects the layer under the pointer (for the menu).
    on(this.canvas, "pointerdown", (e) => {
      if (e.button !== 2 || this.readOnly) return
      const l = this.hit(e.target)
      if (l && !this.selection.includes(l.id)) this.select([l.id])
    })
    on(this.canvas, "dragover", (e) => {
      if (!this.readOnly && [...(e.dataTransfer?.items ?? [])].some((i) => i.kind === "file")) e.preventDefault()
    })
    on(this.canvas, "drop", (e) => {
      if (this.readOnly || !e.dataTransfer?.files?.length) return
      e.preventDefault()
      this.dropFiles([...e.dataTransfer.files], e.clientX, e.clientY)
    })
    on(window, "keydown", (e) => this.onKey(e))
    // Commands come from buttons in the editor and from menu items, which
    // menus render in portals outside it: those go to the only editor of
    // the page, or to the one their data-atelier-for names.
    on(document, "click", (e) => {
      const cmd = e.target.closest?.("[data-atelier-command]")
      if (!cmd) return
      const owner = cmd.closest('[data-slot="atelier"]')
      if (owner ? owner !== this.root : !this.ownsPortal(cmd)) return
      this.command(cmd.dataset.atelierCommand, cmd)
    })
    this.cleanups.push(this.subscribe(() => this.updateChrome()))
    this.root.atelier = this
    this.bindControls()
    this.notify()
    return () => this.cleanups.forEach((fn) => fn())
  }

  ownsPortal(cmd) {
    if (cmd.dataset.atelierFor) return cmd.dataset.atelierFor === this.root.id
    return document.querySelectorAll('[data-slot="atelier"]').length === 1
  }

  // Zoom label, history buttons, the layer tree.
  updateChrome() {
    for (const el of this.root.querySelectorAll("[data-atelier-zoom-label]")) el.textContent = Math.round(this.zoom * 100) + "%"
    for (const el of this.root.querySelectorAll('[data-atelier-command="undo"]')) el.disabled = !this.past.length
    for (const el of this.root.querySelectorAll('[data-atelier-command="redo"]')) el.disabled = !this.future.length
    this.renderTree()
    for (const sel of this.root.querySelectorAll("select[data-atelier-page-select]")) {
      const pages = this.doc.pages?.length ? this.doc.pages : [{ id: "", name: "Page 1" }]
      const key = JSON.stringify(pages)
      if (sel.dataset.key !== key) {
        sel.dataset.key = key
        sel.replaceChildren(...pages.map((p) => new Option(p.name || "Page", p.id)))
      }
      sel.value = this.page
    }
    for (const el of this.root.querySelectorAll('[data-atelier-command="delete-page"]')) el.disabled = this.pageIds().length < 2
    for (const el of this.root.querySelectorAll("[data-atelier-page-count]")) el.textContent = String(this.pageIds().length)
  }

  icon(name) {
    const t = this.root.querySelector(`template[data-atelier-icon="${CSS.escape(name)}"]`)
    return t ? t.content.firstElementChild.cloneNode(true) : document.createElement("span")
  }

  // The layer tree lists the topmost layer first, children under their
  // frame.
  renderTree() {
    const list = this.root.querySelector("[data-atelier-layer-list]")
    if (!list || this.renamingTree) return
    const rows = []
    const walk = (parent, depth) => {
      const kids = this.doc.layers.filter((l) => (l.parent ?? "") === parent && (parent || this.onPage(l))).reverse()
      for (const l of kids) {
        rows.push([l, depth])
        walk(l.id, depth + 1)
      }
    }
    walk("", 0)
    const key = this.page + JSON.stringify(rows.map(([l, d]) => [l.id, l.name, l.type, !!l.hidden, !!l.locked, d])) + this.selection.join()
    if (key === this.treeKey) return
    this.treeKey = key
    list.replaceChildren()
    for (const [l, depth] of rows) {
      const row = document.createElement("div")
      row.dataset.layerRow = l.id
      row.dataset.selected = String(this.selection.includes(l.id))
      row.className =
        "group flex h-8 cursor-default items-center gap-2 rounded-md pr-1 text-xs hover:bg-accent/60 data-[selected=true]:bg-accent data-[hidden=true]:opacity-50"
      row.dataset.hidden = String(!!l.hidden)
      row.style.paddingLeft = 8 + depth * 14 + "px"
      const ic = this.icon(l.type)
      ic.classList.add("size-3.5", "shrink-0", "text-muted-foreground")
      const name = document.createElement("span")
      name.className = "min-w-0 flex-1 truncate"
      name.textContent = l.name || l.type
      const toggle = (kind, on, offIcon, onIcon, label) => {
        const b = document.createElement("button")
        b.type = "button"
        b.dataset.layerToggle = kind
        b.ariaLabel = label
        b.className =
          "inline-flex size-6 items-center justify-center rounded text-muted-foreground opacity-0 hover:text-foreground group-hover:opacity-100 data-[on=true]:opacity-100 [&_svg]:size-3.5"
        b.dataset.on = String(on)
        b.append(this.icon(on ? onIcon : offIcon))
        return b
      }
      row.append(
        ic,
        name,
        toggle("locked", !!l.locked, "@unlock", "@lock", l.locked ? "Unlock" : "Lock"),
        toggle("hidden", !!l.hidden, "@eye", "@eye-off", l.hidden ? "Show" : "Hide"),
      )
      list.append(row)
    }
    if (!list.dataset.bound) {
      list.dataset.bound = "true"
      list.addEventListener("click", (e) => {
        const row = e.target.closest("[data-layer-row]")
        if (!row) return
        const id = row.dataset.layerRow
        const t = e.target.closest("[data-layer-toggle]")
        if (t && this.readOnly) return
        if (t) {
          const kind = t.dataset.layerToggle
          this.commit((d) => {
            const l = d.layers.find((x) => x.id === id)
            if (l) l[kind] = !l[kind] || undefined
          })
          if (kind !== "hidden" || !this.layer(id)?.hidden) return
          this.select(this.selection.filter((x) => x !== id))
          return
        }
        const l = this.layer(id)
        if (!l) return
        // The tree selects hidden and locked layers too.
        this.selection = e.shiftKey
          ? this.selection.includes(id)
            ? this.selection.filter((x) => x !== id)
            : [...this.selection, id]
          : [id]
        this.drawOverlay()
        this.notify()
      })
      list.addEventListener("pointerdown", (e) => this.treeDrag(e, list))
      list.addEventListener("dblclick", (e) => {
        if (this.readOnly) return
        const row = e.target.closest("[data-layer-row]")
        const name = row?.querySelector("span.truncate")
        if (!name) return
        const l = this.layer(row.dataset.layerRow)
        const input = document.createElement("input")
        input.value = l.name ?? ""
        input.className = "h-6 min-w-0 flex-1 rounded bg-background px-1 text-xs outline-none ring-2 ring-ring/50"
        this.renamingTree = true
        name.replaceWith(input)
        input.focus()
        input.select()
        const done = (save) => {
          if (!this.renamingTree) return
          this.renamingTree = false
          this.treeKey = null
          if (save && input.value.trim() && input.value !== l.name) {
            this.commit((d) => (d.layers.find((x) => x.id === l.id).name = input.value.trim()))
          } else this.renderTree()
        }
        input.addEventListener("keydown", (ev) => {
          if (ev.key === "Enter") done(true)
          if (ev.key === "Escape") done(false)
          ev.stopPropagation()
        })
        input.addEventListener("blur", () => done(true))
      })
    }
  }

  // Absolute path of a URL a handler endpoint answered (relative to it).
  resolve(url, base) {
    const u = new URL(url, new URL(base, location.href))
    return u.origin === location.origin ? u.pathname + u.search : u.href
  }

  // Generates an image from the pane's prompt, then inserts it.
  async generate(button) {
    const pane = button.closest('[data-slot="atelier-generate"]')
    if (!pane || !this.config.generateURL || button.dataset.busy === "true") return
    const prompt = pane.querySelector("[data-atelier-gen-prompt]").value.trim()
    const status = pane.querySelector("[data-atelier-gen-status]")
    if (!prompt) {
      status.textContent = "Describe the image first."
      return
    }
    const [width, height] = pane.querySelector("[data-atelier-gen-size]").value.split("x").map(Number)
    button.dataset.busy = "true"
    button.disabled = true
    status.textContent = "Generating…"
    const t0 = performance.now()
    try {
      const res = await fetch(this.config.generateURL, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ prompt, width, height }),
        credentials: "same-origin",
      })
      if (!res.ok) throw new Error((await res.text()).trim() || res.statusText)
      const url = this.resolve((await res.json()).url, this.config.generateURL)
      status.textContent = `Done in ${((performance.now() - t0) / 1000).toFixed(1)} s.`
      const thumb = document.createElement("button")
      thumb.type = "button"
      thumb.title = prompt
      thumb.className = "overflow-hidden rounded-md bg-muted hover:ring-2 hover:ring-ring/50"
      const img = document.createElement("img")
      img.src = url
      img.alt = prompt
      img.className = "aspect-square w-full object-cover"
      thumb.append(img)
      thumb.addEventListener("click", () => this.insertImage(url, prompt, width, height))
      pane.querySelector("[data-atelier-gen-results]").prepend(thumb)
      this.insertImage(url, prompt, width, height)
    } catch (err) {
      status.textContent = "Failed: " + err.message
    } finally {
      button.dataset.busy = "false"
      button.disabled = false
    }
  }

  // Edits the selected image layer with a prompt; the result replaces its
  // source (one undoable change).
  async editImage(button) {
    const box = button.closest('[data-slot="atelier-image-edit"]')
    const l = this.selected()[0]
    const status = box?.querySelector("[data-atelier-edit-status]")
    if (!box || !l || l.type !== "image" || !this.config.editURL || button.disabled) return
    const prompt = box.querySelector("[data-atelier-edit-prompt]").value.trim()
    if (!l.value) return (status.textContent = "This layer has no image yet.")
    if (!prompt) return (status.textContent = "Describe the change first.")
    button.disabled = true
    status.textContent = "Editing…"
    const t0 = performance.now()
    try {
      const blob = await (await fetch(l.value)).blob()
      // The model works at multiples of 64, at most 768 on the long side.
      const img = await createImageBitmap(blob)
      const k = Math.min(1, 768 / Math.max(img.width, img.height))
      const snap = (v) => Math.max(64, Math.round((v * k) / 64) * 64)
      const form = new FormData()
      form.append("image", blob, "image")
      form.append(
        "request",
        JSON.stringify({ prompt, width: snap(img.width), height: snap(img.height), strength: num(box.querySelector("[data-atelier-edit-strength]").value, 0.45) }),
      )
      const res = await fetch(this.config.editURL, { method: "POST", body: form, credentials: "same-origin" })
      if (!res.ok) throw new Error((await res.text()).trim() || res.statusText)
      const url = this.resolve((await res.json()).url, this.config.editURL)
      const id = l.id
      this.commit((d) => {
        const x = d.layers.find((y) => y.id === id)
        if (x) x.value = url
      })
      status.textContent = `Done in ${((performance.now() - t0) / 1000).toFixed(1)} s.`
    } catch (err) {
      status.textContent = "Failed: " + err.message
    } finally {
      button.disabled = false
    }
  }

  // Inserts an image layer, fit in 60% of the frame and centered.
  insertImage(url, name, w, h) {
    const max = Math.min(this.doc.width, this.doc.height) * 0.6
    const s = Math.min(max / w, max / h)
    this.addLayer("image", {
      name: name.slice(0, 40),
      value: url,
      vars: { "--at-width": px(w * s), "--at-height": px(h * s), "--at-fit": "cover" },
    })
  }

  // Wraps the selected layers (siblings) in a new frame sized to their
  // bounds; positions are converted so nothing moves.
  group() {
    const layers = this.selected()
    if (!layers.length || !this.types.get("frame")?.container) return
    const parent = layers[0].parent ?? ""
    if (layers.some((l) => (l.parent ?? "") !== parent)) return
    const b = this.bounds(layers)
    const [ox, oy] = parent ? [this.box(this.layer(parent)).x, this.box(this.layer(parent)).y] : [0, 0]
    const g = { id: uid(), name: "Group", type: "frame", vars: { "--at-width": px(b.x1 - b.x0), "--at-height": px(b.y1 - b.y0), "--at-x": px(b.x0 - ox), "--at-y": px(b.y0 - oy) } }
    if (parent) g.parent = parent
    else if (this.page) g.page = this.page
    const ids = new Set(layers.map((l) => l.id))
    this.commit((d) => {
      // The group takes the place of its topmost member.
      let at = -1
      d.layers.forEach((l, i) => ids.has(l.id) && (at = i))
      for (const l of d.layers) {
        if (!ids.has(l.id)) continue
        l.vars ??= {}
        l.vars["--at-x"] = px(num(l.vars["--at-x"]) + ox - b.x0)
        l.vars["--at-y"] = px(num(l.vars["--at-y"]) + oy - b.y0)
        l.parent = g.id
        delete l.page
      }
      d.layers.splice(at + 1, 0, g)
      // Members after the group in paint order: move them after it.
      const members = d.layers.filter((l) => ids.has(l.id))
      d.layers = d.layers.filter((l) => !ids.has(l.id))
      const gi = d.layers.findIndex((l) => l.id === g.id)
      d.layers.splice(gi + 1, 0, ...members)
    })
    this.select([g.id])
  }

  // Replaces the selected frames by their children (positions kept).
  ungroup() {
    const frames = this.selected().filter((l) => this.types.get(l.type)?.container)
    if (!frames.length) return
    const out = []
    this.commit((d) => {
      for (const f of frames) {
        const fx = num(f.vars?.["--at-x"])
        const fy = num(f.vars?.["--at-y"])
        for (const l of d.layers) {
          if (l.parent !== f.id) continue
          l.vars ??= {}
          l.vars["--at-x"] = px(num(l.vars["--at-x"]) + fx)
          l.vars["--at-y"] = px(num(l.vars["--at-y"]) + fy)
          if (f.parent) l.parent = f.parent
          else {
            delete l.parent
            if (f.page) l.page = f.page
          }
          out.push(l.id)
        }
        d.layers = d.layers.filter((l) => l.id !== f.id)
      }
    })
    this.select(out)
  }

  // Inserts a template's layers, centered in the frame, with new ids.
  insertTemplate(layers) {
    if (!Array.isArray(layers) || !layers.length) return
    const ids = new Map(layers.map((l) => [l.id, uid()]))
    const roots = layers.filter((l) => !l.parent)
    let x0 = Infinity
    let y0 = Infinity
    let x1 = -Infinity
    let y1 = -Infinity
    for (const l of roots) {
      const x = num(l.vars?.["--at-x"])
      const y = num(l.vars?.["--at-y"])
      x0 = Math.min(x0, x)
      y0 = Math.min(y0, y)
      x1 = Math.max(x1, x + num(l.vars?.["--at-width"]))
      y1 = Math.max(y1, y + num(l.vars?.["--at-height"]))
    }
    const dx = (this.doc.width - (x1 - x0)) / 2 - x0
    const dy = (this.doc.height - (y1 - y0)) / 2 - y0
    const out = layers
      .filter((l) => this.types.has(l.type))
      .map((l) => {
        const c = clone(l)
        c.id = ids.get(l.id)
        c.vars ??= {}
        if (c.parent) c.parent = ids.get(c.parent)
        else {
          c.vars["--at-x"] = px(num(c.vars["--at-x"]) + dx)
          c.vars["--at-y"] = px(num(c.vars["--at-y"]) + dy)
          if (this.page) c.page = this.page
        }
        return c
      })
    this.commit((d) => d.layers.push(...out))
    this.select(out.filter((l) => !l.parent).map((l) => l.id))
  }

  // Dragging rows reorders layers: above or below a row, or into a frame
  // (the middle of its row). Positions are converted so nothing moves.
  treeDrag(e, list) {
    if (this.readOnly || e.button !== 0 || e.target.closest("[data-layer-toggle], input")) return
    const row = e.target.closest("[data-layer-row]")
    if (!row) return
    const startY = e.clientY
    let moving = null
    let drop = null
    const line = document.createElement("div")
    line.className = "pointer-events-none absolute right-2 left-2 h-0.5 rounded bg-blue-500"
    const ring = "ring-2 ring-inset ring-blue-500"
    list.style.position = "relative"
    const clear = () => {
      line.remove()
      for (const r of list.querySelectorAll("[data-layer-row]")) r.classList.remove(...ring.split(" "))
    }
    const move = (ev) => {
      if (!moving) {
        if (Math.abs(ev.clientY - startY) < 4) return
        const id = row.dataset.layerRow
        moving = this.selection.includes(id) ? [...this.selection] : [id]
      }
      clear()
      drop = null
      const target = document.elementFromPoint(ev.clientX, ev.clientY)?.closest("[data-layer-row]")
      if (!target || !list.contains(target)) return
      const tl = this.layer(target.dataset.layerRow)
      // Never into itself or what it holds.
      const inside = new Set(this.withDescendants(moving).map((l) => l.id))
      if (!tl || inside.has(tl.id)) return
      const r = target.getBoundingClientRect()
      const f = (ev.clientY - r.top) / r.height
      const container = this.types.get(tl.type)?.container
      if (container && f > 0.25 && f < 0.75) {
        drop = { into: tl.id }
        target.classList.add(...ring.split(" "))
        return
      }
      drop = { target: tl.id, above: f < 0.5 }
      const lr = list.getBoundingClientRect()
      line.style.top = (f < 0.5 ? r.top : r.bottom) - lr.top - 1 + "px"
      list.append(line)
    }
    const up = () => {
      document.removeEventListener("pointermove", move)
      document.removeEventListener("pointerup", up)
      clear()
      if (moving && drop) this.moveLayers(moving, drop)
    }
    document.addEventListener("pointermove", move)
    document.addEventListener("pointerup", up)
  }

  moveLayers(ids, drop) {
    const target = drop.into ? this.layer(drop.into) : this.layer(drop.target)
    const parent = drop.into ? drop.into : target.parent ?? ""
    // Absolute frame position of a would-be parent.
    const origin = (pid) => {
      if (!pid) return [0, 0]
      const b = this.box(this.layer(pid))
      return [b.x, b.y]
    }
    const [px0, py0] = origin(parent)
    const roots = ids.map((id) => this.layer(id)).filter((l) => l && !ids.includes(l.parent))
    const abs = new Map(roots.map((l) => [l.id, this.box(l)]))
    this.commit((d) => {
      const moved = d.layers.filter((l) => roots.some((r) => r.id === l.id))
      d.layers = d.layers.filter((l) => !moved.includes(l))
      for (const l of moved) {
        const b = abs.get(l.id)
        l.vars ??= {}
        l.vars["--at-x"] = px(b.x - px0)
        l.vars["--at-y"] = px(b.y - py0)
        if (parent) {
          l.parent = parent
          delete l.page
        } else {
          delete l.parent
          if (this.page) l.page = this.page
        }
      }
      if (drop.into) {
        d.layers.push(...moved)
        return
      }
      // The tree shows the topmost layer first: "above" paints after.
      const i = d.layers.findIndex((l) => l.id === drop.target)
      d.layers.splice(drop.above ? i + 1 : i, 0, ...moved)
    })
  }

  // --- export -------------------------------------------------------------

  download(blob, name) {
    const a = document.createElement("a")
    a.href = URL.createObjectURL(blob)
    a.download = name
    document.body.append(a)
    a.click()
    a.remove()
    setTimeout(() => URL.revokeObjectURL(a.href), 1000)
  }

  stylesheetText() {
    return Promise.all(
      [...document.querySelectorAll('link[rel="stylesheet"]')].map((l) =>
        fetch(l.href)
          .then((r) => (r.ok ? r.text() : ""))
          .catch(() => ""),
      ),
    ).then((parts) => parts.join("\n"))
  }

  // A copy of the frame with images inlined as data URLs.
  async frameCopy() {
    const copy = this.frame.cloneNode(true)
    copy.style.transform = ""
    // Outside the page (an SVG image, a print window) nothing is inherited
    // from the page's body: carry the frame's own text defaults.
    const cs = getComputedStyle(this.frame)
    for (const prop of ["font-family", "font-size", "line-height", "color", "-webkit-font-smoothing"]) {
      copy.style.setProperty(prop, cs.getPropertyValue(prop))
    }
    for (const img of copy.querySelectorAll("img")) {
      try {
        const blob = await (await fetch(img.src, { mode: "cors" })).blob()
        img.src = await new Promise((res) => {
          const r = new FileReader()
          r.onload = () => res(r.result)
          r.readAsDataURL(blob)
        })
      } catch {
        // Cross-origin without CORS: the image is left out of the export.
      }
    }
    return copy
  }

  async exportPNG(scale = 2) {
    const [css, copy] = await Promise.all([this.stylesheetText(), this.frameCopy()])
    const { width, height } = this.doc
    copy.setAttribute("xmlns", "http://www.w3.org/1999/xhtml")
    const html = new XMLSerializer().serializeToString(copy)
    const svg =
      `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}">` +
      `<foreignObject width="100%" height="100%"><div xmlns="http://www.w3.org/1999/xhtml">` +
      `<style><![CDATA[${css.replace(/]]>/g, "")}]]></style>${html}</div></foreignObject></svg>`
    const img = new Image()
    img.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svg)
    await img.decode()
    const canvas = document.createElement("canvas")
    canvas.width = width * scale
    canvas.height = height * scale
    const ctx = canvas.getContext("2d")
    ctx.scale(scale, scale)
    ctx.drawImage(img, 0, 0)
    canvas.toBlob((blob) => blob && this.download(blob, "design.png"), "image/png")
  }

  // Copies of every page's frame (switching pages to render them).
  async pageCopies() {
    const current = this.page
    const selection = this.selection
    const out = []
    for (const id of this.pageIds()) {
      this.page = id
      this.sync()
      out.push(await this.frameCopy())
    }
    this.page = current
    this.selection = selection
    this.sync()
    return out
  }

  async exportPDF() {
    const [css, copies] = await Promise.all([this.stylesheetText(), this.pageCopies()])
    const { width, height } = this.doc
    // Physical page size: pixels at the document's DPI.
    const dpi = this.doc.dpi || 96
    const pageSize = `${round(width / dpi)}in ${round(height / dpi)}in`
    const w = window.open("", "_blank")
    if (!w) return
    w.document.write(
      `<!doctype html><html><head><title>Design</title><style>${css}</style>` +
        `<style>@page{size:${pageSize};margin:0}html,body{margin:0;padding:0}` +
        `[data-slot="atelier-frame"]{zoom:${round(96 / dpi)}}` +
        `[data-slot="atelier-frame"]{break-after:page}[data-slot="atelier-frame"]:last-child{break-after:auto}</style></head><body></body></html>`,
    )
    w.document.close()
    for (const copy of copies) w.document.body.append(w.document.importNode(copy, true))
    setTimeout(() => {
      w.focus()
      w.print()
    }, 300)
  }

  exportJSON() {
    this.download(new Blob([JSON.stringify(this.doc, null, 2)], { type: "application/json" }), "design.json")
  }

  fitOnce() {
    const r = this.canvas.getBoundingClientRect()
    if (!r.width || !r.height) return
    this.fitted = true
    this.fit()
  }

  onWheel(e) {
    e.preventDefault()
    if (e.ctrlKey || e.metaKey) {
      // Pinch or ctrl/cmd + wheel: zoom around the pointer.
      this.zoomTo(this.zoom * Math.exp(-e.deltaY * 0.01), e.clientX, e.clientY)
    } else {
      this.pan = [this.pan[0] - e.deltaX, this.pan[1] - e.deltaY]
      this.applyView()
    }
  }

  onPointerDown(e) {
    for (const panel of this.root.querySelectorAll('[data-slot="atelier-panel"][data-open="true"]')) panel.dataset.open = "false"
    if (e.pointerType === "touch") {
      this.touches ??= new Map()
      this.touches.set(e.pointerId, [e.clientX, e.clientY])
      if (this.touches.size === 2) return this.pinch()
    }
    if (e.button === 1 || (e.button === 0 && this.spaceDown)) return this.dragPan(e)
    if (e.button !== 0 || this.readOnly) return
    const handle = e.target.closest?.("[data-handle]")
    if (handle) {
      e.preventDefault()
      if (handle.dataset.handle === "rotate") return this.dragRotate(e)
      return this.dragResize(e, handle.dataset.handle.split(",").map(Number))
    }
    if (this.editing && this.editing.contains(e.target)) return
    this.stopEditing()
    const l = this.hit(e.target)
    if (!l) return this.dragMarquee(e)
    if (e.shiftKey) {
      const ids = this.selection.includes(l.id) ? this.selection.filter((i) => i !== l.id) : [...this.selection, l.id]
      this.select(ids)
      return
    }
    if (!this.selection.includes(l.id)) this.select([l.id])
    this.canvas.focus({ preventScroll: true })
    this.dragMove(e)
  }

  // Two fingers zoom around their midpoint and pan.
  pinch() {
    this.end()
    const pts = () => [...this.touches.values()]
    const mid = (p) => [(p[0][0] + p[1][0]) / 2, (p[0][1] + p[1][1]) / 2]
    const dist = (p) => Math.hypot(p[0][0] - p[1][0], p[0][1] - p[1][1])
    let last = pts()
    const onMove = (ev) => {
      if (!this.touches.has(ev.pointerId)) return
      this.touches.set(ev.pointerId, [ev.clientX, ev.clientY])
      const now = pts()
      if (now.length < 2) return
      const [mx, my] = mid(now)
      const [lx, ly] = mid(last)
      this.pan = [this.pan[0] + mx - lx, this.pan[1] + my - ly]
      this.zoomTo(this.zoom * (dist(now) / (dist(last) || 1)), mx, my)
      last = now
    }
    const onUp = (ev) => {
      this.touches.delete(ev.pointerId)
      if (this.touches.size < 2) {
        this.canvas.removeEventListener("pointermove", onMove)
        this.canvas.removeEventListener("pointerup", onUp)
        this.canvas.removeEventListener("pointercancel", onUp)
        this.touches.clear()
      }
    }
    this.canvas.addEventListener("pointermove", onMove)
    this.canvas.addEventListener("pointerup", onUp)
    this.canvas.addEventListener("pointercancel", onUp)
  }

  track(e, move, up) {
    const id = e.pointerId
    this.canvas.setPointerCapture?.(id)
    const onMove = (ev) => ev.pointerId === id && move(ev)
    const onUp = (ev) => {
      if (ev.pointerId !== id) return
      this.canvas.removeEventListener("pointermove", onMove)
      this.canvas.removeEventListener("pointerup", onUp)
      this.canvas.removeEventListener("pointercancel", onUp)
      up?.(ev)
    }
    this.canvas.addEventListener("pointermove", onMove)
    this.canvas.addEventListener("pointerup", onUp)
    this.canvas.addEventListener("pointercancel", onUp)
  }

  dragPan(e) {
    e.preventDefault()
    const start = [e.clientX, e.clientY]
    const pan = [...this.pan]
    this.canvas.style.cursor = "grabbing"
    this.track(
      e,
      (ev) => {
        this.pan = [pan[0] + ev.clientX - start[0], pan[1] + ev.clientY - start[1]]
        this.applyView()
      },
      () => (this.canvas.style.cursor = ""),
    )
  }

  dragMove(e) {
    const start = [e.clientX, e.clientY]
    const origin = this.selected().map((l) => [l.id, num(l.vars?.["--at-x"]), num(l.vars?.["--at-y"])])
    const bounds = this.bounds(this.selected())
    this.snapTargets = this.targets(new Set(this.selection))
    let moved = false
    this.track(
      e,
      (ev) => {
        const dx = (ev.clientX - start[0]) / this.zoom
        const dy = (ev.clientY - start[1]) / this.zoom
        if (!moved && Math.hypot(dx * this.zoom, dy * this.zoom) < 3) return
        moved = true
        this.begin()
        // Snap the selection's bounds (alt: no snapping).
        const [sx, sy, guides] = ev.altKey ? [0, 0, []] : this.snap(bounds, dx, dy)
        for (const [id, x, y] of origin) {
          const l = this.layer(id)
          if (!l) continue
          l.vars ??= {}
          l.vars["--at-x"] = px(x + dx + sx)
          l.vars["--at-y"] = px(y + dy + sy)
        }
        this.sync()
        this.drawGuides(guides)
        this.notify()
      },
      () => {
        this.drawGuides([])
        this.end()
      },
    )
  }

  // The axis-aligned bounds of layers (rotation included), frame coordinates.
  bounds(layers) {
    let x0 = Infinity
    let y0 = Infinity
    let x1 = -Infinity
    let y1 = -Infinity
    for (const l of layers) {
      const b = this.box(l)
      const c = [b.x + b.w / 2, b.y + b.h / 2]
      for (const p of [[-1, -1], [1, -1], [1, 1], [-1, 1]]) {
        const [rx, ry] = rotate([(p[0] * b.w) / 2, (p[1] * b.h) / 2], b.r)
        x0 = Math.min(x0, c[0] + rx)
        x1 = Math.max(x1, c[0] + rx)
        y0 = Math.min(y0, c[1] + ry)
        y1 = Math.max(y1, c[1] + ry)
      }
    }
    return { x0, y0, x1, y1 }
  }

  // Lines to snap to: the frame's edges and center, and every other
  // visible layer's edges and center.
  targets(exclude) {
    const xs = [0, this.doc.width / 2, this.doc.width]
    const ys = [0, this.doc.height / 2, this.doc.height]
    for (const l of this.doc.layers) {
      if (exclude.has(l.id) || l.hidden || !this.onPage(l)) continue
      const b = this.bounds([l])
      xs.push(b.x0, (b.x0 + b.x1) / 2, b.x1)
      ys.push(b.y0, (b.y0 + b.y1) / 2, b.y1)
    }
    return { xs, ys }
  }

  // The correction snapping bounds moved by (dx, dy) to the nearest
  // targets within 6 screen pixels, and the guides to draw.
  snap(b, dx, dy) {
    const t = this.snapTargets
    const limit = 6 / this.zoom
    const best = (edges, lines) => {
      let d = Infinity
      for (const e of edges) for (const l of lines) if (Math.abs(l - e) < Math.abs(d)) d = l - e
      return Math.abs(d) <= limit ? d : 0
    }
    const ex = [b.x0 + dx, (b.x0 + b.x1) / 2 + dx, b.x1 + dx]
    const ey = [b.y0 + dy, (b.y0 + b.y1) / 2 + dy, b.y1 + dy]
    const sx = best(ex, t.xs)
    const sy = best(ey, t.ys)
    const guides = []
    const eps = 0.01
    if (sx || ex.some((e) => t.xs.some((l) => Math.abs(l - e) < eps))) {
      for (const e of ex) for (const l of t.xs) if (Math.abs(l - (e + sx)) < eps) guides.push(["x", l])
    }
    if (sy || ey.some((e) => t.ys.some((l) => Math.abs(l - e) < eps))) {
      for (const e of ey) for (const l of t.ys) if (Math.abs(l - (e + sy)) < eps) guides.push(["y", l])
    }
    return [sx, sy, guides]
  }

  drawGuides(guides) {
    for (const g of this.overlay.querySelectorAll("[data-atelier-guide]")) g.remove()
    for (const [axis, v] of guides) {
      const g = document.createElement("div")
      g.dataset.atelierGuide = axis
      const pos = (axis === "x" ? this.pan[0] : this.pan[1]) + v * this.zoom
      Object.assign(g.style, {
        position: "absolute",
        background: "#ec4899",
        pointerEvents: "none",
        ...(axis === "x" ? { left: pos + "px", top: 0, bottom: 0, width: "1px" } : { top: pos + "px", left: 0, right: 0, height: "1px" }),
      })
      this.overlay.append(g)
    }
  }

  // Dragging on empty canvas selects the layers the rectangle touches.
  dragMarquee(e) {
    const r = this.canvas.getBoundingClientRect()
    const [fx, fy] = this.toFrame(e.clientX, e.clientY)
    const box = document.createElement("div")
    box.dataset.atelierMarquee = ""
    Object.assign(box.style, { position: "absolute", border: "1px solid #3b82f6", background: "rgb(59 130 246 / 0.08)", pointerEvents: "none" })
    const base = e.shiftKey ? [...this.selection] : []
    let shown = false
    this.track(
      e,
      (ev) => {
        const [gx, gy] = this.toFrame(ev.clientX, ev.clientY)
        if (!shown) {
          if (Math.hypot(ev.clientX - e.clientX, ev.clientY - e.clientY) < 3) return
          shown = true
          this.overlay.append(box)
        }
        const x0 = Math.min(fx, gx)
        const y0 = Math.min(fy, gy)
        const x1 = Math.max(fx, gx)
        const y1 = Math.max(fy, gy)
        Object.assign(box.style, {
          left: this.pan[0] + x0 * this.zoom + "px",
          top: this.pan[1] + y0 * this.zoom + "px",
          width: (x1 - x0) * this.zoom + "px",
          height: (y1 - y0) * this.zoom + "px",
        })
        const hit = this.doc.layers
          .filter((l) => !l.parent && !l.hidden && !l.locked && this.onPage(l))
          .filter((l) => {
            const b = this.bounds([l])
            return b.x0 < x1 && b.x1 > x0 && b.y0 < y1 && b.y1 > y0
          })
          .map((l) => l.id)
        this.selection = [...new Set([...base, ...hit])]
        this.drawOverlay()
        this.overlay.append(box)
        this.notify()
      },
      () => {
        box.remove()
        if (!shown && !e.shiftKey) this.select([])
      },
    )
  }

  // Aligns the selection's bounds edges: one layer to the frame, several
  // to their shared bounds. Positions are relative to each layer's frame.
  align(edge) {
    const layers = this.selected()
    if (!layers.length) return
    const ref = layers.length === 1 ? { x0: 0, y0: 0, x1: this.doc.width, y1: this.doc.height } : this.bounds(layers)
    this.commit((d) => {
      for (const sl of layers) {
        const l = d.layers.find((x) => x.id === sl.id)
        const b = this.bounds([sl])
        let dx = 0
        let dy = 0
        if (edge === "left") dx = ref.x0 - b.x0
        if (edge === "center") dx = (ref.x0 + ref.x1) / 2 - (b.x0 + b.x1) / 2
        if (edge === "right") dx = ref.x1 - b.x1
        if (edge === "top") dy = ref.y0 - b.y0
        if (edge === "middle") dy = (ref.y0 + ref.y1) / 2 - (b.y0 + b.y1) / 2
        if (edge === "bottom") dy = ref.y1 - b.y1
        l.vars ??= {}
        l.vars["--at-x"] = px(num(l.vars["--at-x"]) + dx)
        l.vars["--at-y"] = px(num(l.vars["--at-y"]) + dy)
      }
    })
  }

  // Moves the selected layers among their siblings: +1 forward, -1
  // backward, Infinity to the front, -Infinity to the back.
  reorder(step) {
    const ids = new Set(this.selection)
    if (!ids.size) return
    this.commit((d) => {
      const groups = new Map()
      for (const l of d.layers) {
        const k = l.parent ?? ""
        if (!groups.has(k)) groups.set(k, [])
        groups.get(k).push(l)
      }
      const out = []
      for (const [, sibs] of groups) {
        let arr = [...sibs]
        if (step === Infinity) arr = [...arr.filter((l) => !ids.has(l.id)), ...arr.filter((l) => ids.has(l.id))]
        else if (step === -Infinity) arr = [...arr.filter((l) => ids.has(l.id)), ...arr.filter((l) => !ids.has(l.id))]
        else if (step > 0) {
          for (let i = arr.length - 2; i >= 0; i--) {
            if (ids.has(arr[i].id) && !ids.has(arr[i + 1].id)) [arr[i], arr[i + 1]] = [arr[i + 1], arr[i]]
          }
        } else {
          for (let i = 1; i < arr.length; i++) {
            if (ids.has(arr[i].id) && !ids.has(arr[i - 1].id)) [arr[i], arr[i - 1]] = [arr[i - 1], arr[i]]
          }
        }
        out.push(...arr)
      }
      d.layers = out
    })
  }

  // Dropped image files become image layers: uploaded to
  // config.uploadURL (which answers {"url": ...}), or inlined.
  async dropFiles(files, clientX, clientY) {
    const [fx, fy] = this.toFrame(clientX, clientY)
    for (const file of files) {
      if (!file.type.startsWith("image/")) continue
      let url
      try {
        if (this.config.uploadURL) {
          const body = new FormData()
          body.append("file", file)
          const res = await fetch(this.config.uploadURL, { method: "POST", body, credentials: "same-origin" })
          if (!res.ok) throw new Error("upload: " + res.status)
          url = this.resolve((await res.json()).url, this.config.uploadURL)
        } else {
          url = await new Promise((res, rej) => {
            const r = new FileReader()
            r.onload = () => res(r.result)
            r.onerror = rej
            r.readAsDataURL(file)
          })
        }
        const img = new Image()
        img.src = url
        await img.decode().catch(() => {})
        const max = Math.min(this.doc.width, this.doc.height) * 0.6
        const s = Math.min(1, max / Math.max(img.naturalWidth || max, img.naturalHeight || max))
        const w = (img.naturalWidth || max) * s
        const h = (img.naturalHeight || max) * s
        this.addLayer("image", {
          name: file.name.replace(/\.[^.]+$/, ""),
          value: url,
          vars: { "--at-width": px(w), "--at-height": px(h), "--at-x": px(fx - w / 2), "--at-y": px(fy - h / 2), "--at-fit": "cover" },
        })
      } catch (err) {
        console.error("[atelier] drop:", err)
      }
    }
  }

  dragResize(e, [hx, hy]) {
    const l = this.selected()[0]
    if (!l) return
    const b = this.box(l)
    const x0 = num(l.vars?.["--at-x"])
    const y0 = num(l.vars?.["--at-y"])
    const start = [e.clientX, e.clientY]
    const ratio = b.w / (b.h || 1)
    this.snapTargets = this.targets(new Set([l.id]))
    this.track(
      e,
      (ev) => {
        this.begin()
        // The pointer delta in the layer's own (rotated) axes.
        const [dx, dy] = rotate([(ev.clientX - start[0]) / this.zoom, (ev.clientY - start[1]) / this.zoom], -b.r)
        let w = hx ? Math.max(1, b.w + hx * dx) : b.w
        let h = hy ? Math.max(1, b.h + hy * dy) : b.h
        // Unrotated layers: the dragged edges snap (alt: no snapping).
        let guides = []
        if (!b.r && !ev.altKey) {
          const limit = 6 / this.zoom
          const near = (v, lines) => lines.reduce((best, l) => (Math.abs(l - v) < Math.abs(best - v) ? l : best), Infinity)
          if (hx) {
            const edge = hx > 0 ? b.x + w : b.x + b.w - w
            const l = near(edge, this.snapTargets.xs)
            if (Math.abs(l - edge) <= limit) {
              w = Math.max(1, hx > 0 ? l - b.x : b.x + b.w - l)
              guides.push(["x", l])
            }
          }
          if (hy) {
            const edge = hy > 0 ? b.y + h : b.y + b.h - h
            const l = near(edge, this.snapTargets.ys)
            if (Math.abs(l - edge) <= limit) {
              h = Math.max(1, hy > 0 ? l - b.y : b.y + b.h - l)
              guides.push(["y", l])
            }
          }
        }
        if (ev.shiftKey && hx && hy) {
          // Keep the aspect ratio.
          if (w / h > ratio) h = w / ratio
          else w = h * ratio
        }
        // The opposite anchor stays in place.
        const a = [(-hx * b.w) / 2, (-hy * b.h) / 2]
        const a2 = [(-hx * w) / 2, (-hy * h) / 2]
        const [sx, sy] = rotate([a[0] - a2[0], a[1] - a2[1]], b.r)
        const cx = x0 + b.w / 2 + sx
        const cy = y0 + b.h / 2 + sy
        l.vars ??= {}
        l.vars["--at-width"] = px(w)
        l.vars["--at-height"] = px(h)
        l.vars["--at-x"] = px(cx - w / 2)
        l.vars["--at-y"] = px(cy - h / 2)
        this.sync()
        this.drawGuides(guides)
        this.notify()
      },
      () => {
        this.drawGuides([])
        this.end()
      },
    )
  }

  dragRotate(e) {
    const l = this.selected()[0]
    if (!l) return
    const b = this.box(l)
    const r = this.canvas.getBoundingClientRect()
    const cx = r.left + this.pan[0] + (b.x + b.w / 2) * this.zoom
    const cy = r.top + this.pan[1] + (b.y + b.h / 2) * this.zoom
    this.track(
      e,
      (ev) => {
        this.begin()
        let deg = (Math.atan2(ev.clientY - cy, ev.clientX - cx) * 180) / Math.PI + 90
        if (ev.shiftKey) deg = Math.round(deg / 15) * 15
        deg = ((deg % 360) + 360) % 360
        if (deg > 180) deg -= 360
        l.vars ??= {}
        if (Math.abs(deg) < 0.01) delete l.vars["--at-rotate"]
        else l.vars["--at-rotate"] = round(deg) + "deg"
        this.sync()
        this.notify()
      },
      () => this.end(),
    )
  }

  onDoubleClick(e) {
    if (this.readOnly) return
    // Pointer capture (dragMove) retargets the click to the canvas: hit-test
    // at the pointer instead.
    const l = this.hit(document.elementFromPoint(e.clientX, e.clientY) ?? e.target)
    if (!l || l.type !== "text") return
    const el = this.element(l.id)?.querySelector('[data-slot="atelier-text"]')
    if (!el) return
    this.select([l.id])
    this.editing = el
    el.contentEditable = "plaintext-only"
    el.focus()
    const range = document.createRange()
    range.selectNodeContents(el)
    const sel = getSelection()
    sel.removeAllRanges()
    sel.addRange(range)
    this.drawOverlay()
    const onInput = () => {
      this.begin()
      l.value = el.textContent
      this.notify()
    }
    el.addEventListener("input", onInput)
    el.addEventListener("blur", () => this.stopEditing(), { once: true })
    this.stopEditingCleanup = () => el.removeEventListener("input", onInput)
  }

  stopEditing() {
    const el = this.editing
    if (!el) return
    // Cleared first: leaving edit mode blurs the element, whose blur
    // listener calls stopEditing again.
    this.editing = null
    el.removeAttribute("contenteditable")
    this.stopEditingCleanup?.()
    this.end()
    this.drawOverlay()
  }

  // Shortcuts apply while focus is in the editor and not in a field.
  onKey(e) {
    if (!this.root.isConnected) return
    const t = e.target
    const inField = t instanceof HTMLInputElement || t instanceof HTMLTextAreaElement || t?.isContentEditable
    if (e.key === " " && !inField) this.spaceDown = e.type === "keydown"
    if (inField) {
      if (e.key === "Escape" && this.editing) {
        e.preventDefault()
        this.stopEditing()
      }
      return
    }
    if (!this.root.contains(document.activeElement) && document.activeElement !== document.body) return
    const mod = isMac ? e.metaKey : e.ctrlKey
    const k = e.key.toLowerCase()
    const run = (fn) => {
      e.preventDefault()
      fn()
    }
    // Viewing shortcuts work in read-only editors too.
    const viewing = new Set(["zoom-in", "zoom-out", "zoom-fit"])
    for (const [name, combos] of Object.entries(this.keymap())) {
      if (!combos || !combos.split(",").some((c) => matchKey(e, c.trim()))) continue
      if (this.readOnly && !viewing.has(name)) return
      return run(() => this.command(name, document.activeElement ?? this.root))
    }
    if (this.readOnly || mod || e.altKey) return
    const arrows = { arrowleft: [-1, 0], arrowright: [1, 0], arrowup: [0, -1], arrowdown: [0, 1] }
    if (arrows[k] && this.selection.length) {
      const [dx, dy] = arrows[k].map((v) => v * (e.shiftKey ? 10 : 1))
      return run(() =>
        this.commit((d) => {
          for (const id of this.selection) {
            const l = d.layers.find((x) => x.id === id)
            l.vars ??= {}
            l.vars["--at-x"] = px(num(l.vars["--at-x"]) + dx)
            l.vars["--at-y"] = px(num(l.vars["--at-y"]) + dy)
          }
        }),
      )
    }

  }

  // Shortcuts: command -> comma-separated key combos ("mod+shift+z"),
  // config.keys overriding (an empty combo disables one).
  keymap() {
    const map = {
      undo: "mod+z",
      redo: "mod+shift+z, mod+y",
      "zoom-in": "mod+=, mod+shift+=",
      "zoom-out": "mod+-",
      "zoom-fit": "mod+0",
      duplicate: "mod+d",
      "select-all": "mod+a",
      group: "mod+g",
      ungroup: "mod+shift+g",
      copy: "mod+c",
      cut: "mod+x",
      forward: "mod+]",
      backward: "mod+[",
      front: "mod+shift+]",
      back: "mod+shift+[",
      deselect: "escape",
      delete: "delete, backspace",
    }
    for (const t of this.types.values()) if (t.key) map["add:" + t.type] = t.key
    return { ...map, ...(this.config.keys ?? {}) }
  }

  // Commands from data-atelier-command buttons (toolbar, menus) and
  // shortcuts.
  command(name, el) {
    if (name.startsWith("add:")) return this.readOnly ? undefined : this.addLayer(name.slice(4))
    if (name === "select-all" && !this.readOnly) {
      return this.select(this.doc.layers.filter((l) => !l.parent && this.onPage(l)).map((l) => l.id))
    }
    // A read-only editor only views: zoom, pages and exports.
    const viewing = ["zoom-in", "zoom-out", "zoom-fit", "zoom", "page", "export-png", "export-pdf", "export-json", "shortcuts", "copy"]
    if (this.readOnly && !viewing.includes(name)) return
    switch (name) {
      case "undo":
        return this.undo()
      case "redo":
        return this.redo()
      case "zoom-in":
        return this.zoomTo(this.zoom * 1.25)
      case "zoom-out":
        return this.zoomTo(this.zoom / 1.25)
      case "zoom-fit":
        return this.fit()
      case "zoom":
        return this.zoomTo(num(el.dataset.atelierZoom, 100) / 100)
      case "add":
        return this.addLayer(el.dataset.atelierType)
      case "delete":
        return this.removeSelected()
      case "duplicate":
        return this.duplicateSelected()
      case "deselect":
        return this.select([])
      case "page":
        return this.showPage(el.dataset.atelierPage)
      case "add-page":
        return this.addPage()
      case "delete-page":
        return this.deletePage()
      case "group":
        return this.group()
      case "ungroup":
        return this.ungroup()
      case "toggle-panel": {
        for (const panel of this.root.querySelectorAll('[data-slot="atelier-panel"]')) {
          const open = panel.dataset.side === el.dataset.atelierSide && panel.dataset.open !== "true"
          panel.dataset.open = String(open)
        }
        return
      }
      case "generate":
        return this.generate(el)
      case "edit-image":
        return this.editImage(el)
      case "insert":
        return this.insertTemplate(JSON.parse(el.dataset.atelierTemplate || "[]"))
      case "align":
        return this.align(el.dataset.atelierEdge)
      case "cut":
        return this.cut()
      case "copy":
        return this.copy()
      case "paste":
        return this.paste()
      case "forward":
        return this.reorder(1)
      case "backward":
        return this.reorder(-1)
      case "front":
        return this.reorder(Infinity)
      case "back":
        return this.reorder(-Infinity)
      case "export-png":
        return this.exportPNG()
      case "export-pdf":
        return this.exportPDF()
      case "export-json":
        return this.exportJSON()
      case "shortcuts":
        return this.root.querySelector("dialog[data-atelier-shortcuts]")?.showModal()
      case "clear":
        return this.setVars(Object.fromEntries((el.dataset.atelierProps ?? "").split(/\s+/).filter(Boolean).map((p) => [p, ""])))
      case "rotate": {
        const l = this.selected()[0]
        if (!l) return
        let deg = (num(l.vars?.["--at-rotate"]) + num(el.dataset.atelierDegrees)) % 360
        if (deg > 180) deg -= 360
        if (deg <= -180) deg += 360
        return this.setVars({ "--at-rotate": deg ? deg + "deg" : "" })
      }
      case "flip": {
        const prop = el.dataset.atelierAxis === "y" ? "--at-scale-y" : "--at-scale-x"
        const l = this.selected()[0]
        if (!l) return
        return this.setVars({ [prop]: num(l.vars?.[prop], 1) < 0 ? "" : "-1" })
      }
    }
  }

  // --- actions ----------------------------------------------------------

  // The value a control shows for a property of a layer: the property
  // without its unit, or the effective value for the box properties.
  shown(l, prop, unit) {
    const k = unit === "px" ? this.perUnit() : 1
    const v = l.vars?.[prop]
    if (v != null && v !== "") {
      if (unit && String(v).endsWith(unit)) return String(round(num(v) / k))
      return String(v)
    }
    if (prop in DEFAULTS) return DEFAULTS[prop]
    const b = this.box(l)
    switch (prop) {
      case "--at-width":
        return String(round(b.w / k))
      case "--at-height":
        return String(round(b.h / k))
      case "--at-x":
      case "--at-y":
      case "--at-rotate":
        return "0"
    }
    return ""
  }

  bindControls() {
    const write = (el, isLive) => {
      const prop = el.dataset.atelierVar
      const unit = el.dataset.atelierUnit ?? ""
      let v = el.value.trim()
      if (unit && v !== "") {
        const n = parseFloat(v)
        if (!Number.isFinite(n)) return
        v = round(unit === "px" ? n * this.perUnit() : n) + unit
      }
      this.setVars({ [prop]: v }, { live: isLive })
    }
    const onInput = (e) => {
      const el = e.target
      if (!this.root.contains(el) || !this.selection.length) return
      if (el.dataset.atelierVar && !el.dataset.atelierSet) {
        if (el.tagName === "SELECT") return
        write(el, true)
      } else if (el.dataset.atelierValue !== undefined) {
        this.setValue(el.value, { live: true })
        // Typing becomes one history entry (and a save) once it pauses.
        clearTimeout(this.typingTimer)
        this.typingTimer = setTimeout(() => this.end(), 600)
      }
    }
    const onChange = (e) => {
      const el = e.target
      if (!this.root.contains(el)) return
      if (el.dataset.atelierPageSelect !== undefined) return this.showPage(el.value)
      if (el.dataset.atelierDocUnit !== undefined) {
        return this.commit((d) => {
          if (el.value === "px") delete d.unit
          else d.unit = el.value
        })
      }
      if (el.dataset.atelierDoc === "dpi") {
        const dpi = Math.max(1, Math.min(10000, num(el.value, 96)))
        return this.commit((d) => {
          if (dpi === 96) delete d.dpi
          else d.dpi = dpi
        })
      }
      if (el.dataset.atelierDoc || el.dataset.atelierDocPreset !== undefined) {
        let [w, h] = [this.doc.width, this.doc.height]
        if (el.dataset.atelierDoc === "width") w = num(el.value, w / this.perUnit()) * this.perUnit()
        else if (el.dataset.atelierDoc === "height") h = num(el.value, h / this.perUnit()) * this.perUnit()
        else if (el.value) [w, h] = el.value.split("x").map(Number)
        w = Math.max(1, Math.min(100000, Math.round(w)))
        h = Math.max(1, Math.min(100000, Math.round(h)))
        if (w !== this.doc.width || h !== this.doc.height) {
          this.commit((d) => {
            d.width = w
            d.height = h
          })
          this.fit()
        }
        return
      }
      if (!this.selection.length) return
      if (el.tagName === "SELECT" && el.dataset.atelierVar) write(el, false)
      else if (el.dataset.atelierVar || el.dataset.atelierValue !== undefined) this.end()
    }
    // Arrow keys step number fields (shift: 10).
    const onKeyDown = (e) => {
      const el = e.target
      if (!el.dataset?.atelierVar || !el.dataset.atelierUnit || el.type === "range") return
      if (e.key === "Enter") return el.blur()
      if (e.key !== "ArrowUp" && e.key !== "ArrowDown") return
      e.preventDefault()
      const step = (e.shiftKey ? 10 : 1) * (e.key === "ArrowUp" ? 1 : -1)
      el.value = String(round(num(el.value) + step))
      write(el, true)
      this.end()
    }
    const onClick = (e) => {
      const el = e.target.closest?.("[data-atelier-set]")
      if (!el || !this.root.contains(el) || !this.selection.length) return
      this.setVars({ [el.dataset.atelierVar]: el.dataset.atelierSet })
    }
    for (const [type, fn] of [["input", onInput], ["change", onChange], ["keydown", onKeyDown], ["click", onClick]]) {
      this.root.addEventListener(type, fn)
      this.cleanups.push(() => this.root.removeEventListener(type, fn))
    }
    this.cleanups.push(this.subscribe(() => this.updateControls()))
  }

  updateControls() {
    const l = this.readOnly ? null : this.selected()[0]
    const types = new Set(this.selected().map((x) => x.type))
    for (const pane of this.root.querySelectorAll('[data-slot="atelier-pane"][data-for-types]')) {
      const want = JSON.parse(pane.dataset.forTypes)
      pane.dataset.hidden = String(!want.some((t) => types.has(t)))
    }
    for (const el of this.root.querySelectorAll("[data-atelier-var]")) {
      const prop = el.dataset.atelierVar
      if (el.dataset.atelierSet !== undefined) {
        el.dataset.active = String(!!l && (l.vars?.[prop] ?? "") === el.dataset.atelierSet)
        el.disabled = !l
        continue
      }
      el.disabled = !l
      if (el === document.activeElement && el.type !== "range") continue
      const v = l ? this.shown(l, prop, el.dataset.atelierUnit) : ""
      if (el.type === "color") {
        if (/^#[0-9a-f]{6}$/i.test(v)) el.value = v
      } else if (el.value !== v) el.value = v
    }
    for (const sw of this.root.querySelectorAll("[data-atelier-swatch]")) {
      sw.style.background = (l && l.vars?.[sw.dataset.atelierSwatch]) || "transparent"
    }
    for (const el of this.root.querySelectorAll("[data-atelier-doc]")) {
      if (el === document.activeElement) continue
      const f = el.dataset.atelierDoc
      el.value = f === "dpi" ? String(this.doc.dpi || 96) : String(round((this.doc[f] ?? 0) / this.perUnit()))
    }
    for (const el of this.root.querySelectorAll("select[data-atelier-doc-unit]")) el.value = this.unitName()
    for (const el of this.root.querySelectorAll("[data-atelier-doc-preset]")) {
      const v = `${this.doc.width}x${this.doc.height}`
      el.value = [...el.options].some((o) => o.value === v) ? v : ""
    }
    for (const el of this.root.querySelectorAll("[data-atelier-value]")) {
      el.disabled = !l
      if (el !== document.activeElement && el.value !== (l?.value ?? "")) el.value = l?.value ?? ""
    }
  }
}

define("atelier", {
  selector: '[data-slot="atelier"]',
  mount(root) {
    const editor = new Editor(root)
    return editor.mount()
  },
})
