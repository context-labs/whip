package daemon

import (
	"context"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
)

// DiscoverProviders persists missing routes without contacting providers. A
// failed write leaves the effective inventory usable and reports its failure.
func (s *ProviderService) DiscoverProviders(ctx context.Context, model, provider string) (protocol.ProviderList, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ProviderList{}, err
	}
	_, _, discoveryErr := config.PersistDiscoveredProviders(ctx)
	if err := ctx.Err(); err != nil {
		return protocol.ProviderList{}, err
	}
	list, err := s.ListProvidersFor(model, provider)
	if err != nil {
		return protocol.ProviderList{}, err
	}
	if discoveryErr != nil {
		list.DiscoveryError = "Provider discovery needs attention: " + discoveryErr.Error()
	}
	return list, nil
}
