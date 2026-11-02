// Package docs embeds the documents that the binary serves itself: the
// glossary, which the MCP server offers as the resource school://glossary
// (ADR 0022) so that an assistant uses the school's words.
package docs

import _ "embed" // the glossary

// Glossary is docs/glossary.md, the domain terms in Markdown.
//
//go:embed glossary.md
var Glossary string
