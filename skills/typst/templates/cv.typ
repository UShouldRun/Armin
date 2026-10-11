#set page(paper: "a4", margin: (x: 30pt, y: 20pt))
#set text(size: 7.75pt)
#set par(justify: true, leading: 0.45em)

#let section(title, content) = {
  text(size: 10pt, weight: "bold", upper(title))
  v(-6pt)
  line(length: 100%, stroke: 0.5pt)
  v(-2pt)
  content
}

#let entry(
  title: none,
  company: none,
  date: none,
  location: none,
  description: none,
) = {
  if title != none {
    text(weight: "bold", title)
  }
  if company != none or date != none or location != none {
    [ ]
    if company != none {
      text(style: "italic", company)
    }
    if date != none {
      h(1fr)
      text(date)
    }
    if location != none {
      [ | ]
      text(location)
    }
  }
  linebreak()
  if description != none {
    description
  }
}

#block(width: 100%, breakable: false)[
  #align(center)[
    #text(size: 15pt, weight: "bold")[Full Name]
    #v(0.1pt)
    #text(size: 8.5pt)[
      name\@example.com | #link("https://example.com")[example.com] | #link("https://github.com/user")[github.com/user]
    ]
  ]
]

#section[EXPERIENCE][
  #entry(
    title: "Role - Project",
    company: "Organization",
    date: "MM/YYYY – now",
    location: "City, Country",
    description: [
      - What you did, how, and the measured result.
    ]
  )
]

#section[EDUCATION][
  #entry(
    title: "Degree",
    company: "School",
    date: "MM/YYYY – MM/YYYY",
    location: "City, Country",
    description: [
      - Relevant courses or distinctions.
    ]
  )
]
