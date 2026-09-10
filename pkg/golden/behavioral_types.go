package golden

// Behavioral Golden 100 — v1.
//
// This layer sits above the capability/security conversations. It runs the
// SAME Conversation/Runner machinery (real AgentLoop, real broker, real
// canonical events) but tags each scenario with behavioral metadata and grades
// it along six product dimensions:
//
//	understanding, authority, execution, evidence, recovery, experience.
//
// Hard-fail invariants (false success, unauthorized execution, secret leak,
// stale authority, replay side effect, duplicate side effect, cross-context
// leak) override the score: a high conversational score can never offset a
// security violation.
//
// The suite is a product benchmark, not an LLM benchmark: runtime state is
// inspected wherever a claim is made. Model-only dimensions use deterministic
// heuristic graders, not open-ended model judgement.

// Family is the product area a scenario belongs to.
type Family string

const (
	FamilyConversation Family = "conversation"
	FamilyMemory       Family = "memory"
	FamilyActions      Family = "actions"
	FamilyPermission   Family = "permission"
	FamilyFailure      Family = "failure"
	FamilyBrowser      Family = "browser"
	FamilyComputer     Family = "computer"
	FamilyRoutines     Family = "routines"
	FamilyArtifacts    Family = "artifacts"
	FamilyAdversarial  Family = "adversarial"
)

// FamilyOrder is the stable report order for the ten families.
var FamilyOrder = []Family{
	FamilyConversation, FamilyMemory, FamilyActions, FamilyPermission,
	FamilyFailure, FamilyBrowser, FamilyComputer, FamilyRoutines,
	FamilyArtifacts, FamilyAdversarial,
}

// Style is how the user talks in a scenario.
type Style string

const (
	StyleCasual          Style = "casual"
	StyleTerse           Style = "terse"
	StyleFragmented      Style = "fragmented"
	StyleStreamOfThought Style = "stream-of-consciousness"
	StyleIndirect        Style = "indirect"
	StylePolite          Style = "polite"
	StyleCommanding      Style = "commanding"
	StyleDirect          Style = "direct"
	StyleDistracted      Style = "distracted"
	StyleCorrective      Style = "corrective"
	StyleContradictory   Style = "contradictory"
	StyleEmotional       Style = "emotional"
	StyleUncertain       Style = "uncertain"
	StyleRepetitive      Style = "repetitive"
	StyleReferential     Style = "referential"
	StyleLongForm        Style = "long-form"
	StyleMinimal         Style = "minimal"
	StyleAdversarial     Style = "adversarial"
	StyleTrusting        Style = "trusting"
	StyleCollaborative   Style = "collaborative"
	StyleHandsOff        Style = "hands-off"
)

// Tier is the difficulty band.
type Tier int

const (
	TierL1 Tier = 1 // ordinary
	TierL2 Tier = 2 // multi-turn
	TierL3 Tier = 3 // stateful
	TierL4 Tier = 4 // failure / recovery
	TierL5 Tier = 5 // adversarial / capstone
)

// Dimension is one graded product question.
type Dimension string

const (
	DimUnderstanding Dimension = "understanding"
	DimAuthority     Dimension = "authority"
	DimExecution     Dimension = "execution"
	DimEvidence      Dimension = "evidence"
	DimRecovery      Dimension = "recovery"
	DimExperience    Dimension = "experience"
)

// DimensionOrder is the stable report order.
var DimensionOrder = []Dimension{
	DimUnderstanding, DimAuthority, DimExecution, DimEvidence, DimRecovery, DimExperience,
}

// HardFailCode is a critical violation that fails the whole Golden.
type HardFailCode string

const (
	HFUnauthorizedExecution HardFailCode = "UNAUTHORIZED_EXECUTION"
	HFFalseSuccess          HardFailCode = "FALSE_SUCCESS"
	HFSecretLeak            HardFailCode = "SECRET_LEAK"
	HFStaleAuthority        HardFailCode = "STALE_AUTHORITY"
	HFReplaySideEffect      HardFailCode = "REPLAY_SIDE_EFFECT"
	HFDuplicateSideEffect   HardFailCode = "DUPLICATE_SIDE_EFFECT"
	HFCrossContextLeak      HardFailCode = "CROSS_CONTEXT_LEAK"
	HFCrossConversationLeak HardFailCode = "CROSS_CONVERSATION_LEAK"
	HFClientAuthorityBypass HardFailCode = "CLIENT_AUTHORITY_BYPASS"
	HFPermissionScopeBypass HardFailCode = "PERMISSION_SCOPE_BYPASS"
	HFForgedArgument        HardFailCode = "FORGED_ARGUMENT_ACCEPTED"
	HFCredentialExposure    HardFailCode = "CREDENTIAL_EXPOSURE"
)

// BehavioralMeta tags a Conversation as a Behavioral Golden scenario.
type BehavioralMeta struct {
	Family    Family
	Styles    []Style
	Tier      Tier
	MultiTurn bool
	// Capability/feature flags, for filtering and coverage reporting.
	UsesMemory         bool
	UsesTools          bool
	UsesPermission     bool
	UsesBrowser        bool
	UsesComputer       bool
	UsesRoutine        bool
	UsesArtifact       bool
	UsesFaultInjection bool
	UsesRestart        bool
	UsesProvider       bool
	UsesBackgroundWork bool
	// Dimensions this scenario actually grades.
	Dimensions []Dimension
	// Skip, when set, means the scenario cannot be exercised on this
	// appliance; the reason is reported honestly instead of a verdict.
	Skip string
}

// HasDimension reports whether d is graded by the scenario.
func (m *BehavioralMeta) HasDimension(d Dimension) bool {
	if m == nil {
		return false
	}
	for _, x := range m.Dimensions {
		if x == d {
			return true
		}
	}
	return false
}

// hasStyle reports whether s is present.
func (m *BehavioralMeta) hasStyle(s Style) bool {
	for _, x := range m.Styles {
		if x == s {
			return true
		}
	}
	return false
}

// bc is the compact constructor for a Behavioral Golden conversation.
func bc(id string, fam Family, title string, tier Tier, styles []Style, dims []Dimension, people []Person, exp Expect) Conversation {
	m := &BehavioralMeta{Family: fam, Styles: styles, Tier: tier,
		MultiTurn: len(people) > 0 && len(people[0].Turns) > 1, Dimensions: dims}
	switch fam {
	case FamilyMemory:
		m.UsesMemory = true
	case FamilyActions:
		m.UsesTools = true
	case FamilyPermission:
		m.UsesPermission, m.UsesTools = true, true
	case FamilyFailure:
		m.UsesFaultInjection, m.UsesProvider = true, true
	case FamilyBrowser:
		m.UsesBrowser, m.UsesTools = true, true
	case FamilyComputer:
		m.UsesComputer, m.UsesTools = true, true
	case FamilyRoutines:
		m.UsesRoutine, m.UsesBackgroundWork = true, true
	case FamilyArtifacts:
		m.UsesArtifact, m.UsesTools = true, true
	case FamilyAdversarial:
		m.UsesTools = true
	}
	if exp.Fixture != "" {
		m.UsesTools = true
	}
	c := Conversation{
		ID: id, Category: catForFamily(fam), Title: title, Severity: "normal",
		People: people, Expect: exp, Behavioral: m,
	}
	c.Fixture = exp.Fixture
	c.Behavioral.Skip = exp.Skip
	return c
}

// catForFamily maps a behavioral family onto the existing Category taxonomy so
// the shared runner, history, and reporting keep working unchanged.
func catForFamily(f Family) Category {
	switch f {
	case FamilyMemory:
		return CatMemory
	case FamilyActions:
		return CatConversation
	case FamilyPermission:
		return CatPermission
	case FamilyFailure:
		return CatToolFailure
	case FamilyBrowser, FamilyComputer:
		return CatConversation
	case FamilyRoutines:
		return CatRoutines
	case FamilyArtifacts:
		return CatConversation
	case FamilyAdversarial:
		return CatContextIsolation
	default:
		return CatConversation
	}
}
