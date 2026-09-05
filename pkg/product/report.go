package product

// FailureReport separates a single failure into three layers:
//
//	Product:     human-readable result ("Calendar is unavailable…").
//	Diagnostics: technical detail for debugging (class, provider,
//	            request id, attempt). Never secrets, never raw traces.
//	Secret:      nothing. This layer must always be empty — enforced by
//	            construction (there is no field for it).
//
// One failure, three audiences, zero leak paths.
type FailureReport struct {
	// UserMessage is safe to show verbatim anywhere.
	UserMessage string `json:"user_message"`
	// Completion is the canonical outcome (never success here).
	Completion Completion `json:"completion"`
	// Diagnostic carries debugging fields (all redacted at the source).
	Diagnostic map[string]string `json:"diagnostic"`
}

// ReportFailure builds the layered report for a failed provider result.
// capability/provider/requestID/attempt are safe metadata; err contributes
// only its CLASS (never its text — provider errors may quote tokens).
func ReportFailure(capability, providerName, requestID string, attempt int, class ErrorClass, failureReason string) FailureReport {
	outcome := Failure(class, FriendlyFor(capability, class), "", false)
	diag := map[string]string{
		"capability": capability,
		"class":      string(class),
		"reason":     failureReason,
	}
	if providerName != "" {
		diag["provider"] = providerName
	}
	if requestID != "" {
		diag["request_id"] = requestID
	}
	if attempt > 0 {
		diag["attempt"] = itoa(attempt)
	}
	return FailureReport{UserMessage: outcome.UserMessage, Completion: outcome.Completion, Diagnostic: diag}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
