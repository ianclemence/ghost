// Package proto defines the binary frame protocol for the Ghost relay tunnel.
//
// The relay tunnel multiplexes HTTP request/response streams and WebSocket
// push streams over a single persistent WebSocket connection between a Ghost
// device and the relay server.
//
// Frame layout (13-byte header + payload):
//
//	[1 byte  kind] [8 bytes stream_id] [4 bytes length] [N bytes payload]
//
// Stream IDs are uint64 big-endian. Control messages use stream ID 0.
// Length is uint32 big-endian (max ~4 GB per frame; practical limit much smaller).
package proto

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/gorilla/websocket"
)

const (
	HeaderLen = 13 // 1 + 8 + 4

	KindCTL   byte = 0x01 // Control message (JSON payload, stream ID 0)
	KindOPEN  byte = 0x02 // Stream open (JSON metadata)
	KindDATA  byte = 0x03 // Stream data bytes
	KindEND   byte = 0x04 // Stream EOF (no payload)
	KindERROR byte = 0x05 // Stream error (UTF-8 reason)
	KindPING  byte = 0x06 // Heartbeat ping
	KindPONG  byte = 0x07 // Heartbeat pong

	MaxFramePayload = 1 << 20 // 1 MB per frame (SSE chunks, WS messages)
)

// Frame is a decoded relay protocol frame.
type Frame struct {
	Kind     byte
	StreamID uint64
	Payload  []byte
}

// Control messages (KindCTL, stream ID 0).

// ControlOp identifies the type of control message.
type ControlOp string

const (
	OpWelcome      ControlOp = "welcome"
	OpEnroll       ControlOp = "enroll"
	OpAddClients   ControlOp = "add_clients"
	OpRevokeClient ControlOp = "revoke_client"
	OpClientsOK    ControlOp = "clients_ok"
	OpHeartbeat    ControlOp = "heartbeat"
	OpError        ControlOp = "error"
)

// Control is the JSON payload of a CTL frame.
type Control struct {
	Op           ControlOp     `json:"op"`
	Version      int           `json:"version,omitempty"`
	DeviceID     string        `json:"device_id,omitempty"`
	DeviceSecret string        `json:"device_secret,omitempty"`
	EnrollToken  string        `json:"enroll_token,omitempty"`
	Clients      []ClientEntry `json:"clients,omitempty"`
	TokenHash    string        `json:"token_hash,omitempty"`
	OK           bool          `json:"ok,omitempty"`
	Message      string        `json:"message,omitempty"`
}

// ClientEntry is a allowed client token entry.
type ClientEntry struct {
	TokenHash string `json:"token_hash"`
	Name      string `json:"name,omitempty"`
}

// Stream metadata for OPEN frames.

// StreamType identifies the type of stream.
type StreamType string

const (
	StreamHTTP StreamType = "http"
	StreamWS   StreamType = "ws"
)

// HTTPMetadata is the JSON payload of an OPEN frame for an HTTP stream.
type HTTPMetadata struct {
	Type    StreamType          `json:"type"`
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Query   string              `json:"query,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
}

// HTTPResponseMeta is the JSON payload of an OPEN frame for an HTTP response.
type HTTPResponseMeta struct {
	Type    StreamType          `json:"type"`
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers,omitempty"`
}

// WSMetadata is the JSON payload of an OPEN frame for a WS stream.
type WSMetadata struct {
	Type StreamType `json:"type"`
}

// WSReadyMeta is sent by the device when its local WS connection is established.
type WSReadyMeta struct {
	Type StreamType `json:"type"`
}

// WriteFrame writes a single frame to w.
func WriteFrame(w io.Writer, f *Frame) error {
	if len(f.Payload) > MaxFramePayload {
		return fmt.Errorf("payload too large: %d > %d", len(f.Payload), MaxFramePayload)
	}
	var hdr [HeaderLen]byte
	hdr[0] = f.Kind
	binary.BigEndian.PutUint64(hdr[1:9], f.StreamID)
	binary.BigEndian.PutUint32(hdr[9:13], uint32(len(f.Payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(f.Payload) > 0 {
		if _, err := w.Write(f.Payload); err != nil {
			return err
		}
	}
	return nil
}

// ReadFrame reads a single frame from r.
func ReadFrame(r io.Reader) (*Frame, error) {
	var hdr [HeaderLen]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	kind := hdr[0]
	streamID := binary.BigEndian.Uint64(hdr[1:9])
	length := binary.BigEndian.Uint32(hdr[9:13])

	if length > MaxFramePayload {
		return nil, fmt.Errorf("frame too large: %d bytes", length)
	}

	var payload []byte
	if length > 0 {
		payload = make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
	}

	return &Frame{Kind: kind, StreamID: streamID, Payload: payload}, nil
}

// WriteCTL writes a control frame.
func WriteCTL(w io.Writer, streamID uint64, ctrl *Control) error {
	data, err := json.Marshal(ctrl)
	if err != nil {
		return fmt.Errorf("marshal control: %w", err)
	}
	return WriteFrame(w, &Frame{Kind: KindCTL, StreamID: streamID, Payload: data})
}

// WriteOPEN writes an OPEN frame with JSON metadata.
func WriteOPEN(w io.Writer, streamID uint64, meta interface{}) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal open metadata: %w", err)
	}
	return WriteFrame(w, &Frame{Kind: KindOPEN, StreamID: streamID, Payload: data})
}

// WriteDATA writes a DATA frame.
func WriteDATA(w io.Writer, streamID uint64, payload []byte) error {
	return WriteFrame(w, &Frame{Kind: KindDATA, StreamID: streamID, Payload: payload})
}

// WriteEND writes an END frame (no payload).
func WriteEND(w io.Writer, streamID uint64) error {
	return WriteFrame(w, &Frame{Kind: KindEND, StreamID: streamID})
}

// WriteERROR writes an ERROR frame with a reason string.
func WriteERROR(w io.Writer, streamID uint64, reason string) error {
	return WriteFrame(w, &Frame{Kind: KindERROR, StreamID: streamID, Payload: []byte(reason)})
}

// ParseControl parses the JSON payload of a CTL frame.
func ParseControl(f *Frame) (*Control, error) {
	if f.Kind != KindCTL {
		return nil, fmt.Errorf("not a CTL frame: kind=%d", f.Kind)
	}
	var ctrl Control
	if err := json.Unmarshal(f.Payload, &ctrl); err != nil {
		return nil, fmt.Errorf("parse control: %w", err)
	}
	return &ctrl, nil
}

// ParseHTTPMeta parses the JSON payload of an OPEN frame for HTTP.
func ParseHTTPMeta(f *Frame) (*HTTPMetadata, error) {
	if f.Kind != KindOPEN {
		return nil, fmt.Errorf("not an OPEN frame: kind=%d", f.Kind)
	}
	var meta HTTPMetadata
	if err := json.Unmarshal(f.Payload, &meta); err != nil {
		return nil, fmt.Errorf("parse http metadata: %w", err)
	}
	return &meta, nil
}

// ParseHTTPResponseMeta parses the JSON payload of an OPEN frame for HTTP response.
func ParseHTTPResponseMeta(f *Frame) (*HTTPResponseMeta, error) {
	if f.Kind != KindOPEN {
		return nil, fmt.Errorf("not an OPEN frame: kind=%d", f.Kind)
	}
	var meta HTTPResponseMeta
	if err := json.Unmarshal(f.Payload, &meta); err != nil {
		return nil, fmt.Errorf("parse http response metadata: %w", err)
	}
	return &meta, nil
}

// ParseWSMeta parses the JSON payload of an OPEN frame for WS.
func ParseWSMeta(f *Frame) (*WSMetadata, error) {
	if f.Kind != KindOPEN {
		return nil, fmt.Errorf("not an OPEN frame: kind=%d", f.Kind)
	}
	var meta WSMetadata
	if err := json.Unmarshal(f.Payload, &meta); err != nil {
		return nil, fmt.Errorf("parse ws metadata: %w", err)
	}
	return &meta, nil
}

// NextStreamID returns the next valid stream ID. Stream IDs start at 1
// and increment by 2 (odd for device-initiated, even for relay-initiated).
func NextStreamID(current uint64) uint64 {
	if current == 0 {
		return 1
	}
	next := current + 2
	if next == 0 || next > math.MaxUint64-2 {
		return 1
	}
	return next
}

// Relay protocol version.
const Version = 1

// --- WebSocket-specific helpers ---
// These encode a frame into a single WebSocket binary message, since each
// WS message = one relay frame. This is cleaner than io.Reader/io.Writer
// adapters because WebSocket messages are discrete, not streaming.

// EncodeFrame serializes a frame into its full wire representation
// (13-byte header + payload) as a single byte slice. Callers that funnel
// frames through a single-writer pump use this to prepare atomic writes.
func EncodeFrame(f *Frame) ([]byte, error) {
	if len(f.Payload) > MaxFramePayload {
		return nil, fmt.Errorf("payload too large: %d > %d", len(f.Payload), MaxFramePayload)
	}
	msg := make([]byte, HeaderLen+len(f.Payload))
	msg[0] = f.Kind
	binary.BigEndian.PutUint64(msg[1:9], f.StreamID)
	binary.BigEndian.PutUint32(msg[9:13], uint32(len(f.Payload)))
	copy(msg[HeaderLen:], f.Payload)
	return msg, nil
}

// WriteFrameWS writes a frame as a single WebSocket binary message.
//
// Only one goroutine may call this at a time (gorilla requirement). Control
// frames (ping/pong) should use conn.WriteControl, which is safe concurrently.
func WriteFrameWS(conn *websocket.Conn, f *Frame) error {
	msg, err := EncodeFrame(f)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, msg)
}

// ReadFrameWS reads a frame from a single WebSocket binary message.
func ReadFrameWS(conn *websocket.Conn) (*Frame, error) {
	msgType, msg, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	if msgType != websocket.BinaryMessage {
		return nil, fmt.Errorf("expected binary message, got type %d", msgType)
	}
	if len(msg) < HeaderLen {
		return nil, fmt.Errorf("frame too short: %d bytes", len(msg))
	}
	kind := msg[0]
	streamID := binary.BigEndian.Uint64(msg[1:9])
	length := binary.BigEndian.Uint32(msg[9:13])
	if int(length) != len(msg)-HeaderLen {
		return nil, fmt.Errorf("length mismatch: header says %d, got %d payload bytes", length, len(msg)-HeaderLen)
	}
	var payload []byte
	if length > 0 {
		payload = make([]byte, length)
		copy(payload, msg[HeaderLen:])
	}
	return &Frame{Kind: kind, StreamID: streamID, Payload: payload}, nil
}

// WriteCTLWS writes a control frame over WebSocket.
func WriteCTLWS(conn *websocket.Conn, streamID uint64, ctrl *Control) error {
	data, err := json.Marshal(ctrl)
	if err != nil {
		return fmt.Errorf("marshal control: %w", err)
	}
	return WriteFrameWS(conn, &Frame{Kind: KindCTL, StreamID: streamID, Payload: data})
}

// WriteOPENWS writes an OPEN frame over WebSocket.
func WriteOPENWS(conn *websocket.Conn, streamID uint64, meta interface{}) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal open metadata: %w", err)
	}
	return WriteFrameWS(conn, &Frame{Kind: KindOPEN, StreamID: streamID, Payload: data})
}

// WriteDATAWS writes a DATA frame over WebSocket.
func WriteDATAWS(conn *websocket.Conn, streamID uint64, payload []byte) error {
	return WriteFrameWS(conn, &Frame{Kind: KindDATA, StreamID: streamID, Payload: payload})
}

// WriteENDWS writes an END frame over WebSocket.
func WriteENDWS(conn *websocket.Conn, streamID uint64) error {
	return WriteFrameWS(conn, &Frame{Kind: KindEND, StreamID: streamID})
}

// WriteERRORWS writes an ERROR frame over WebSocket.
func WriteERRORWS(conn *websocket.Conn, streamID uint64, reason string) error {
	return WriteFrameWS(conn, &Frame{Kind: KindERROR, StreamID: streamID, Payload: []byte(reason)})
}

// SetWriteDeadline sets the write deadline on the WebSocket connection.
func SetWriteDeadline(conn *websocket.Conn, t time.Time) error {
	return conn.SetWriteDeadline(t)
}
