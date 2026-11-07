// Package seed embeds the first-run content: the profile (with its local
// images under assets/) and the seed posts imported as drafts.
package seed

import "embed"

// FS holds profile.json, assets/ and posts/<slug>/{post.json,part-N.md,img/}.
// posts/README.md keeps the pattern matching while no post folders exist.
//
//go:embed profile.json posts assets
var FS embed.FS
