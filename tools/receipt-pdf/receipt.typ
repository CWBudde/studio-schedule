// The receipt of a pass sale, or its cancellation (ADR 0012).
//
// All text, labels, amounts and dates come preformatted in the school's
// language from /data.json (internal/service/receipt_pdf.go); the template only
// lays them out. An optional logo is /logo.png or /logo.jpg.
#let d = json("/data.json")
// A list of the data, empty when absent or null.
#let items(key) = {
  let v = d.at(key, default: none)
  if v == none { () } else { v }
}
#let issuerLines = d.issuer.at("lines", default: none)
#let issuerLines = if issuerLines == none { () } else { issuerLines }

#let muted = luma(90)
#let rule = 0.5pt + luma(150)

#set document(
  title: d.title,
  author: d.issuer.name,
  date: datetime(year: d.date.year, month: d.date.month, day: d.date.day),
)
#set text(font: "Libertinus Serif", lang: d.lang, size: 10.5pt, number-type: "lining")
#set par(justify: false, leading: 0.6em)
#set page(
  paper: "a4",
  margin: (top: 2cm, bottom: 2.8cm, left: 2.2cm, right: 2cm),
  footer: context [
    #set text(size: 8pt, fill: muted)
    #line(length: 100%, stroke: rule)
    #v(-0.3em)
    #grid(
      columns: (1fr, auto),
      gutter: 1em,
      items("footer").join([ · ]),
      if counter(page).final().first() > 1 {
        counter(page).display("1 / 1", both: true)
      },
    )
  ],
)

// Issuer and logo.
#grid(
  columns: (1fr, auto),
  gutter: 1.5em,
  [
    #text(size: 14pt, weight: "bold", d.issuer.name) \
    #set text(size: 9.5pt, fill: muted)
    #issuerLines.join(linebreak())
  ],
  if d.at("logo", default: none) not in (none, "") {
    align(right + top, image("/logo." + d.logo, height: 2cm, width: 5cm, fit: "contain"))
  },
)

#v(1.6cm)

// Recipient and document facts.
#grid(
  columns: (1fr, auto),
  gutter: 2em,
  [
    #text(size: 8.5pt, fill: muted, d.recipientLabel) \
    #items("recipient").join(linebreak())
  ],
  table(
    columns: 2,
    stroke: none,
    inset: (x: 0pt, y: 2.5pt),
    column-gutter: 1em,
    align: (left, right),
    ..items("meta").map(m => (text(fill: muted, m.at(0)), m.at(1))).flatten(),
  ),
)

#v(1.2cm)
#text(size: 18pt, weight: "bold", d.heading)
#v(0.2em)
#if d.at("intro", default: none) not in (none, "") { d.intro }
#v(0.6em)

// The lines.
#let cols = if d.showVat { (auto, 1fr, auto, auto, auto, auto) } else { (auto, 1fr, auto, auto, auto) }
#let row(l) = if d.showVat {
  (l.pos, l.description, l.quantity, l.unit, l.vat, l.total)
} else {
  (l.pos, l.description, l.quantity, l.unit, l.total)
}
#let header = if d.showVat { d.columns } else { d.columns.slice(0, 4) + (d.columns.at(5),) }
#table(
  columns: cols,
  stroke: (x, y) => if y == 0 { (bottom: 0.8pt) } else { (bottom: rule) },
  inset: (x: 4pt, y: 6pt),
  align: (x, y) => if x == 1 { left } else if x == 0 { left } else { right },
  table.header(..header.map(h => text(weight: "bold", size: 9.5pt, h))),
  ..items("lines").map(row).flatten(),
)

// Net, VAT and the total.
#align(right, table(
  columns: 2,
  stroke: none,
  inset: (x: 4pt, y: 3pt),
  column-gutter: 2em,
  align: (left, right),
  ..items("summary").map(s => (text(fill: muted, s.at(0)), s.at(1))).flatten(),
  table.hline(stroke: 0.8pt),
  text(weight: "bold", d.total.at(0)), text(weight: "bold", d.total.at(1)),
))

#v(0.8cm)
#for n in items("notes") [
  #n \
]
