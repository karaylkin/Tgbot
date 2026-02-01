package models

// BookFormatOption describes a downloadable file format for a book.
type BookFormatOption struct {
	// Path is the trailing part of the download URL, e.g. "fb2", "epub", "fb2.zip".
	Path string

	// Label is a human-readable label extracted from the website, often includes size.
	Label string
}

// BookDetails is a best-effort set of details used by the Telegram UI.
type BookDetails struct {
	ID     string
	Title  string
	Author string

	// CoverPath is a relative or absolute URL to the cover image.
	CoverPath string

	Formats []BookFormatOption
}
