package main

// LinkType categorises the kind of resource a link points to.
type LinkType int

//go:generate stringer -linecomment -type=LinkType
const (
	LinkTypeHyperlink LinkType = iota // hyperlink
	LinkTypeImage                     // image
	LinkTypeCSS                       // css
	LinkTypeScript                    // script
	LinkTypeVideo                     // video
)
