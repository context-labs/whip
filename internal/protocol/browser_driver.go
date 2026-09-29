package protocol

type HostBrowserDriver struct {
	Revision         string `json:"revision" pattern:"^[a-f0-9]{64}$"`
	ConfiguredDriver string `json:"configured_driver" enum:"rod,chromedp"`
	Driver           string `json:"driver" enum:"rod,chromedp"`
	Pinned           bool   `json:"pinned"`
}
type SetBrowserDriverParams struct {
	ExpectedRevision string `json:"expected_revision" pattern:"^[a-f0-9]{64}$"`
	Driver           string `json:"driver" enum:"rod,chromedp"`
}
