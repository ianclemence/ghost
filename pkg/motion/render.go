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
// repeats a frame however slow the Pod is.
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
	return os.Rename(tmp, out)
}
