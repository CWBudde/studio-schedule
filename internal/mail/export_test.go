package mail

import "io/fs"

// TemplateFS exposes the embedded templates directory to the parity test.
func TemplateFS() fs.FS { return templateFS() }
