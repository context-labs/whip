package daemon

import (
	"context"
	"errors"
	"strings"

	"github.com/context-labs/whip/internal/llm"
)

// providerValidationError gives a next action without exposing upstream bodies.
func providerValidationError(err error) error {
	if errors.Is(err, errNoCompatibleProviderModels) {
		return errNoCompatibleProviderModels
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("provider validation timed out; check the connection and retry")
	}
	var response *llm.HTTPError
	if errors.As(err, &response) {
		code := strings.Fields(response.Status)
		if len(code) > 0 {
			switch code[0] {
			case "401":
				return errors.New("provider rejected the API key; check the key and try again")
			case "402":
				return errors.New("provider requires account credit or billing setup; check your provider account")
			case "403":
				return errors.New("provider denied access; check your account and API key permissions")
			case "429":
				return errors.New("provider rate limit reached; wait and try again")
			}
		}
	}
	return errors.New("could not load the provider model list; check the connection and retry")
}
