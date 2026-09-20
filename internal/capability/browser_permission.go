package capability

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// browserPermissionSummary renders only the broker-resolved resource envelope.
// It is presentation, not validation or authority; never display code, grant
// references, raw arguments, credentials, or transport addresses here.
func browserPermissionSummary(operation string, arguments json.RawMessage) string {
	const unresolved = "Desktop browser: unresolved or invalid resource summary. No resource approval can be inferred."
	switch operation {
	case "browser.open", "browser.attach", "browser.run", "browser.detach", "browser.allow_preview_port":
	default:
		return unresolved
	}
	var call BrowserCall
	var args struct {
		URL  string `json:"url"`
		Port int    `json:"port"`
	}
	if json.Unmarshal(arguments, &call) != nil || !strings.HasPrefix(strings.TrimSpace(string(call.Arguments)), "{") || json.Unmarshal(call.Arguments, &args) != nil {
		return unresolved
	}
	scope := call.Scope
	if scope.ProviderID == "" || scope.ProviderEpoch == "" || scope.TabID == "" ||
		scope.TabGeneration == "" || scope.ProfileID == "" ||
		(scope.AttachmentID == "") != (scope.AttachmentGeneration == "") {
		return unresolved
	}
	if operation != "browser.open" && operation != "browser.attach" && scope.AttachmentID == "" {
		return unresolved
	}
	if preview := scope.Preview; preview != nil {
		if preview.HostID == "" || preview.HostIdentity == "" || preview.ConnectionGeneration == "" ||
			preview.EnvironmentID == "" || (preview.Loopback != "127.0.0.1" && preview.Loopback != "::1") || len(preview.Ports) > 64 {
			return unresolved
		}
		previous := 0
		for _, port := range preview.Ports {
			if port <= previous || port > 65535 {
				return unresolved
			}
			previous = port
		}
	}
	if operation == "browser.allow_preview_port" && (scope.Preview == nil || args.Port < 1 || args.Port > 65535) {
		return unresolved
	}

	var summary strings.Builder
	fmt.Fprintf(&summary, "Agent control — %s\nProvider: %s; epoch: %s\nTab: %s; generation: %s\nProfile: %s\n",
		operation, browserIdentityLabel(scope.ProviderID), browserIdentityLabel(scope.ProviderEpoch),
		browserIdentityLabel(scope.TabID), browserIdentityLabel(scope.TabGeneration), browserIdentityLabel(scope.ProfileID))
	if scope.AttachmentID != "" {
		fmt.Fprintf(&summary, "Attachment: %s; generation: %s\n", browserIdentityLabel(scope.AttachmentID), browserIdentityLabel(scope.AttachmentGeneration))
	} else {
		summary.WriteString("Attachment: new agent-scoped control attachment\n")
	}
	if operation == "browser.open" {
		fmt.Fprintf(&summary, "Requested initial URL: %s (credentials, query, and fragment omitted)\n", browserURLLabel(args.URL))
	}
	summary.WriteString("Consent: Once for this request; no remembered rule.\n")
	if operation == "browser.detach" {
		summary.WriteString("Detaching ends agent control, not independent tab/environment network authority.\n")
	} else {
		summary.WriteString("The control attachment persists scoped to this agent until detach, revoke, or disconnect; it is not limited to one call.\n")
	}
	if preview := scope.Preview; preview != nil {
		fmt.Fprintf(&summary, "\nSSH preview network — independent lifetime\nOffered saved host ID: %s\nVerified runtime identity (not SSH fingerprint): %s\nConnection generation: %s\nEnvironment identity: %s\nLoopback target: %s\n",
			browserIdentityLabel(preview.HostID), browserIdentityLabel(preview.HostIdentity),
			browserIdentityLabel(preview.ConnectionGeneration), browserIdentityLabel(preview.EnvironmentID),
			browserIdentityLabel(preview.Loopback))
		switch operation {
		case "browser.open":
			// Resolve includes the initial URL port in this proposed scope; it
			// need not have been present in the provider's earlier host offer.
			fmt.Fprintf(&summary, "Proposed preview ports (not yet approved for this new tab): %v\n", preview.Ports)
			summary.WriteString("This approval also authorizes the described SSH preview-network scope, including the initial URL port, for the new tab.\n")
		case "browser.attach":
			// Attach copies the offered existing tab's scope without adding ports.
			fmt.Fprintf(&summary, "Offered existing-tab preview ports: %v\n", preview.Ports)
			summary.WriteString("This approval also authorizes the agent to use the described existing SSH preview-network scope on the offered tab; no additional ports are requested.\n")
		case "browser.allow_preview_port":
			fmt.Fprintf(&summary, "Currently approved preview ports: %v\n", preview.Ports)
			if slices.Contains(preview.Ports, args.Port) {
				fmt.Fprintf(&summary, "Requested preview port: %d (already in the approved scope; no expansion needed)\n", args.Port)
			} else {
				fmt.Fprintf(&summary, "Requested additional preview port: %d (proposed; not yet approved)\n", args.Port)
				summary.WriteString("This approval also authorizes adding the requested port to the described SSH preview-network scope.\n")
			}
		default:
			fmt.Fprintf(&summary, "Currently approved preview ports: %v\n", preview.Ports)
			summary.WriteString("This request does not expand the SSH preview-network scope.\n")
		}
		summary.WriteString("Preview-network authority has this tab/environment lifetime, independent of the agent-control attachment. Approval is Once for this request, but resulting network access is not limited to one call. Tab closure or environment disconnect ends it; detaching agent control does not.\n")
	} else {
		summary.WriteString("\nPreview network: none; this request grants no SSH preview-network authority.\n")
	}
	return strings.TrimSpace(summary.String())
}

func browserIdentityLabel(value string) string {
	// Resource fields must be opaque identities, not local profile/socket paths.
	if strings.ContainsAny(value, `/\`) {
		return `"[non-opaque identity redacted]"`
	}
	return browserQuotedLabel(value, 128)
}

func browserQuotedLabel(value string, limit int) string {
	if len(value) > limit {
		value = value[:limit]
		for !utf8.ValidString(value) && len(value) > 0 {
			value = value[:len(value)-1]
		}
		value += "... [truncated]"
	}
	// QuoteToASCII escapes controls, bidi markers, and line separators as data.
	return strconv.QuoteToASCII(value)
}

func browserURLLabel(raw string) string {
	if strings.IndexFunc(raw, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0 {
		return `"[invalid URL]"`
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.Opaque != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return `"[invalid or unsupported URL; local paths omitted]"`
	}
	parsed.User, parsed.RawQuery, parsed.Fragment, parsed.RawFragment = nil, "", "", ""
	parsed.ForceQuery = false
	return browserQuotedLabel(parsed.String(), 512)
}
