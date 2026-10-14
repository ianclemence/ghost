package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// NodeType is the AgentRun-compatible node name.
type NodeType string

const (
	NodeChain    NodeType = "chain"
	NodeRoute    NodeType = "route"
	NodeJudge    NodeType = "judge"
	NodeSift     NodeType = "sift"
	NodeCode     NodeType = "code"
	NodeMap      NodeType = "map"
	NodeLoop     NodeType = "loop_until"
	NodeEscalate NodeType = "escalate"
)

// QuestionKind mirrors Jev primitives without importing any SDK.
type QuestionKind string

const (
	QChoice QuestionKind = "choice"
	QScore  QuestionKind = "score"
	QNoul   QuestionKind = "noul"
)

// Question is one typed micro-decision. Thresholds live in code gates,
// never inside the question, so retuning never rewrites the rubric.
type Question struct {
	ID           string            `json:"id"`
	Kind         QuestionKind      `json:"kind"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"` // choice: option->criterion
	Rubric       []string          `json:"rubric,omitempty"`   // score: ordered levels
}

// Node is one step. Only fields for its Type are populated.
type Node struct {
	ID        string     `json:"id"`
	Type      NodeType   `json:"type"`
	Question  *Question  `json:"question,omitempty"`  // judge
	Questions []Question `json:"questions,omitempty"` // sift: same question per item
	// Route: branch name -> node id, plus "uncertain" fallback id.
	Branches map[string]string `json:"branches,omitempty"`
	// Code gate: named predicate evaluated in Go (see engine.go registry).
	Gate string `json:"gate,omitempty"`
	// Map: apply Sub to each element of ListKey with Concurrency bound.
	ListKey     string `json:"list_key,omitempty"`
	Concurrency int    `json:"concurrency,omitempty"`
	Sub         *Node  `json:"sub,omitempty"`
	// Loop: repeat Body until Until gate is true or MaxIter reached.
	Body    *Node  `json:"body,omitempty"`
	Until   string `json:"until,omitempty"`
	MaxIter int    `json:"max_iter,omitempty"`
	// Chain: ordered steps.
	Steps []Node `json:"steps,omitempty"`
	// Escalate: human-readable reason recorded on the ledger.
	Reason string `json:"reason,omitempty"`
}

// Workflow is a named, versioned routine template.
type Workflow struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Root    Node   `json:"root"`
}

// ParseStrict decodes AgentRun-compatible JSON and validates structure.
func ParseStrict(data []byte) (*Workflow, error) {
	var w Workflow
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil {
		return nil, fmt.Errorf("workflow: decode: %w", err)
	}
	if err := Validate(&w); err != nil {
		return nil, err
	}
	return &w, nil
}

// Validate enforces bounds: loop 1..20, map concurrency 1..16, required ids.
func Validate(w *Workflow) error {
	if w == nil {
		return errors.New("workflow: nil")
	}
	if strings.TrimSpace(w.Name) == "" {
		return errors.New("workflow: name required")
	}
	if strings.TrimSpace(w.Version) == "" {
		return errors.New("workflow: version required")
	}
	return validateNode(&w.Root, 0)
}

func validateNode(n *Node, depth int) error {
	if depth > 8 {
		return errors.New("workflow: nesting too deep (max 8)")
	}
	if n == nil {
		return errors.New("workflow: nil node")
	}
	if strings.TrimSpace(n.ID) == "" {
		return errors.New("workflow: node id required")
	}
	switch n.Type {
	case NodeChain:
		if len(n.Steps) == 0 {
			return fmt.Errorf("workflow: chain %q needs steps", n.ID)
		}
		for i := range n.Steps {
			if err := validateNode(&n.Steps[i], depth+1); err != nil {
				return err
			}
		}
	case NodeMap:
		if strings.TrimSpace(n.ListKey) == "" {
			return fmt.Errorf("workflow: map %q needs list_key", n.ID)
		}
		if n.Concurrency <= 0 || n.Concurrency > 16 {
			return fmt.Errorf("workflow: map %q concurrency must be 1..16", n.ID)
		}
		if n.Sub == nil {
			return fmt.Errorf("workflow: map %q needs sub", n.ID)
		}
		return validateNode(n.Sub, depth+1)
	case NodeLoop:
		if n.Body == nil {
			return fmt.Errorf("workflow: loop %q needs body", n.ID)
		}
		if n.MaxIter < 1 || n.MaxIter > 20 {
			return fmt.Errorf("workflow: loop %q max_iter must be 1..20", n.ID)
		}
		if strings.TrimSpace(n.Until) == "" {
			return fmt.Errorf("workflow: loop %q needs until gate", n.ID)
		}
		return validateNode(n.Body, depth+1)
	case NodeJudge:
		if n.Question == nil || strings.TrimSpace(n.Question.Instructions) == "" {
			return fmt.Errorf("workflow: judge %q needs question.instructions", n.ID)
		}
	case NodeSift:
		if len(n.Questions) == 0 {
			return fmt.Errorf("workflow: sift %q needs questions", n.ID)
		}
	case NodeRoute:
		if len(n.Branches) == 0 {
			return fmt.Errorf("workflow: route %q needs branches", n.ID)
		}
	case NodeCode:
		if strings.TrimSpace(n.Gate) == "" {
			return fmt.Errorf("workflow: code %q needs gate", n.ID)
		}
	case NodeEscalate:
		if strings.TrimSpace(n.Reason) == "" {
			return fmt.Errorf("workflow: escalate %q needs reason", n.ID)
		}
	default:
		return fmt.Errorf("workflow: unknown node type %q", n.Type)
	}
	return nil
}

// Digest fingerprints name+version+canonical JSON for ledger + replay.
func Digest(w *Workflow) string {
	raw, _ := json.Marshal(struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Root    Node   `json:"root"`
	}{w.Name, w.Version, w.Root})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}
