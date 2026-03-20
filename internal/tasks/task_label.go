package tasks

import "strings"

func canonicalTaskLabel(tool string, action string, detail string) string {
	parts := []string{normalizeLabelPart(tool), normalizeLabelPart(action)}
	if detail != "" {
		parts = append(parts, normalizeLabelPart(detail))
	}
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, "-")
}

func normalizeLabelPart(value string) string {
	value = normalizeRelative(value)
	var builder strings.Builder
	lastHyphen := false
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
			lastHyphen = false
		case r == '.' || r == '_':
			builder.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen && builder.Len() > 0 {
				builder.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}
