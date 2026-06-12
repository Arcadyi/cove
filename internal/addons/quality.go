package addons

import "strings"

var qualityOrder = []string{"4k dv", "4k hdr", "4k", "1080p", "720p", "480p", "ts", "cam"}

func inferQuality(s Stream) string {
	parts := strings.SplitN(s.Name, "\n", 3)
	if len(parts) > 1 {
		line := strings.ToLower(strings.TrimSpace(parts[1]))
		for _, q := range qualityOrder {
			if strings.Contains(line, q) {
				return q
			}
		}
	}
	text := strings.ToLower(s.Name + " " + s.Title)
	switch {
	case strings.Contains(text, "dolby vision"), strings.Contains(text, "4k dv"):
		return "4k dv"
	case strings.Contains(text, "hdr"):
		return "4k hdr"
	case strings.Contains(text, "2160"), strings.Contains(text, "4k"):
		return "4k"
	case strings.Contains(text, "1080"):
		return "1080p"
	case strings.Contains(text, "720"):
		return "720p"
	case strings.Contains(text, "480"):
		return "480p"
	case strings.Contains(text, "telesync"), strings.Contains(text, "ts "), strings.Contains(text, "[ts]"):
		return "ts"
	case strings.Contains(text, "hdcam"), strings.Contains(text, "cam"):
		return "cam"
	}
	return ""
}

func GetMaxQuality(streams []Stream) string {
	found := make(map[string]bool)
	for _, s := range streams {
		if q := inferQuality(s); q != "" {
			found[q] = true
		}
	}
	for _, q := range qualityOrder {
		if found[q] {
			return q
		}
	}
	return ""
}
