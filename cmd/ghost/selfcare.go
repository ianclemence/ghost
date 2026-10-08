package main

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/appliance"
	"github.com/ianclemence/ghost/pkg/awareness"
	"github.com/ianclemence/ghost/pkg/channels"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/hardware"
)

// securityGuard notices repeated failed access to the gateway and tells the
// owner. Set once at startup; nil-safe everywhere it is used.
var securityGuard *awareness.Guard

// noteFailedAccess records a failed attempt from a non-local peer.
func noteFailedAccess(r *http.Request, kind awareness.Kind) {
	if securityGuard == nil || r == nil || isLoopbackRequest(r) {
		return
	}
	securityGuard.Note(kind, awareness.Source(r.RemoteAddr))
}

// Thresholds. A Pod runs warm by design, so these are for "act on it", not "it
// is a computer". Each has hysteresis through the announcer's cooldown.
const (
	cpuHotC       = 80.0
	roomHotC      = 33.0
	roomColdC     = 8.0
	roomHumidPct  = 75.0
	roomDryPct    = 20.0
	selfCareEvery = 5 * time.Minute
	// Memory short on this many checks in a row (half an hour) is not a blip.
	sustainedShortChecks = 6
	updateCheckGap       = 6 * time.Hour
)

var (
	memoryShortChecks int
	// retract takes an alert back once what it reported has stayed gone.
	retract = newConditionWatch()
	// uncleanStart is set at boot when the last run did not stop cleanly.
	uncleanStart bool
)

// startSelfCare runs the loop that lets Ghost be aware of itself and its
// surroundings, and say so.
func startSelfCare(al *agent.AgentLoop, workspace string) {
	// A stranger writing to one of Ghost's messaging channels is refused; the
	// owner is told once per person per day, with what they need to allow them.
	channels.OnUnknownSender = func(channel, sender string) {
		id, name, _ := strings.Cut(sender, "|")
		who := id
		if name != "" {
			who = name + " (" + id + ")"
		}
		al.Announce("stranger:"+channel,
			fmt.Sprintf("Someone I don't know, %s, wrote to me on %s. I didn't answer. If they're a friend you want me to talk to, add %q under Channels in the console; otherwise ignore this.", who, strings.Title(channel), id),
			time.Hour, false) // per channel, so a flood of made-up IDs is one message
	}
	securityGuard = awareness.NewGuard(func(text string) {
		al.Announce("security:"+fmt.Sprint(time.Now().Unix()/1800), text, time.Minute, true)
	})
	// Alerts written before they carried a key: key them by their opening
	// words, so one whose condition has cleared since can be resolved.
	al.KeyOlderNotices(map[string]string{
		"I'm almost out of storage:": "storage-critical",
		"Storage is getting low:":    "storage-low",
		"I'm short on memory:":       "memory-critical",
		"Memory is getting tight:":   "memory-low",
		"The Pod is running hot:":    "pod-hot",
	})
	go func() {
		lastUpdateCheck := time.Time{}
		t := time.NewTicker(selfCareEvery)
		defer t.Stop()
		time.Sleep(90 * time.Second) // let the daemon settle after boot
		for {
			selfCareOnce(al, workspace, &lastUpdateCheck)
			<-t.C
		}
	}()
}

func selfCareOnce(al *agent.AgentLoop, workspace string, lastUpdateCheck *time.Time) {
	// Anything the daemon left readable only by the service account would make
	// a backup or an update fail; hand it back to the workspace's owner.
	config.RepairOwnership(workspace)
	snap := hardware.Snapshot(workspace)
	// Tell the owner when the Pod is getting short of something, before it is
	// slow, and when it has been short for so long that it is simply too small.
	if snap.Memory != hardware.PressureNormal {
		memoryShortChecks++
	} else {
		memoryShortChecks = 0
	}
	for _, a := range hardware.Assess(snap, hardware.OnMemoryCard(), memoryShortChecks >= sustainedShortChecks) {
		al.Announce(a.Key, a.Text, a.Cooldown, a.Urgent)
	}
	// What is true now, for each alert that reports a condition that can clear.
	// A reading that could not be taken is left out: unknown is not "fine".
	conditions := map[string]bool{
		"storage-critical": snap.Storage == hardware.PressureCritical,
		"storage-low":      snap.Storage == hardware.PressureWarning,
		"memory-critical":  snap.Memory == hardware.PressureCritical,
		"memory-low":       snap.Memory == hardware.PressureWarning,
	}
	if uncleanStart {
		uncleanStart = false
		al.Announce("unclean-stop", "I restarted after being cut off, most likely a power cut or a crash. I checked my memory and it's intact, so nothing was lost. If this happens often, a small battery backup for the Pod is worth it.", time.Minute, false)
	}

	rs := hardware.ReadEnvironment("")
	if cpu := hardware.CPU(rs); cpu != nil {
		conditions["pod-hot"] = cpu.Value >= cpuHotC
	}
	if cpu := hardware.CPU(rs); cpu != nil && cpu.Value >= cpuHotC {
		al.Announce("pod-hot", fmt.Sprintf("The Pod is running hot: %.0f°C. It slows itself down to protect its chips. Check that it has airflow and its fan is turning.", cpu.Value), 3*time.Hour, true)
	}
	temp, hum := hardware.Room(rs)
	if temp != nil {
		conditions["room-hot"] = temp.Value >= roomHotC
		conditions["room-cold"] = temp.Value <= roomColdC
	}
	if hum != nil {
		conditions["room-humid"] = hum.Value >= roomHumidPct
		conditions["room-dry"] = hum.Value <= roomDryPct
	}
	if temp != nil {
		switch {
		case temp.Value >= roomHotC:
			al.Announce("room-hot", fmt.Sprintf("It's %.1f°C in the room. That's hot enough to worry about, for you and for the Pod.", temp.Value), 6*time.Hour, false)
		case temp.Value <= roomColdC:
			al.Announce("room-cold", fmt.Sprintf("It's %.1f°C in the room. That's cold.", temp.Value), 6*time.Hour, false)
		}
	}
	if hum != nil {
		switch {
		case hum.Value >= roomHumidPct:
			al.Announce("room-humid", fmt.Sprintf("Humidity in the room is %.0f%%. That's damp enough for mould and hard on electronics.", hum.Value), 6*time.Hour, false)
		case hum.Value <= roomDryPct:
			al.Announce("room-dry", fmt.Sprintf("Humidity in the room is %.0f%%. The air is very dry.", hum.Value), 6*time.Hour, false)
		}
	}

	retract.observe(conditions, func(key string) { al.ResolveNotice(key) })

	if time.Since(*lastUpdateCheck) >= updateCheckGap {
		*lastUpdateCheck = time.Now()
		announceNewRelease(al)
	}
}

// announceNewRelease tells the owner a newer Ghost is out, once per version. It
// never installs anything: updating is the owner's decision.
func announceNewRelease(al *agent.AgentLoop) {
	if offline() {
		return
	}
	rel, err := latestRelease()
	if err != nil {
		return
	}
	installed := ghostVersion()
	if !appliance.IsNewer(rel.Version, installed) {
		return
	}
	text := fmt.Sprintf("A new version of Ghost is out: %s (you're on %s).", rel.Version, installed)
	if first := firstNoteLine(rel.Notes); first != "" {
		text += " What's new: " + first
	}
	text += "\n\nTo update, open **Your Pod** in the app, or run `ghost update` on the Pod."
	al.Announce("update:"+rel.Version, text, 30*24*time.Hour, false)
}

// noteHeadlineRE finds a bold headline: the sentence a release note leads with.
var noteHeadlineRE = regexp.MustCompile(`\*\*([^*]{8,160}?)\*\*`)

// firstNoteLine is the headline of the newest thing in the release notes: the
// bold lead of the first bullet ("Backups now back up the right Ghost, and can
// be restored."), or, for notes without one, the first plain sentence. It is
// always a whole sentence, never a fragment cut at a length limit.
func firstNoteLine(notes string) string {
	if m := noteHeadlineRE.FindStringSubmatch(notes); m != nil {
		return wholeSentence(m[1])
	}
	for _, ln := range strings.Split(notes, "\n") {
		ln = strings.TrimSpace(strings.TrimLeft(ln, "-*# "))
		ln = strings.NewReplacer("**", "", "`", "").Replace(ln)
		if ln == "" || strings.HasPrefix(strings.ToLower(ln), "release") {
			continue
		}
		if i := strings.IndexAny(ln, ".!?"); i > 0 {
			ln = ln[:i+1]
		}
		if r := []rune(ln); len(r) <= 200 {
			return wholeSentence(ln)
		}
	}
	return ""
}

// wholeSentence ends a headline with a full stop.
func wholeSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasSuffix(s, ".") || strings.HasSuffix(s, "!") || strings.HasSuffix(s, "?") {
		return s
	}
	return s + "."
}
