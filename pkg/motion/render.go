package motion

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/browser/native"
)

// FPS is the frame rate a motion renders at.
const FPS = 30

// Progress reports frames done of all, while a render runs.
type Progress func(done, total int)

// Render draws the motion frame by frame in a headless browser with the same
// player the phone uses, and encodes the frames into an H.264 MP4 at out.
// Every frame is the player at an exact t, so the video never drops or
// repeats a frame however slow the Pod is. It also ships two companions
// beside the MP4: a .srt of the beats (voice, else caption) and a soft
// tempo-locked procedural bed muxed in as the audio track — one motif that
// returns on the key idea, cues only where the spec names a sound.
func Render(ctx context.Context, s Spec, out string, progress Progress) error {
	if err := s.Check(); err != nil {
		return err
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return errors.New("ffmpeg is not installed on this Pod, so it can't make videos")
	}
	w, h := s.Frame()
	dir, err := os.MkdirTemp("", "ghost-motion-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	page := filepath.Join(dir, "motion.html")
	if err := os.WriteFile(page, []byte(Page(s, true)), 0o600); err != nil {
		return err
	}

	sess, err := native.Launch(ctx, native.Options{
		Headless: true,
		Viewport: [2]int{w, h},
		// No desktop keyring or session bus: on a desktop either can hold a
		// headless Chrome up indefinitely.
		Env: []string{"DBUS_SESSION_BUS_ADDRESS=disabled:"},
	})
	if err != nil {
		return fmt.Errorf("couldn't start the browser to draw it: %w", err)
	}
	defer sess.Close()
	if err := sess.SetViewport(ctx, w, h); err != nil {
		return err
	}
	if err := sess.Open(ctx, "file://"+page, 30*time.Second); err != nil {
		return err
	}
	if err := sess.Eval(ctx, "ghostMotion.ready.then(function(){return true})"); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	tmp := out + ".part.mp4"
	enc := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "image2pipe", "-framerate", fmt.Sprint(FPS), "-c:v", "mjpeg", "-i", "-",
		// Browser frames are full-range; phones expect video range.
		"-vf", "scale=in_range=full:out_range=tv,format=yuv420p", "-color_range", "tv",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-pix_fmt", "yuv420p",
		"-movflags", "+faststart", tmp)
	var stderr strings.Builder
	enc.Stderr = &stderr
	stdin, err := enc.StdinPipe()
	if err != nil {
		return err
	}
	if err := enc.Start(); err != nil {
		return err
	}
	frames := int(s.Duration()*FPS + 0.5)
	werr := func() error {
		defer stdin.Close()
		for i := 0; i <= frames; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			t := float64(i) / FPS
			if err := sess.Eval(ctx, fmt.Sprintf("ghostMotion.seek(%.5f)", t)); err != nil {
				return err
			}
			img, err := sess.CaptureJPEG(ctx, 92)
			if err != nil {
				return err
			}
			if _, err := stdin.Write(img); err != nil {
				return err
			}
			if progress != nil && (i%FPS == 0 || i == frames) {
				progress(i, frames)
			}
		}
		return nil
	}()
	if err := enc.Wait(); err != nil && werr == nil {
		werr = fmt.Errorf("encoding failed: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	if werr != nil {
		os.Remove(tmp)
		return werr
	}
	if err := os.Rename(tmp, out); err != nil {
		return err
	}
	// Companions live beside the MP4 and never fail the export on their own.
	_ = os.WriteFile(strings.TrimSuffix(out, ".mp4")+".srt", []byte(FormatSRT(s.Cues())), 0o644)
	muxBed(ctx, ffmpeg, s, out)
	return nil
}

// muxBed muxes a soft procedural bed into a rendered MP4: two low sines with
// a slow tremolo at ~92 BPM plus a short soft tick at each scene start that
// names a sound. Levels stay low with a limiter so true peak holds under
// -1.5 dBTP; when ffmpeg or the mux fails the silent video stands.
func muxBed(ctx context.Context, ffmpeg string, s Spec, out string) {
	dur := s.Duration()
	if dur <= 0 {
		return
	}
	starts := make([]float64, 0, len(s.Scenes))
	t := 0.0
	sounded := 0
	for _, sc := range s.Scenes {
		if strings.TrimSpace(sc.Sound) != "" {
			starts = append(starts, t+0.15)
			sounded++
		}
		t += sc.Duration
	}
	// One motif that returns on the key idea: always tick the payoff.
	if sounded == 0 && len(s.Scenes) > 0 {
		starts = append(starts, t*0.75)
	}
	jctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	tmp := out + ".sound.mp4"
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", out,
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=174:duration=%.2f", dur),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=261.63:duration=%.2f", dur),
	}
	for _, st := range starts {
		ms := int(st*1000 + 0.5)
		args = append(args, "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=880:duration=0.12"))
		_ = ms
	}
	// Build: bed = low mixed sines with tremolo; ticks delayed to their beats.
	var fc strings.Builder
	fc.WriteString("[1:a]volume=0.5[a0];[2:a]volume=0.32[a1];[a0][a1]amix=inputs=2:duration=longest:dropout_transition=0,volume=0.10,tremolo=f=1.53:d=0.4,alimiter=limit=0.84[bed];")
	idx := 3
	var mix []string
	mix = append(mix, "[bed]")
	for i, st := range starts {
		ms := int(st*1000 + 0.5)
		fmt.Fprintf(&fc, "[%d:a]volume=0.5,adelay=%d|%d[t%d];", idx, ms, ms, i)
		mix = append(mix, fmt.Sprintf("[t%d]", i))
		idx++
	}
	if len(starts) > 0 {
		fmt.Fprintf(&fc, "%samix=inputs=%d:duration=longest:dropout_transition=0,alimiter=limit=0.84[aout]", strings.Join(mix, ""), len(mix))
	} else {
		fc.WriteString("[bed]anull[aout]")
	}
	args = append(args, "-filter_complex", fc.String(), "-map", "0:v", "-map", "[aout]",
		"-c:v", "copy", "-c:a", "aac", "-b:a", "96k", "-movflags", "+faststart", "-shortest", tmp)
	cmd := exec.CommandContext(jctx, ffmpeg, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return
	}
	_ = os.Rename(tmp, out)
}
