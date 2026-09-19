// Package docs embeds openapi.yaml so internal/httpapi can serve it live
// (GET /docs) without duplicating the file — this directory otherwise
// holds only the planning-doc deliverables (markdown, docx), not Go
// source; this one small file is the exception, kept here specifically so
// there's exactly one openapi.yaml, not a copy for tooling and a copy for
// the live route to drift apart.
package docs

import _ "embed"

//go:embed openapi.yaml
var OpenAPISpec []byte
