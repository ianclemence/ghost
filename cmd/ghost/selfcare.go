package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/appliance"
	"github.com/ianclemence/ghost/pkg/awareness"
	"github.com/ianclemence/ghost/pkg/channels"
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
	cpuHotC        = 80.0
	roomHotC       = 33.0
	roomColdC      = 8.0
	roomHumidPct   = 75.0
	roomDryPct     = 20.0
	selfCareEvery  = 5 * time.Minute
	updateCheckGap = 6 * time.Hour
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
	snap := hardware.Snapshot(workspace)
	if snap.Storage == hardware.PressureCritical {
		al.Announce("storage-critical", fmt.Sprintf("I'm almost out of storage: %d GB left on the Pod. When it fills up I can't save memory or files. Clearing old downloads and backups would fix it.", snap.DiskFreeGB), 12*time.Hour, true)
	}
	if snap.Memory == hardware.PressureCritical {
		al.Announce("memory-critical", fmt.Sprintf("I'm short on memory (%d MB free of %d MB), so I may be slow or restart. If this keeps happening, something on the Pod is using too much.", snap.MemAvailableMB, snap.MemTotalMB), 12*time.Hour, false)
	}

	rs := hardware.ReadEnvironment("")
	if cpu := hardware.CPU(rs); cpu != nil && cpu.Value >= cpuHotC {
		al.Announce("pod-hot", fmt.Sprintf("The Pod is running hot: %.0f°C. It slows itself down to protect its chips. Check that it has airflow and its fan is turning.", cpu.Value), 3*time.Hour, true)
	}
	temp, hum := hardware.Room(rs)
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
	first := firstNoteLine(rel.Notes)
	text := fmt.Sprintf("Ghost %s is out (you have %s).", rel.Version, installed)
	if first != "" {
		text += " " + first
	}
	text += " Update from Your Pod in the app, or run `ghost update`."
	al.Announce("update:"+rel.Version, text, 30*24*time.Hour, false)
}

// firstNoteLine takes the first sentence-sized piece of release notes.
func firstNoteLine(notes string) string {
	for _, ln := range strings.Split(plainNotes(notes), "\n") {
		ln = strings.TrimSpace(strings.TrimLeft(ln, "-*# "))
		if ln == "" || strings.HasPrefix(strings.ToLower(ln), "release") {
			continue
		}
		if r := []rune(ln); len(r) > 160 {
			ln = string(r[:160]) + "…"
		}
		return ln
	}
	return ""
}
