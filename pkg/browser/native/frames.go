package native

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"sync"
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

// EvalValue runs script in the page and returns its JSON-serialisable result.
func (s *Session) EvalValue(ctx context.Context, expr string) (json.RawMessage, error) {
	var res struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := s.cdp.send(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression": expr, "returnByValue": true, "awaitPromise": true,
	}, &res); err != nil {
		return nil, err
	}
	if res.ExceptionDetails != nil {
		return nil, errors.New("script: " + res.ExceptionDetails.Text)
	}
	return res.Result.Value, nil
}

// WatchErrors starts collecting what goes wrong in the page: uncaught
// exceptions and console errors, each as one line with where it happened.
// Call it before Open so a failure while the page loads is not missed. The
// returned function gives what has been seen so far.
func (s *Session) WatchErrors(ctx context.Context) (func() []string, error) {
	if err := s.cdp.send(ctx, "Runtime.enable", nil, nil); err != nil {
		return nil, err
	}
	exc := s.cdp.on("Runtime.exceptionThrown")
	con := s.cdp.on("Runtime.consoleAPICalled")
	var (
		mu   sync.Mutex
		seen []string
	)
	add := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		mu.Lock()
		if len(seen) < 20 {
			seen = append(seen, line)
		}
		mu.Unlock()
	}
	go func() {
		for exc != nil || con != nil {
			select {
			case ev, ok := <-exc:
				if !ok {
					exc = nil
					continue
				}
				var p struct {
					ExceptionDetails struct {
						Text       string `json:"text"`
						LineNumber int    `json:"lineNumber"`
						URL        string `json:"url"`
						Exception  struct {
							Description string `json:"description"`
						} `json:"exception"`
					} `json:"exceptionDetails"`
				}
				if json.Unmarshal(ev.Params, &p) != nil {
					continue
				}
				d := p.ExceptionDetails
				msg := d.Exception.Description
				if i := strings.IndexByte(msg, '\n'); i >= 0 {
					msg = msg[:i]
				}
				if msg == "" {
					msg = d.Text
				}
				where := ""
				if d.URL != "" {
					where = " (" + path.Base(d.URL) + " line " + strconv.Itoa(d.LineNumber+1) + ")"
				}
				add(msg + where)
			case ev, ok := <-con:
				if !ok {
					con = nil
					continue
				}
				var p struct {
					Type string `json:"type"`
					Args []struct {
						Value       interface{} `json:"value"`
						Description string      `json:"description"`
					} `json:"args"`
				}
				if json.Unmarshal(ev.Params, &p) != nil || p.Type != "error" {
					continue
				}
				var parts []string
				for _, a := range p.Args {
					if a.Value != nil {
						parts = append(parts, fmt.Sprint(a.Value))
					} else if a.Description != "" {
						parts = append(parts, a.Description)
					}
				}
				add(strings.Join(parts, " "))
			}
		}
	}()
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}, nil
}
