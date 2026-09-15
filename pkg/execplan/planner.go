// Package execplan selects phone / pod / cloud execution deterministically.
// Effort never automatically means "send to cloud": the policy stays
// local-first and privacy modes are hard gates, not hints.
package execplan

import "github.com/ianclemence/ghost/pkg/effort"

// Target is an execution location.
type Target string

const (
	TargetPhone Target = "phone"
	TargetPod   Target = "pod"
	TargetCloud Target = "cloud"
)

// Privacy is the user-selected data policy.
type Privacy string

const (
	// PrivacyLocalOnly: phone-local, plus pod-local only when explicitly
	// selected/available. Never cloud.
	PrivacyLocalOnly Privacy = "local_only"
	// PrivacyBalanced: phone + pod preferred; cloud for eligible tasks.
	PrivacyBalanced Privacy = "balanced"
	// PrivacyCloudCapable: any available target subject to task policy.
	PrivacyCloudCapable Privacy = "cloud_capable"
)

// Availability reports which targets can execute right now.
type Availability struct {
	Phone bool
	Pod   bool
	Cloud bool
	// PhoneModel, PodModel indicate a usable model is loaded/known.
	PhoneModel bool
	PodModel   bool
	CloudModel bool
	// PodHasHardware: task needs Pod-only tools (GPIO/ESP32/sensors).
	PodHasHardware bool
	// NeedsHardware: this task requires Pod hardware executors.
	NeedsHardware bool
	// NeedsCloud: task inherently needs cloud (e.g. cloud-only tool).
	NeedsCloud bool
}

// Input is one routing decision.
type Input struct {
	Effort       effort.Level
	Privacy      Privacy
	Avail        Availability
	PodPreferred bool // user pinned "Ghost · Home"
}

// Decision is the deterministic, explainable outcome.
type Decision struct {
	Target Target `json:"target"`
	Reason string `json:"reason"`
}

// Plan selects the execution target. Deterministic: same input → same output.
func Plan(in Input) Decision {
	// Hard gates first.
	if in.Avail.NeedsHardware {
		if in.Avail.Pod && in.Avail.PodModel {
			return Decision{TargetPod, "task requires pod hardware executor"}
		}
		if in.Avail.Phone {
			// Phone understands the request; without the Pod it must say so.
			return Decision{TargetPhone, "hardware unavailable; phone explains and offers retry when pod returns"}
		}
		return Decision{TargetCloud, "no local executor available"}
	}
	switch in.Privacy {
	case PrivacyLocalOnly:
		// Never cloud. Prefer explicit pod pin, else phone.
		if in.PodPreferred && in.Avail.Pod && in.Avail.PodModel {
			return Decision{TargetPod, "local-only; user-selected pod"}
		}
		if in.Avail.Phone && in.Avail.PhoneModel {
			return Decision{TargetPhone, "local-only; phone-local model"}
		}
		if in.Avail.Pod && in.Avail.PodModel {
			return Decision{TargetPod, "local-only; pod-local fallback"}
		}
		if in.Avail.Phone {
			return Decision{TargetPhone, "local-only; phone best effort (model missing)"}
		}
		return Decision{TargetPod, "local-only; pod best effort"}
	case PrivacyBalanced, PrivacyCloudCapable:
		// Local-first ladder: Quick→phone, Normal→phone/pod, Deep→pod, Max→cloud.
		// (Max maps to Deep budget + cloud eligibility; effort pkg has no Max
		// level, so callers pass Deep with NeedsCloud or CloudModel-only avail.)
		switch in.Effort {
		case effort.Quick:
			if in.Avail.Phone && in.Avail.PhoneModel {
				return Decision{TargetPhone, "quick effort served by phone-local"}
			}
			if in.Avail.Pod && in.Avail.PodModel {
				return Decision{TargetPod, "quick; phone model missing, pod-local"}
			}
		case effort.Normal:
			if in.PodPreferred && in.Avail.Pod && in.Avail.PodModel {
				return Decision{TargetPod, "user prefers home pod"}
			}
			if in.Avail.Phone && in.Avail.PhoneModel {
				return Decision{TargetPhone, "normal effort served by phone-local"}
			}
			if in.Avail.Pod && in.Avail.PodModel {
				return Decision{TargetPod, "normal; phone model missing, pod-local"}
			}
		case effort.Deep:
			if in.Avail.Pod && in.Avail.PodModel {
				return Decision{TargetPod, "deep effort served by stronger pod-local model"}
			}
			if in.Avail.Phone && in.Avail.PhoneModel {
				return Decision{TargetPhone, "deep; pod unavailable, phone-local best effort"}
			}
		}
		// Cloud only when local targets cannot serve and policy allows.
		if in.Avail.NeedsCloud || (!in.Avail.PhoneModel && !in.Avail.PodModel) {
			if in.Avail.Cloud && in.Avail.CloudModel {
				if in.Privacy == PrivacyCloudCapable || in.Avail.NeedsCloud {
					return Decision{TargetCloud, "local models unavailable; policy permits cloud"}
				}
			}
		}
		if in.Avail.Pod && in.Avail.PodModel {
			return Decision{TargetPod, "fallback to pod-local"}
		}
		if in.Avail.Phone {
			return Decision{TargetPhone, "fallback to phone (best effort)"}
		}
		return Decision{TargetCloud, "no local target; cloud last resort"}
	default:
		return Decision{TargetPhone, "unknown privacy; safest local default"}
	}
}
