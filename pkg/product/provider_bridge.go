package product

// ProviderFailureBridge maps generic provider failure classes to the
// product error taxonomy so tool results speak product language without
// each tool reimplementing the mapping.
//
// NOTE: this file imports pkg/provider. The dependency direction is
// product -> provider (provider never imports product), so no cycle.
import (
	"github.com/ianclemence/ghost/pkg/provider"
)

// ClassForProviderFailure maps a provider failure to a product error class.
func ClassForProviderFailure(f provider.FailureClass) ErrorClass {
	switch f {
	case provider.FailAuth, provider.FailCredentialBad:
		return ErrAuthRequired
	case provider.FailAuthorization:
		return ErrPermission
	case provider.FailRateLimited:
		return ErrRateLimited
	case provider.FailTimeout:
		return ErrTimeout
	case provider.FailNetwork, provider.FailDNS, provider.FailServer, provider.FailUnavailable:
		return ErrProvider
	case provider.FailInvalid, provider.FailMalformed:
		return ErrValidation
	case provider.FailEmpty:
		return ErrExecution
	case provider.FailNotConfigured:
		return ErrConfigRequired
	default:
		return ErrProvider
	}
}

// OutcomeForProviderFailure builds the honest tool outcome for a failed
// provider result. It delegates to Failure so completion mapping cannot
// diverge between call sites.
func OutcomeForProviderFailure(capability string, f provider.FailureClass, err error) Outcome {
	class := ClassForProviderFailure(f)
	return Failure(class, FriendlyFor(capability, class), "", f.Retryable())
}
