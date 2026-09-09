// Package computer makes computers a capability of the personal AI
// runtime — not a separate product. One Ghost drives any number of
// computers (its own appliance, a paired laptop, a future sandbox)
// through one interface, one lease discipline, one permission broker,
// and one evidence trail.
//
// The model never touches a computer directly: it proposes operations,
// the broker authorizes them, execution produces evidence, and canonical
// events record what happened.
package computer

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Op is one computer operation. The set is deliberately bounded: observe
// and navigate freely, change things only through the broker.
type Op string

const (
	OpObserve   Op = "observe"   // read screen state / list windows
	OpScreenshot Op = "screenshot" // capture pixels
	OpInspectUI Op = "inspect_ui" // accessibility tree / interactive elements
	OpOpenApp   Op = "open_app"  // launch an application by name
	OpOpenURL   Op = "open_url"  // open a URL in the browser
	OpNavigate  Op = "navigate"  // browser back/forward/goto
	OpScroll    Op = "scroll"    // scroll a view
	OpClick     Op = "click"     // click at a point or element
	OpClose     Op = "close"     // close a window/tab
	OpWait      Op = "wait"      // wait for condition or duration
	OpType      Op = "type"      // type text (credential-adjacent: brokered)
	OpPressKey  Op = "press_key" // press keys (credential-adjacent: brokered)
	OpUpload    Op = "upload"    // move bytes onto the computer
	OpDownload  Op = "download"  // move bytes off the computer
	OpExecute   Op = "execute"   // bounded shell execution, separately permissioned
)

// Risk maps an operation to Ghost's broker risk vocabulary
// (read_only, low_risk, consequential, high_impact). Financial,
// destructive, and account actions are classified by the broker on top
// of this via capability+action detail — this mapping is the floor, not
// the ceiling.
func OpRisk(op Op) string {
	switch op {
	case OpObserve, OpScreenshot, OpInspectUI, OpWait:
		return "read_only"
	case OpOpenApp, OpOpenURL, OpNavigate, OpScroll, OpClick, OpClose:
		return "low_risk"
	case OpType, OpPressKey, OpUpload, OpDownload:
		return "consequential"
	case OpExecute:
		return "high_impact"
	default:
		return "consequential"
	}
}

// Placement is where a computer executes.
type Placement string

const (
	// PlacementLocal is the Ghost appliance itself.
	PlacementLocal Placement = "local"
	// PlacementPaired is a user computer paired with this Ghost.
	PlacementPaired Placement = "paired"
	// PlacementRemote is a remote execution host (future).
	PlacementRemote Placement = "remote"
	// PlacementSandbox is an isolated execution environment (future).
	PlacementSandbox Placement = "sandbox"
)

// Args carries operation parameters. Values are plain data; credentials
// must never be placed here (see credential handling in the browser
// package and the credential store).
type Args map[string]string

// Result is what execution returns. Success is established by Verified,
// never by the model claiming it: drivers set Verified only when they
// observe the expected end state (window present, URL loaded, file hash
// matches, exit code read).
type Result struct {
	Output   string
	Evidence map[string]string
	Verified bool
}

// Computer is one controllable machine. Implementations must be safe for
// concurrent use by the lease discipline (one owner at a time); the
// interface itself does not enforce ownership — Acquire does.
type Computer interface {
	// ID is the stable resource identity leases bind to.
	ID() string
	// Placement reports where this computer executes.
	Placement() Placement
	// DisplayName is the human-facing label ("Ghost appliance", "MacBook").
	DisplayName() string
	// Do executes one operation. Unimplemented ops return an explicit
	// error naming the op — never silent success.
	Do(ctx context.Context, op Op, args Args) (Result, error)
	// SupportedOps lists what Do can actually perform here.
	SupportedOps() []Op
}

// Supports reports whether op is in the computer's repertoire.
func Supports(c Computer, op Op) bool {
	for _, o := range c.SupportedOps() {
		if o == op {
			return true
		}
	}
	return false
}

// CapabilityID renders the broker capability for an op on a computer,
// e.g. "computer.click". Risk comes from OpRisk; the broker adds
// capability+action detail (financial, destructive, account) on top.
func CapabilityID(op Op) string {
	return "computer." + string(op)
}

// DescribeOp renders one line of human-readable documentation for an op.
func DescribeOp(op Op) string {
	noun := strings.ReplaceAll(string(op), "_", " ")
	return noun + " (" + OpRisk(op) + ")"
}

// ErrUnsupportedOp is returned when a placement cannot perform an op.
func ErrUnsupportedOp(id string, op Op) error {
	return fmt.Errorf("computer %s does not support %s", id, op)
}

// ErrComputerBusy is returned when a lease is held by another task.
func ErrComputerBusy(id, holder string) error {
	return fmt.Errorf("computer %s is busy (held by %s)", id, holder)
}

// ErrComputerUnavailable is returned when the computer cannot be reached.
func ErrComputerUnavailable(id, reason string) error {
	return fmt.Errorf("computer %s unavailable: %s", id, reason)
}

// DefaultLeaseTTL bounds how long one task may hold a computer without
// renewing. Short enough that a crashed task frees the machine quickly;
// long enough that steady work never churns.
const DefaultLeaseTTL = 10 * time.Minute

// MaxLeaseTTL caps renewals so a runaway task cannot squat forever.
const MaxLeaseTTL = 2 * time.Hour
