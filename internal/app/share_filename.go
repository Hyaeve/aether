package app

import "strings"

// Keep display text in STRM bodies. Only URL delimiters and literal percent
// signs must be escaped so the HTTP client can reconstruct the exact name.
var shareFilenameEscaper = strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23")

func shareFilenamePath(name string) string {
	encoded := shareFilenameEscaper.Replace(name)
	var out strings.Builder
	const digits = "0123456789ABCDEF"
	for i := 0; i < len(encoded); i++ {
		c := encoded[i]
		if c < 32 || c == 127 || c == ' ' && i == len(encoded)-1 {
			out.WriteByte('%')
			out.WriteByte(digits[c>>4])
			out.WriteByte(digits[c&15])
		} else {
			out.WriteByte(c)
		}
	}
	return out.String()
}
