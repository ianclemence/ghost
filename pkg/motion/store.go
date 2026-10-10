package motion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// A motion is kept like a canvas: each change is the next version,
// motion/<name>-v<N>.json in the workspace, and its video, once made, sits
// beside it as motion/<name>-v<N>.mp4.

var (
	nonSlug   = regexp.MustCompile(`[^a-z0-9]+`)
	versioned = regexp.MustCompile(`^motion/([a-z0-9-]+)-v(\d+)\.json$`)
)

func slug(title string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(title)), "-"), "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	if s == "" {
		return "motion"
	}
	return s
}

// Save keeps the spec as the next version of the motion with its title and
// returns its workspace path and version.
func Save(workspace string, s Spec) (string, int, error) {
	if err := s.Check(); err != nil {
		return "", 0, err
	}
	dir := filepath.Join(workspace, "motion")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	name := slug(s.Title)
	v := 1
	matches, _ := filepath.Glob(filepath.Join(dir, name+"-v*.json"))
	for _, m := range matches {
		if sub := versioned.FindStringSubmatch("motion/" + filepath.Base(m)); sub != nil && sub[1] == name {
			var n int
			fmt.Sscanf(sub[2], "%d", &n)
			if n >= v {
				v = n + 1
			}
		}
	}
	rel := fmt.Sprintf("motion/%s-v%d.json", name, v)
	b, _ := json.MarshalIndent(s, "", " ")
	if err := os.WriteFile(filepath.Join(workspace, rel), b, 0o644); err != nil {
		return "", 0, err
	}
	return rel, v, nil
}

// IsMotion reports whether a workspace path is a saved motion.
func IsMotion(rel string) bool { return versioned.MatchString(filepath.ToSlash(rel)) }

// Load reads a saved motion.
func Load(workspace, rel string) (Spec, error) {
	if !IsMotion(rel) {
		return Spec{}, errors.New("not a motion")
	}
	b, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(rel)))
	if err != nil {
		return Spec{}, err
	}
	return Parse(b)
}

// Latest is the newest version of the motion with this title, if any.
func Latest(workspace, title string) (string, bool) {
	name := slug(title)
	matches, _ := filepath.Glob(filepath.Join(workspace, "motion", name+"-v*.json"))
	best, bestV := "", 0
	for _, m := range matches {
		rel := "motion/" + filepath.Base(m)
		if sub := versioned.FindStringSubmatch(rel); sub != nil && sub[1] == name {
			var n int
			fmt.Sscanf(sub[2], "%d", &n)
			if n > bestV {
				best, bestV = rel, n
			}
		}
	}
	return best, best != ""
}

// VideoPath is where a motion's video is kept.
func VideoPath(rel string) string { return strings.TrimSuffix(rel, ".json") + ".mp4" }

var videoOf = regexp.MustCompile(`^motion/[a-z0-9-]+-v\d+\.mp4$`)

// MotionOfVideo is the motion a video was made from (motion/x-v2.mp4 →
// motion/x-v2.json), when the path is a motion's video.
func MotionOfVideo(rel string) (string, bool) {
	rel = filepath.ToSlash(rel)
	if !videoOf.MatchString(rel) {
		return "", false
	}
	return strings.TrimSuffix(rel, ".mp4") + ".json", true
}

// Job is one video being made.
type Job struct {
	Motion string `json:"motion"`
	State  string `json:"state"` // queued | rendering | done | failed
	Done   int    `json:"done"`
	Total  int    `json:"total"`
	Error  string `json:"error,omitempty"`
	Video  string `json:"video,omitempty"`
}

// Exports makes videos one at a time (a Pod has one browser's worth of room)
// and remembers how each went.
type Exports struct {
	mu   sync.Mutex
	jobs map[string]*Job
	sem  chan struct{}
}

func NewExports() *Exports { return &Exports{jobs: map[string]*Job{}, sem: make(chan struct{}, 1)} }

// Start makes the video for the motion at rel, unless it is already made or
// being made. done is called once, when it is.
func (x *Exports) Start(workspace, rel string, done func(j Job)) (Job, error) {
	s, err := Load(workspace, rel)
	if err != nil {
		return Job{}, err
	}
	x.mu.Lock()
	if j, ok := x.jobs[rel]; ok && (j.State == "queued" || j.State == "rendering") {
		out := *j
		x.mu.Unlock()
		return out, nil
	}
	video := VideoPath(rel)
	if fi, err := os.Stat(filepath.Join(workspace, video)); err == nil && fi.Size() > 0 {
		j := &Job{Motion: rel, State: "done", Video: video}
		x.jobs[rel] = j
		x.mu.Unlock()
		return *j, nil
	}
	j := &Job{Motion: rel, State: "queued", Total: int(s.Duration()*FPS + 0.5)}
	x.jobs[rel] = j
	x.mu.Unlock()
	go func() {
		x.sem <- struct{}{}
		defer func() { <-x.sem }()
		x.set(rel, func(j *Job) { j.State = "rendering" })
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		err := Render(ctx, s, filepath.Join(workspace, video), func(d, t int) {
			x.set(rel, func(j *Job) { j.Done, j.Total = d, t })
		})
		x.set(rel, func(j *Job) {
			if err != nil {
				j.State, j.Error = "failed", err.Error()
			} else {
				j.State, j.Video, j.Done = "done", video, j.Total
			}
		})
		if done != nil {
			done(x.Status(rel))
		}
	}()
	return x.Status(rel), nil
}

func (x *Exports) set(rel string, fn func(j *Job)) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if j, ok := x.jobs[rel]; ok {
		fn(j)
	}
}

// Status is how the video for rel is going (state "none" if never asked).
func (x *Exports) Status(rel string) Job {
	x.mu.Lock()
	defer x.mu.Unlock()
	if j, ok := x.jobs[rel]; ok {
		return *j
	}
	return Job{Motion: rel, State: "none"}
}

// Versions lists the saved versions of a motion, oldest first.
func Versions(workspace, title string) []string {
	name := slug(title)
	matches, _ := filepath.Glob(filepath.Join(workspace, "motion", name+"-v*.json"))
	var out []string
	for _, m := range matches {
		rel := "motion/" + filepath.Base(m)
		if sub := versioned.FindStringSubmatch(rel); sub != nil && sub[1] == name {
			out = append(out, rel)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		var a, b int
		fmt.Sscanf(versioned.FindStringSubmatch(out[i])[2], "%d", &a)
		fmt.Sscanf(versioned.FindStringSubmatch(out[j])[2], "%d", &b)
		return a < b
	})
	return out
}

var (
	exportsMu sync.Mutex
	exportsBy = map[string]*Exports{}
)

// ExportsFor is the one video maker of a workspace, shared by the tool and the
// API so a video asked for twice is made once.
func ExportsFor(workspace string) *Exports {
	exportsMu.Lock()
	defer exportsMu.Unlock()
	if x, ok := exportsBy[workspace]; ok {
		return x
	}
	x := NewExports()
	exportsBy[workspace] = x
	return x
}
