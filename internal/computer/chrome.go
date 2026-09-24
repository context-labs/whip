// chrome.go drives the user's already-open Chrome via its AppleScript
// dictionary — the flagship computer-use path (zero CDP setup, no
// remote-debugging port, no browser restart). Requires no permissions
// beyond Automation (granted once per app via the TCC prompt on first use).
//
// JS execution (ChromeJS) additionally requires Chrome's
// View → Developer → "Allow JavaScript from Apple Events" toggle; the error
// surfaces that requirement when it's off.

package computer

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ChromeTab is one tab in one Chrome window.
type ChromeTab struct {
	Window int    `json:"window"`
	Index  int    `json:"index"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

// ChromeActive returns the front window's active tab (URL + title).
func ChromeActive(automation Automation) (url, title string, err error) {
	out, err := automation.Tell("Google Chrome",
		`set theUrl to URL of active tab of front window`,
		`set theTitle to title of active tab of front window`,
		`return theUrl & "\n" & theTitle`)
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(out, "\n", 2)
	url = parts[0]
	if len(parts) > 1 {
		title = parts[1]
	}
	return url, title, nil
}

// ChromeTabs lists every tab of every window.
func ChromeTabs(automation Automation) ([]ChromeTab, error) {
	// Emit one line per tab: "winIdx|tabIdx|url|title" — parsed robustly
	// because titles can contain the field separator.
	out, err := automation.Tell("Google Chrome", `
set out to ""
set wCount to count windows
repeat with w from 1 to wCount
  set tCount to count tabs of window w
  repeat with t from 1 to tCount
    set theTab to tab t of window w
    set out to out & w & "￨" & t & "￨" & (URL of theTab) & "￨" & (title of theTab) & "
"
  end repeat
end repeat
return out`)
	if err != nil {
		return nil, err
	}
	var tabs []ChromeTab
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.SplitN(line, "￨", 4)
		if len(f) != 4 {
			continue
		}
		var w, i int
		_, _ = fmt.Sscanf(f[0], "%d", &w)
		_, _ = fmt.Sscanf(f[1], "%d", &i)
		tabs = append(tabs, ChromeTab{Window: w, Index: i, URL: f[2], Title: f[3]})
	}
	return tabs, nil
}

// ChromeGoto navigates the front window's active tab to url. The URL is
// SSRF-checked by the caller (the tool layer runs CheckURL first).
func ChromeGoto(url string, automation Automation) error {
	_, err := automation.Tell("Google Chrome",
		`set URL of active tab of front window to `+quote(url))
	return err
}

// ChromeNewTab opens url in a new tab of the front window (or a new window
// when Chrome has none).
func ChromeNewTab(url string, automation Automation) error {
	_, err := automation.Tell("Google Chrome",
		`if (count windows) is 0 then make new window`,
		`make new tab at end of tabs of front window with properties {URL:`+quote(url)+`}`,
		`activate`)
	return err
}

// ChromeActivateTab focuses window w, tab i (1-indexed, per ChromeTabs).
func ChromeActivateTab(window, index int, automation Automation) error {
	_, err := automation.Tell("Google Chrome",
		fmt.Sprintf(`set active tab index of window %d to %d`, window, index),
		fmt.Sprintf(`set index of window %d to 1`, window),
		`activate`)
	return err
}

// ChromeCloseTab closes window w, tab i.
func ChromeCloseTab(window, index int, automation Automation) error {
	_, err := automation.Tell("Google Chrome",
		fmt.Sprintf(`close tab %d of window %d`, index, window))
	return err
}

// ChromeBack steps history in the front window's active tab.
func ChromeBack(automation Automation) error {
	_, err := automation.Tell("Google Chrome", `tell active tab of front window to go back`)
	return err
}

// ChromeReload reloads the front window's active tab.
func ChromeReload(automation Automation) error {
	_, err := automation.Tell("Google Chrome", `tell active tab of front window to reload`)
	return err
}

// ErrJSFromAppleEvents surfaces the Chrome toggle requirement.
var ErrJSFromAppleEvents = errors.New("chrome's 'Allow JavaScript from Apple Events' is off — enable it in Chrome: View → Developer → Allow JavaScript from Apple Events")

// ChromeJS evaluates JavaScript in the front window's active tab and returns
// the result (AppleScript stringifies it). Requires the Chrome toggle; a
// refusal error is rewritten to ErrJSFromAppleEvents guidance.
func ChromeJS(js string, automation Automation) (string, error) {
	out, err := automation.Tell("Google Chrome",
		`tell active tab of front window to execute javascript `+quote(js))
	if err != nil && (strings.Contains(err.Error(), "execute javascript") || strings.Contains(err.Error(), "not allowed") || strings.Contains(err.Error(), "1743") || strings.Contains(err.Error(), "Allow JavaScript")) {
		return "", ErrJSFromAppleEvents
	}
	return out, err
}

// ChromeFindTab returns the first tab whose URL contains the substring.
func ChromeFindTab(urlPart string, automation Automation) (*ChromeTab, error) {
	tabs, err := ChromeTabs(automation)
	if err != nil {
		return nil, err
	}
	for _, t := range tabs {
		if strings.Contains(t.URL, urlPart) {
			return &t, nil
		}
	}
	return nil, nil //nolint:nilnil // nil tab = no match; callers handle "not found" as a nil tab with nil error
}

// ChromeState is the JSON shape the model gets from `chrome_state()`.
func ChromeState(automation Automation) (string, error) {
	url, title, err := ChromeActive(automation)
	if err != nil {
		return "", err
	}
	tabs, err := ChromeTabs(automation)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(map[string]any{
		"active": ChromeTab{URL: url, Title: title},
		"tabs":   tabs,
	})
	return string(data), nil
}
