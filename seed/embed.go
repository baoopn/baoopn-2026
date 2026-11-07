// Package seed embeds the first-run content: the profile, the v3 blog series
// and the images they reference (paths under assets/ mirror v3's public/).
package seed

import "embed"

// FS holds profile.json, blog/<series>/{metadata.json,page-N.md} and assets/.
//
//go:embed profile.json blog assets
var FS embed.FS
