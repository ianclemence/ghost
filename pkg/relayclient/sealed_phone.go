package relayclient

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/relaycrypto"
)

// SealedResponse is a response that crossed the relay sealed.
type SealedResponse struct {
	Status  int
	Headers map[string][]string
	Body    []byte
}

// ErrTruncated means the response ended without its closing message: the relay
// (or the network) cut it short, and what arrived must not be trusted as whole.
var ErrTruncated = errors.New("relayclient: sealed response was cut short")

// DoSealed is the phone's side of a sealed exchange, as a reference for the app
// and a tool for tests and the phone simulator. podPublic is the key pinned
// when the phone paired.
func DoSealed(relayURL, ghostID, clientToken string, podPublic []byte, req SealedRequest) (*SealedResponse, error) {
	req.Token = clientToken
	if req.Timestamp == 0 {
		req.Timestamp = time.Now().UnixMilli()
	}
	if req.Method == "" {
		req.Method = http.MethodGet
	}
	return doSealedRaw(relayURL, ghostID, clientToken, podPublic, req)
}

func doSealedRaw(relayURL, ghostID, clientToken string, podPublic []byte, req SealedRequest) (*SealedResponse, error) {
	plain, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	sess, env, err := relaycrypto.SealRequest(podPublic, plain)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequest(http.MethodPost, strings.TrimRight(relayURL, "/")+"/v1/sealed", bytes.NewReader(env))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("X-Ghost-Client-Id", ghostID)
	hreq.Header.Set("X-Ghost-Client-Token", clientToken)
	hreq.Header.Set("Content-Type", "application/octet-stream")
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(hreq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("relay answered %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	out := &SealedResponse{}
	var pending []byte
	buf := make([]byte, 32*1024)
	gotHead, gotEnd := false, false
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			var frames [][]byte
			frames, pending = relaycrypto.SplitFrames(append(pending, buf[:n]...))
			for _, f := range frames {
				kind, content, err := sess.OpenMessage(f)
				if err != nil {
					return nil, err
				}
				switch kind {
				case relaycrypto.MsgHead:
					var h SealedHead
					if err := json.Unmarshal(content, &h); err != nil {
						return nil, err
					}
					out.Status, out.Headers, gotHead = h.Status, h.Headers, true
				case relaycrypto.MsgData:
					out.Body = append(out.Body, content...)
				case relaycrypto.MsgEnd:
					gotEnd = true
				case relaycrypto.MsgErr:
					return nil, fmt.Errorf("the Pod could not answer: %s", content)
				}
			}
		}
		if rerr != nil {
			break
		}
	}
	if !gotHead || !gotEnd || len(pending) != 0 {
		return nil, ErrTruncated
	}
	return out, nil
}

// EncodeBody is the base64 the sealed request carries a body in.
func EncodeBody(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
