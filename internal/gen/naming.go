package main

import "strings"

// typeNameFixes corrects the handful of Bot API type names whose scraped
// spelling isn't idiomatic Go (verified exhaustively against api.json: these
// are the only three names containing "Id" or "Url" as of API 10.1).
var typeNameFixes = map[string]string{
	"LoginUrl":    "LoginURL",
	"MessageId":   "MessageID",
	"RichTextUrl": "RichTextURL",
}

// goTypeName resolves a Bot API type/union name to the Go identifier it
// should be referenced by — applying the idiomatic-name fixes above so
// generated code and the hand-written types it fixed for (LoginURL) agree.
func goTypeName(apiName string) string {
	if fixed, ok := typeNameFixes[apiName]; ok {
		return fixed
	}
	return apiName
}

// fieldName converts a snake_case JSON field/param name to an exported Go
// identifier, normalizing "id" -> "ID" and "url" -> "URL" per-word (so
// "message_id" -> "MessageID", "web_app_url" -> "WebAppURL") — the same
// convention applied to type names in typeNameFixes, kept as a per-word rule
// here since field names arrive as separate snake_case components rather
// than one scraped CamelCase blob.
func fieldName(jsonName string) string {
	parts := strings.Split(jsonName, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		switch strings.ToLower(p) {
		case "id":
			b.WriteString("ID")
		case "url":
			b.WriteString("URL")
		default:
			b.WriteString(strings.ToUpper(p[:1]))
			b.WriteString(p[1:])
		}
	}
	return b.String()
}
