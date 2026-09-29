package config

import "github.com/context-labs/whip/internal/session"

// ResolveBrowserDriver resolves omission to the retained Rod default.
func ResolveBrowserDriver(value string) (string, error) {
	switch value {
	case "", "rod":
		return "rod", nil
	case "chromedp":
		return value, nil
	default:
		return "", session.ErrInvalid
	}
}
