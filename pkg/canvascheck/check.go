package canvascheck

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/browser/native"
)

const (
	phoneWidth  = 390
	phoneHeight = 844
	overall     = 25 * time.Second
	settle      = 700 * time.Millisecond
	taps        = 4
	minTarget   = 40
)

// harness runs before the page's own script. It records what the page throws
// and makes the page's world match the phone's sandbox: no storage, and the
// ghost.save bridge.
const harness = `<script>(function(){
window.__errs=[];
function note(m){if(window.__errs.length<8)window.__errs.push(String(m||'error').slice(0,200))}
window.addEventListener('error',function(e){note(e.message||(e.error&&e.error.message))});
window.addEventListener('unhandledrejection',function(e){note((e.reason&&e.reason.message)||e.reason)});
var ce=console.error;console.error=function(){try{note(Array.prototype.join.call(arguments,' '))}catch(x){}return ce&&ce.apply(console,arguments)};
['localStorage','sessionStorage','indexedDB'].forEach(function(k){try{Object.defineProperty(window,k,{get:function(){throw new DOMException('storage is not available here','SecurityError')}})}catch(x){}});
try{Object.defineProperty(document,'cookie',{get:function(){throw new DOMException('cookies are not available here','SecurityError')},set:function(){throw new DOMException('cookies are not available here','SecurityError')}})}catch(x){}
window.ghost={saved:null,save:function(){return true}};
})();</script>`

var (
	headOpen = regexp.MustCompile(`(?i)<head(\s[^>]*)?>`)
	htmlOpen = regexp.MustCompile(`(?i)<html(\s[^>]*)?>`)
)

// Instrument places the harness ahead of everything the page does.
func Instrument(page string) string {
	if headOpen.MatchString(page) {
		return headOpen.ReplaceAllStringFunc(page, func(m string) string { return m + harness })
	}
	if htmlOpen.MatchString(page) {
		return htmlOpen.ReplaceAllStringFunc(page, func(m string) string { return m + "<head>" + harness + "</head>" })
	}
	return "<!doctype html><html><head><meta charset=\"utf-8\">" + harness + "</head><body>" + page + "</body></html>"
}

const measure = `(function(){
var out={errs:window.__errs||[],sw:document.documentElement.scrollWidth,iw:window.innerWidth,text:((document.body&&document.body.innerText)||'').trim().length,small:[],tiny:0};
document.querySelectorAll('button,a[href],input:not([type=hidden]),select,textarea,[role=button]').forEach(function(e){
 var r=e.getBoundingClientRect();
 if(r.width>0&&r.height>0&&(r.height<%d||r.width<%d)&&out.small.length<5){out.small.push((e.tagName.toLowerCase()+' '+((e.innerText||e.getAttribute('aria-label')||e.type||'')+'').trim().slice(0,24)).trim())}
});
document.querySelectorAll('p,li,span,td,th,label,a,button,small').forEach(function(e){
 var t=(e.innerText||'').trim(); if(!t||e.children.length>0)return;
 if(parseFloat(getComputedStyle(e).fontSize)<12)out.tiny++
});
return JSON.stringify(out)})()`

const tap = `(function(n){
var bs=Array.prototype.filter.call(document.querySelectorAll('button,[role=button]'),function(b){var r=b.getBoundingClientRect();return r.width>0&&r.height>0&&!b.disabled});
var b=bs[n]; if(!b)return JSON.stringify({done:true});
var label=((b.innerText||b.getAttribute('aria-label')||'button')+'').trim().slice(0,24);
var before=window.__errs.length; try{b.click()}catch(x){window.__errs.push(String(x.message||x))}
return JSON.stringify({label:label,errs:window.__errs.slice(before)})})(%d)`

type measured struct {
	Errs  []string `json:"errs"`
	SW    int      `json:"sw"`
	IW    int      `json:"iw"`
	Text  int      `json:"text"`
	Small []string `json:"small"`
	Tiny  int      `json:"tiny"`
}

type tapped struct {
	Done  bool     `json:"done"`
	Label string   `json:"label"`
	Errs  []string `json:"errs"`
}

// Findings turns one measurement, and what tapping turned up, into the
// problems worth telling the page's author. Facts only, worst first.
func Findings(m measured, taps []tapped) []string {
	var out []string
	for _, e := range m.Errs {
		out = append(out, "it throws on load: "+e)
	}
	for _, t := range taps {
		for _, e := range t.Errs {
			out = append(out, fmt.Sprintf("tapping %q throws: %s", t.Label, e))
		}
	}
	if m.Text < 20 {
		out = append(out, "it renders almost nothing: no visible text")
	}
	if m.SW > m.IW+2 {
		out = append(out, fmt.Sprintf("it is %dpx wider than a %dpx phone screen, so it scrolls sideways", m.SW-m.IW, m.IW))
	}
	if len(m.Small) > 0 {
		out = append(out, fmt.Sprintf("tap targets under %dpx: %s", minTarget, strings.Join(m.Small, ", ")))
	}
	if m.Tiny > 0 {
		out = append(out, fmt.Sprintf("%d text elements are under 12px", m.Tiny))
	}
	return out
}

// Check opens the page in a headless browser at phone size, taps its buttons,
// and reports what is wrong with it. An empty result means it looked fine,
// or the browser could not be started: a check that cannot run says nothing.
func Check(ctx context.Context, page string) []string {
	ctx, cancel := context.WithTimeout(ctx, overall)
	defer cancel()
	dir, err := os.MkdirTemp("", "ghost-canvas-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "page.html")
	if err := os.WriteFile(file, []byte(Instrument(page)), 0o600); err != nil {
		return nil
	}
	sess, err := native.Launch(ctx, native.Options{
		Headless: true,
		Viewport: [2]int{phoneWidth, phoneHeight},
		Env:      []string{"DBUS_SESSION_BUS_ADDRESS=disabled:"},
	})
	if err != nil {
		return nil
	}
	defer sess.Close()
	if sess.SetViewport(ctx, phoneWidth, phoneHeight) != nil || sess.Open(ctx, "file://"+file, 15*time.Second) != nil {
		return nil
	}
	time.Sleep(settle)
	raw, err := sess.EvalValue(ctx, fmt.Sprintf(measure, minTarget, minTarget))
	if err != nil {
		return nil
	}
	var m measured
	if json.Unmarshal(unquote(raw), &m) != nil {
		return nil
	}
	var made []tapped
	for i := 0; i < taps; i++ {
		raw, err := sess.EvalValue(ctx, fmt.Sprintf(tap, i))
		if err != nil {
			break
		}
		var t tapped
		if json.Unmarshal(unquote(raw), &t) != nil || t.Done {
			break
		}
		made = append(made, t)
		time.Sleep(150 * time.Millisecond)
	}
	return Findings(m, made)
}

// unquote unwraps a JSON string that itself holds JSON.
func unquote(raw json.RawMessage) []byte {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []byte(s)
	}
	return raw
}
