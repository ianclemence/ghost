package personalcontext

import (
	"encoding/json"
	"strings"
)

// Domain classifies what area of life a memory concerns. It is a controlled
// vocabulary: the model may not invent arbitrary categories.
type Domain string

const (
	DomainIdentity      Domain = "identity"
	DomainFood          Domain = "food"
	DomainLocation      Domain = "location"
	DomainWork          Domain = "work"
	DomainTravel        Domain = "travel"
	DomainTechnology    Domain = "technology"
	DomainCommunication Domain = "communication"
	DomainLifestyle     Domain = "lifestyle"
	DomainRelationship  Domain = "relationship"
	DomainHealth        Domain = "health"
	DomainFinance       Domain = "finance"
	DomainEducation     Domain = "education"
	DomainEntertainment Domain = "entertainment"
	DomainFamily        Domain = "family"
	DomainOther         Domain = "other"
)

// domainMap maps predicates to their domain. Unknown predicates map to
// DomainOther so the taxonomy is never corrupted by model output.
var domainMap = map[string]Domain{
	"identity/name":                  DomainIdentity,
	"identity/age":                   DomainIdentity,
	"identity/gender":                DomainIdentity,
	"fact/location":                  DomainLocation,
	"fact/home":                      DomainLocation,
	"fact/job":                       DomainWork,
	"fact/work":                      DomainWork,
	"fact/school":                    DomainEducation,
	"preference/favorite_color":      DomainIdentity,
	"preference/favorite_food":       DomainFood,
	"preference/favorite_drink":      DomainFood,
	"preference/favorite_show":       DomainEntertainment,
	"preference/favorite_movie":      DomainEntertainment,
	"preference/favorite_book":       DomainEntertainment,
	"preference/favorite_song":       DomainEntertainment,
	"preference/favorite_place":      DomainTravel,
	"preference/favorite_music":      DomainEntertainment,
	"preference/communication.style": DomainCommunication,
	"preference/likes":               DomainLifestyle,
	"preference/prefers":             DomainLifestyle,
	"preference/favorite":            DomainLifestyle,
	"goal/primary":                   DomainWork,
	"relationship/partner":           DomainRelationship,
	"relationship/family":            DomainRelationship,
	"relationship/colleague":         DomainRelationship,
	"routine/sleep":                  DomainHealth,
	"routine/exercise":               DomainHealth,
}

// ClassifyDomain returns the controlled-domain classification for an entry's
// predicate. Unknown predicates map to DomainOther.
func ClassifyDomain(predicate string) Domain {
	if d, ok := domainMap[predicate]; ok {
		return d
	}
	// Heuristic fallback: infer domain from the predicate prefix.
	if i := strings.LastIndex(predicate, "/"); i >= 0 {
		suffix := strings.ToLower(predicate[i+1:])
		switch {
		case strings.Contains(suffix, "food") || strings.Contains(suffix, "eat") || strings.Contains(suffix, "drink"):
			return DomainFood
		case strings.Contains(suffix, "location") || strings.Contains(suffix, "live") || strings.Contains(suffix, "city"):
			return DomainLocation
		case strings.Contains(suffix, "work") || strings.Contains(suffix, "job") || strings.Contains(suffix, "career"):
			return DomainWork
		case strings.Contains(suffix, "travel") || strings.Contains(suffix, "trip") || strings.Contains(suffix, "visit"):
			return DomainTravel
		case strings.Contains(suffix, "communi") || strings.Contains(suffix, "style"):
			return DomainCommunication
		}
	}
	return DomainOther
}

// DomainLabel returns a human-readable label for a domain, e.g. "Food",
// "Location", "Work". It is the user-facing form used by the console.
func DomainLabel(d Domain) string {
	switch d {
	case DomainIdentity:
		return "Identity"
	case DomainFood:
		return "Food"
	case DomainLocation:
		return "Location"
	case DomainWork:
		return "Work"
	case DomainTravel:
		return "Travel"
	case DomainTechnology:
		return "Technology"
	case DomainCommunication:
		return "Communication"
	case DomainLifestyle:
		return "Lifestyle"
	case DomainRelationship:
		return "Relationship"
	case DomainHealth:
		return "Health"
	case DomainFinance:
		return "Finance"
	case DomainEducation:
		return "Education"
	case DomainEntertainment:
		return "Entertainment"
	default:
		return "Other"
	}
}

// Title generates a short, deterministic, human-readable title for a memory
// entry. It never calls an LLM — the title is a pure function of kind,
// predicate, and value. Examples:
//
//	identity/name "Ian"          → "Named Ian"
//	fact/location "Bangkok"      → "Lives in Bangkok"
//	preference/likes "cats"      → "Likes cats"
//	goal/primary "build a house" → "Goal: build a house"
//	relationship/partner "Sam"   → "Partner: Sam"
func Title(e Entry) string {
	val := entryValueString(e)
	label := Label(e.Predicate)

	// Identity entries use a special phrasing.
	if e.Kind == KindIdentity {
		switch {
		case strings.Contains(e.Predicate, "name"):
			return "Named " + val
		case strings.Contains(e.Predicate, "age"):
			return "Age: " + val
		case strings.Contains(e.Predicate, "gender"):
			return "Gender: " + val
		default:
			return label + ": " + val
		}
	}

	// Fact entries describe state.
	if e.Kind == KindFact {
		switch {
		case strings.Contains(e.Predicate, "location"):
			return "Lives in " + val
		case strings.Contains(e.Predicate, "home"):
			return "Home: " + val
		case strings.Contains(e.Predicate, "work"):
			return "Works at " + val
		case strings.Contains(e.Predicate, "job"):
			return "Job: " + val
		case strings.Contains(e.Predicate, "school"):
			return "School: " + val
		default:
			return label + ": " + val
		}
	}

	// Preference entries describe likes and preferences.
	if e.Kind == KindPreference {
		switch {
		case strings.Contains(e.Predicate, "likes"):
			return "Likes " + val
		case strings.Contains(e.Predicate, "prefers"):
			return "Prefers " + val
		case strings.Contains(e.Predicate, "favorite"):
			if strings.Contains(e.Predicate, "color") {
				return "Favorite color: " + val
			}
			if strings.Contains(e.Predicate, "food") {
				return "Favorite food: " + val
			}
			if strings.Contains(e.Predicate, "drink") {
				return "Favorite drink: " + val
			}
			if strings.Contains(e.Predicate, "show") || strings.Contains(e.Predicate, "movie") {
				return "Favorite show: " + val
			}
			if strings.Contains(e.Predicate, "book") {
				return "Favorite book: " + val
			}
			if strings.Contains(e.Predicate, "song") {
				return "Favorite song: " + val
			}
			if strings.Contains(e.Predicate, "place") {
				return "Favorite place: " + val
			}
			if strings.Contains(e.Predicate, "music") {
				return "Favorite music: " + val
			}
			return "Favorite: " + val
		case strings.Contains(e.Predicate, "communication"):
			return "Communication style: " + val
		default:
			return label + ": " + val
		}
	}

	// Goal entries.
	if e.Kind == KindGoal {
		return "Goal: " + val
	}

	// Relationship entries.
	if e.Kind == KindRelationship {
		switch {
		case strings.Contains(e.Predicate, "partner"):
			return "Partner: " + val
		case strings.Contains(e.Predicate, "family"):
			return "Family: " + val
		case strings.Contains(e.Predicate, "colleague"):
			return "Colleague: " + val
		default:
			return label + ": " + val
		}
	}

	// Routine entries.
	if e.Kind == KindRoutine {
		return label + ": " + val
	}

	// Fallback: "Label: value".
	return label + ": " + val
}

// Summary generates a natural-language sentence describing a memory entry.
// It is deterministic and never calls an LLM. Examples:
//
//	identity/name "Ian"          → "Your name is Ian."
//	fact/location "Bangkok"      → "You live in Bangkok."
//	preference/likes "cats"      → "You like cats."
//	goal/primary "build a house" → "Your goal is to build a house."
func Summary(e Entry) string {
	val := entryValueString(e)

	if e.Kind == KindIdentity {
		if strings.Contains(e.Predicate, "name") {
			return "Your name is " + val + "."
		}
		if strings.Contains(e.Predicate, "age") {
			return "You are " + val + "."
		}
		return "Your " + Label(e.Predicate) + " is " + val + "."
	}

	if e.Kind == KindFact {
		switch {
		case strings.Contains(e.Predicate, "location"):
			return "You live in " + val + "."
		case strings.Contains(e.Predicate, "home"):
			return "Your home is " + val + "."
		case strings.Contains(e.Predicate, "work"):
			return "You work at " + val + "."
		case strings.Contains(e.Predicate, "job"):
			return "Your job is " + val + "."
		default:
			return "Your " + Label(e.Predicate) + " is " + val + "."
		}
	}

	if e.Kind == KindPreference {
		switch {
		case strings.Contains(e.Predicate, "likes"):
			return "You like " + val + "."
		case strings.Contains(e.Predicate, "prefers"):
			return "You prefer " + val + "."
		case strings.Contains(e.Predicate, "favorite"):
			// Extract the domain part from predicate like "preference/favorite_food"
			domain := lastSegment(e.Predicate)
			// Clean up: remove "favorite_" prefix to get natural name
			domain = strings.TrimPrefix(domain, "favorite_")
			if domain == "favorite" {
				// Legacy "preference/favorite" without domain
				return "Your favorite is " + val + "."
			}
			return "Your favorite " + domain + " is " + val + "."
		case strings.Contains(e.Predicate, "communication"):
			return "You prefer " + val + " communication."
		default:
			return "You prefer " + val + "."
		}
	}

	if e.Kind == KindGoal {
		if strings.HasPrefix(strings.ToLower(val), "to ") {
			return "Your goal is " + val + "."
		}
		return "Your goal is to " + val + "."
	}

	if e.Kind == KindRelationship {
		if strings.Contains(e.Predicate, "partner") {
			return "Your partner is " + val + "."
		}
		if strings.Contains(e.Predicate, "colleague") {
			return "You work with " + val + "."
		}
		return "Your " + Label(e.Predicate) + " is " + val + "."
	}

	if e.Kind == KindRoutine {
		return "Your " + Label(e.Predicate) + " routine is " + val + "."
	}

	return "You " + strings.ToLower(Label(e.Predicate)) + ": " + val + "."
}

// lastSegment returns the part after the last "/" in a predicate, e.g.
// "preference/favorite_food" → "favorite_food".
func lastSegment(predicate string) string {
	if i := strings.LastIndex(predicate, "/"); i >= 0 {
		return predicate[i+1:]
	}
	return predicate
}

// entryValueString reads a string entry value for display.
func entryValueStringForDisplay(e Entry) string {
	var s string
	if err := json.Unmarshal(e.Value, &s); err == nil {
		return s
	}
	return string(e.Value)
}
