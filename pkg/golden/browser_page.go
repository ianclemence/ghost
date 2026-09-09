package golden

import (
	"fmt"
	"net"
	"net/http"
	"sync"
)

// A deterministic, local browser test page served by the Golden run itself
// (never an external website). Exercises navigation, observation, typing,
// clicking, submission, and post-submit observation.

const browserPageHTML = `<!doctype html><html><head><title>Ghost Browser Test</title></head>
<body>
<h1>Ghost Browser Test</h1>
<p>Please enter your name below and press the Continue button.</p>
<p>Name: <input id="name" type="text"></p>
<button id="continue">Continue</button>
<p id="status">Status: idle</p>
<script>
document.getElementById('continue').addEventListener('click', function(){
  var n = document.getElementById('name').value;
  document.getElementById('status').textContent = 'Status: submitted (' + n + ')';
  document.title = 'Submitted';
});
</script>
</body></html>`

const browserPagePort = 8931

// BrowserPageURL is the canonical local page for Browser Golden cases.
var BrowserPageURL = fmt.Sprintf("http://127.0.0.1:%d/", browserPagePort)

var (
	browserPageOnce sync.Once
	browserPageErr  error
)

// ensureBrowserPage starts the local deterministic page server once per
// process. Idempotent; safe to call from fixtures and tests.
func ensureBrowserPage() error {
	browserPageOnce.Do(func() {
		// Reuse an already-listening server (e.g. a previous run or an
		// externally started one on the same port).
		if conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", browserPagePort)); err == nil {
			conn.Close()
			return
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(browserPageHTML))
		})
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", browserPagePort))
		if err != nil {
			browserPageErr = err
			return
		}
		go func() { _ = http.Serve(ln, mux) }()
	})
	return browserPageErr
}
