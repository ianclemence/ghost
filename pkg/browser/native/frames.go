package native

import (
	"context"
	"encoding/base64"
	"errors"
	"time"
)

// What rendering a page frame by frame needs (a motion into a video): a fixed
// viewport, a page opened without the accessibility snapshot the agent uses,
// script that can be awaited, and frames captured as JPEG bytes.

// SetViewport fixes the page's size in CSS pixels at a device scale of one.
func (s *Session) SetViewport(ctx context.Context, w, h int) error {
	return s.cdp.send(ctx, "Emulation.setDeviceMetricsOverride", map[string]interface{}{
		"width": w, "height": h, "deviceScaleFactor": 1, "mobile": false,
	}, nil)
}

// Open loads a URL and waits for its load event (up to timeout).
func (s *Session) Open(ctx context.Context, url string, timeout time.Duration) error {
	loaded := s.cdp.on("Page.loadEventFired")
	if err := s.cdp.send(ctx, "Page.navigate", map[string]interface{}{"url": url}, nil); err != nil {
		return err
	}
	select {
	case <-loaded:
		return nil
	case <-time.After(timeout):
		return errors.New("the page did not load")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Eval runs script in the page, awaiting it when it returns a promise, and
// reports a script error as an error.
func (s *Session) Eval(ctx context.Context, expr string) error {
	var res struct {
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := s.cdp.send(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression": expr, "returnByValue": true, "awaitPromise": true,
	}, &res); err != nil {
		return err
	}
	if res.ExceptionDetails != nil {
		return errors.New("script: " + res.ExceptionDetails.Text)
	}
	return nil
}

// CaptureJPEG returns the page as it is now, as JPEG bytes.
func (s *Session) CaptureJPEG(ctx context.Context, quality int) ([]byte, error) {
	var res struct {
		Data string `json:"data"`
	}
	if err := s.cdp.send(ctx, "Page.captureScreenshot", map[string]interface{}{
		"format": "jpeg", "quality": quality, "fromSurface": true,
	}, &res); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(res.Data)
}
