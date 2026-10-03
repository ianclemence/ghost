package native

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The accessibility snapshot is the heart of the agent interface: it turns
// Chrome's accessibility tree into a small, readable outline whose interactive
// nodes carry stable @eN refs. A ref resolves back to a backendDOMNodeId, which
// the element layer turns into a real click or keystroke. This mirrors the
// upstream snapshot.rs: interactive roles always get a ref, content roles get
// one when they have a name, and a ref for a given backend node survives
// re-snapshots within the same document (durable refs) so an agent can act on
// what it just read.

// interactiveRoles are the elements an agent can act on.
var interactiveRoles = map[string]bool{
	"button": true, "link": true, "textbox": true, "checkbox": true, "radio": true,
	"combobox": true, "listbox": true, "menuitem": true, "menuitemcheckbox": true,
	"menuitemradio": true, "option": true, "searchbox": true, "slider": true,
	"spinbutton": true, "switch": true, "tab": true, "treeitem": true, "Iframe": true,
}

// contentRoles carry meaning worth surfacing even when not directly clickable.
var contentRoles = map[string]bool{
	"heading": true, "cell": true, "gridcell": true, "columnheader": true,
	"rowheader": true, "listitem": true, "article": true, "region": true,
	"main": true, "navigation": true,
}

type axValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

func (v *axValue) str() string {
	if v == nil || len(v.Value) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(v.Value, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(v.Value, &n) == nil {
		return n.String()
	}
	var b bool
	if json.Unmarshal(v.Value, &b) == nil {
		return fmt.Sprintf("%t", b)
	}
	return ""
}

type axProperty struct {
	Name  string  `json:"name"`
	Value axValue `json:"value"`
}

type axNode struct {
	NodeID           string       `json:"nodeId"`
	ParentID         string       `json:"parentId"`
	ChildIDs         []string     `json:"childIds"`
	Ignored          bool         `json:"ignored"`
	Role             *axValue     `json:"role"`
	Name             *axValue     `json:"name"`
	Value            *axValue     `json:"value"`
	Description      *axValue     `json:"description"`
	Properties       []axProperty `json:"properties"`
	BackendDOMNodeID *int64       `json:"backendDOMNodeId"`

	// refID/hasRef are set during snapshot building, not decoded.
	refID  string
	hasRef bool
}

type axTree struct {
	Nodes []axNode `json:"nodes"`
}

// refEntry is what a ref resolves to: a concrete backend node, plus the role
// and name that let the element layer re-find it if the backend id goes stale
// (a client-side re-render keeps the element but changes its node id).
type refEntry struct {
	BackendNodeID int64
	Role          string
	Name          string
	Nth           int
	HasBackend    bool
}

// refMap holds the refs minted by the most recent snapshot.
type refMap struct {
	mu       sync.Mutex
	refs     map[string]refEntry
	durable  map[int64]string
	next     int
	document string
}

func newRefMap() *refMap {
	return &refMap{refs: map[string]refEntry{}, durable: map[int64]string{}, next: 1}
}

func (r *refMap) reset(document string) {
	r.document = document
	r.refs = map[string]refEntry{}
	r.durable = map[int64]string{}
	r.next = 1
}

func (r *refMap) get(ref string) (refEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.refs[ref]
	return e, ok
}

// snapshotOptions mirror the CLI flags that shape a snapshot.
type snapshotOptions struct {
	Interactive bool
	Compact     bool
	Depth       int
	Selector    string
}

// takeSnapshot fetches the accessibility tree, mints refs, stores them, and
// returns the rendered outline. If the page's document changed since the last
// snapshot it drops every prior ref first, so a stale ref never silently acts
// on a recycled node.
func (s *Session) takeSnapshot(ctx context.Context, opts snapshotOptions) (string, error) {
	if err := s.cdp.send(ctx, "DOM.enable", nil, nil); err != nil {
		return "", err
	}
	if err := s.cdp.send(ctx, "Accessibility.enable", nil, nil); err != nil {
		return "", err
	}

	// Document identity: the loader id changes when the top frame navigates.
	// Same document keeps refs; a new one invalidates them.
	doc := ""
	var ft struct {
		FrameTree struct {
			Frame struct {
				ID       string `json:"id"`
				LoaderID string `json:"loaderId"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err := s.cdp.send(ctx, "Page.getFrameTree", nil, &ft); err == nil {
		doc = ft.FrameTree.Frame.LoaderID
	}
	s.refs.mu.Lock()
	if doc != "" && s.refs.document != doc {
		s.refs.reset(doc)
	}
	s.refs.mu.Unlock()

	var tree axTree
	if err := s.cdp.send(ctx, "Accessibility.getFullAXTree", map[string]interface{}{}, &tree); err != nil {
		return "", err
	}

	byID := make(map[string]*axNode, len(tree.Nodes))
	for i := range tree.Nodes {
		byID[tree.Nodes[i].NodeID] = &tree.Nodes[i]
	}
	// Roots: nodes whose parent is absent from this tree.
	var roots []*axNode
	for i := range tree.Nodes {
		n := &tree.Nodes[i]
		if n.ParentID == "" {
			roots = append(roots, n)
		} else if _, ok := byID[n.ParentID]; !ok {
			roots = append(roots, n)
		}
	}
	// Stable order: prefer the order Chrome returned.
	order := map[string]int{}
	for i, n := range tree.Nodes {
		order[n.NodeID] = i
	}
	sort.SliceStable(roots, func(i, j int) bool { return order[roots[i].NodeID] < order[roots[j].NodeID] })

	counts := map[string]int{}

	// First pass: decide which nodes get refs and their duplicate index.
	type refAssignment struct {
		node *axNode
		role string
		name string
		nth  int
	}
	var assignments []refAssignment
	var walk func(n *axNode)
	walk = func(n *axNode) {
		if n.Ignored {
			for _, cid := range n.ChildIDs {
				if c, ok := byID[cid]; ok {
					walk(c)
				}
			}
			return
		}
		role := n.Role.str()
		name := normalizeName(n.Name.str())
		shouldRef := interactiveRoles[role] || (contentRoles[role] && name != "")
		if shouldRef {
			key := role + ":" + name
			idx := counts[key]
			counts[key]++
			assignments = append(assignments, refAssignment{node: n, role: role, name: name, nth: idx})
		}
		for _, cid := range n.ChildIDs {
			if c, ok := byID[cid]; ok {
				walk(c)
			}
		}
	}
	for _, r := range roots {
		walk(r)
	}

	// Second pass: allocate or reuse durable refs.
	s.refs.mu.Lock()
	for _, a := range assignments {
		var ref string
		if a.node.BackendDOMNodeID != nil {
			if existing, ok := s.refs.durable[*a.node.BackendDOMNodeID]; ok {
				ref = existing
			} else {
				ref = fmt.Sprintf("e%d", s.refs.next)
				s.refs.next++
				s.refs.durable[*a.node.BackendDOMNodeID] = ref
			}
		} else {
			ref = fmt.Sprintf("e%d", s.refs.next)
			s.refs.next++
		}
		hasBackend := a.node.BackendDOMNodeID != nil
		var bid int64
		if hasBackend {
			bid = *a.node.BackendDOMNodeID
		}
		nth := a.nth
		if counts[a.role+":"+a.name] < 2 {
			nth = -1 // unique: no nth qualifier needed
		}
		s.refs.refs[ref] = refEntry{BackendNodeID: bid, Role: a.role, Name: a.name, Nth: nth, HasBackend: hasBackend}
		a.node.setRef(ref)
	}
	s.refs.mu.Unlock()

	// Render.
	var b strings.Builder
	render := func(n *axNode, depth int) {}
	render = func(n *axNode, depth int) {
		if opts.Depth > 0 && depth > opts.Depth {
			return
		}
		if !n.Ignored {
			line := renderNode(n, depth)
			if line != "" && (!opts.Interactive || n.ref() != "") {
				b.WriteString(line)
				b.WriteByte('\n')
			}
		}
		for _, cid := range n.ChildIDs {
			if c, ok := byID[cid]; ok {
				render(c, depth+1)
			}
		}
	}
	if opts.Interactive {
		// Keep ref-bearing nodes with their ancestors for readability.
		out := renderInteractive(roots, byID, opts)
		return strings.TrimRight(out, "\n"), nil
	}
	for _, r := range roots {
		render(r, 0)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// refHolder attaches the allocated ref to an AX node for rendering. The
// upstream keeps this on its TreeNode; here a side map avoids mutating the
// decoded struct's shape.
func (n *axNode) setRef(ref string) { n.refID = ref; n.hasRef = true }
func (n *axNode) ref() string       { return n.refID }

// renderNode formats one line like the CLI: "- role \"name\" [ref=eN]".
func renderNode(n *axNode, depth int) string {
	role := n.Role.str()
	name := normalizeName(n.Name.str())
	if role == "" {
		role = "generic"
	}
	indent := strings.Repeat("  ", depth)
	var sb strings.Builder
	sb.WriteString(indent)
	sb.WriteString("- ")
	sb.WriteString(role)
	if name != "" {
		sb.WriteString(" ")
		sb.WriteString(strconv.Quote(name))
	}
	if v := n.Value.str(); v != "" && role != "textbox" && role != "searchbox" {
		sb.WriteString(fmt.Sprintf(" value=%q", v))
	}
	for _, p := range n.Properties {
		switch p.Name {
		case "checked":
			sb.WriteString(" checked=" + p.Value.str())
		case "expanded":
			sb.WriteString(" expanded=" + p.Value.str())
		case "selected":
			sb.WriteString(" selected=" + p.Value.str())
		case "disabled":
			sb.WriteString(" disabled=" + p.Value.str())
		case "level":
			sb.WriteString(" level=" + p.Value.str())
		case "required":
			sb.WriteString(" required=" + p.Value.str())
		}
	}
	if ref := n.ref(); ref != "" {
		sb.WriteString(" [ref=" + ref + "]")
	}
	return sb.String()
}

// renderInteractive emits ref-bearing nodes, each with its ancestor chain, so
// an agent gets a compact actionable list.
func renderInteractive(roots []*axNode, byID map[string]*axNode, opts snapshotOptions) string {
	var b strings.Builder
	seen := map[string]bool{}
	var walk func(n *axNode, ancestors []*axNode, depth int)
	walk = func(n *axNode, ancestors []*axNode, depth int) {
		if !n.Ignored {
			if r := n.ref(); r != "" {
				for i, a := range ancestors {
					if a.Ignored || seen[a.NodeID] {
						continue
					}
					seen[a.NodeID] = true
					b.WriteString(renderNode(a, i))
					b.WriteByte('\n')
				}
				if !seen[n.NodeID] {
					seen[n.NodeID] = true
					b.WriteString(renderNode(n, len(ancestors)))
					b.WriteByte('\n')
				}
			}
		}
		for _, cid := range n.ChildIDs {
			if c, ok := byID[cid]; ok {
				walk(c, append(ancestors, n), depth+1)
			}
		}
	}
	for _, r := range roots {
		walk(r, nil, 0)
	}
	return b.String()
}

// normalizeName collapses the whitespace and strips the zero-width runes that
// make AX names compare unequal for no visible reason.
func normalizeName(s string) string {
	replacer := strings.NewReplacer("\u00a0", " ", "\ufeff", "", "\u200b", "", "\u200c", "", "\u200d", "", "\u2060", "")
	s = replacer.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
