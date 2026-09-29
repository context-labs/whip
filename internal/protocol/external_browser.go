package protocol

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

type ExternalBrowserConfiguration struct {
	Mode             string `json:"mode" enum:"disabled,live,dedicated,headless,extension"`
	Executable       string `json:"executable"`
	LiveEndpoint     string `json:"live_endpoint"`
	LiveProfile      string `json:"live_profile"`
	AllowPrivateURLs bool   `json:"allow_private_urls"`
}
type ExternalBrowserStatus struct {
	Revision      string                       `json:"revision" pattern:"^[a-f0-9]{64}$"`
	Configuration ExternalBrowserConfiguration `json:"configuration"`
	Driver        string                       `json:"driver" enum:"rod,chromedp"`
	DriverPinned  bool                         `json:"driver_pinned"`
}
type ConfigureExternalBrowserParams struct {
	ExpectedRevision string                       `json:"expected_revision" pattern:"^[a-f0-9]{64}$"`
	Configuration    ExternalBrowserConfiguration `json:"configuration"`
}
type ExternalBrowserSession struct {
	RootID     ID     `json:"root_id"`
	Name       string `json:"name" pattern:"^[A-Za-z0-9_-]{1,64}$"`
	Mode       string `json:"mode" enum:"live,dedicated,headless,extension"`
	Driver     string `json:"driver" enum:"rod,chromedp"`
	Generation ID     `json:"generation"`
	Resource   string `json:"resource" pattern:"^browser-external:[a-f0-9]{64}$"`
	State      string `json:"state" enum:"prepared,connected,ended"`
}
type (
	ExternalBrowserSessions struct {
		Items []ExternalBrowserSession `json:"items"`
	}
	ExternalBrowserConnectionParams struct {
		RootID     ID     `json:"root_id"`
		Name       string `json:"name" pattern:"^[A-Za-z0-9_-]{1,64}$"`
		Generation ID     `json:"generation"`
	}
)

func externalBrowserSchema(schema *jsonschema.Schema, t reflect.Type) {
	switch t {
	case reflect.TypeFor[ExternalBrowserConfiguration]():
		for _, name := range []string{"executable", "live_endpoint", "live_profile"} {
			schema.Properties[name].MaxLength = new(4096)
		}
	case reflect.TypeFor[ExternalBrowserSessions]():
		schema.Properties["items"].MaxItems = new(4)
	}
}
