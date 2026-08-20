package server

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	wsTextMessage  = 0x1
	wsCloseMessage = 0x8
	wsPingMessage  = 0x9
	wsPongMessage  = 0xA
	websocketGUID  = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

type wsConn struct {
	conn      net.Conn
	rw        *bufio.ReadWriter
	writeMu   sync.Mutex
	closeOnce sync.Once
	readLimit int64
}

func upgradeWebSocket(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if !headerContainsToken(r.Header, "Connection", "upgrade") ||
		!strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return nil, errors.New("request is not a websocket upgrade")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		return nil, errors.New("unsupported websocket version")
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	decodedKey, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decodedKey) != 16 {
		return nil, errors.New("invalid websocket key")
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("http server does not support connection hijacking")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}

	accept := websocketAccept(key)
	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := rw.WriteString(response); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return &wsConn{conn: conn, rw: rw, readLimit: 4096}, nil
}

func websocketAccept(key string) string {
	hash := sha1.Sum([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(hash[:])
}

func headerContainsToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func (c *wsConn) SetReadLimit(limit int64) {
	c.readLimit = limit
}

func (c *wsConn) SetReadDeadline(deadline time.Time) error {
	return c.conn.SetReadDeadline(deadline)
}

func (c *wsConn) SetWriteDeadline(deadline time.Time) error {
	return c.conn.SetWriteDeadline(deadline)
}

func (c *wsConn) ReadJSON(target any) error {
	for {
		opcode, payload, err := c.readFrame()
		if err != nil {
			return err
		}
		switch opcode {
		case wsTextMessage:
			return json.Unmarshal(payload, target)
		case wsPingMessage:
			if err := c.WriteMessage(wsPongMessage, payload); err != nil {
				return err
			}
		case wsPongMessage:
			_ = c.SetReadDeadline(time.Now().Add(45 * time.Second))
		case wsCloseMessage:
			_ = c.WriteMessage(wsCloseMessage, payload)
			return io.EOF
		default:
			return fmt.Errorf("unsupported websocket opcode %d", opcode)
		}
	}
}

func (c *wsConn) WriteJSON(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.WriteMessage(wsTextMessage, payload)
}

func (c *wsConn) WriteMessage(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if len(payload) > 125 && (opcode == wsCloseMessage || opcode == wsPingMessage || opcode == wsPongMessage) {
		return errors.New("websocket control frame payload is too large")
	}

	header := []byte{0x80 | opcode}
	switch length := len(payload); {
	case length <= 125:
		header = append(header, byte(length))
	case length <= 65535:
		header = append(header, 126, byte(length>>8), byte(length))
	default:
		header = append(header, 127)
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(length))
		header = append(header, size[:]...)
	}

	if _, err := c.rw.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := c.rw.Write(payload); err != nil {
			return err
		}
	}
	return c.rw.Flush()
}

func (c *wsConn) readFrame() (byte, []byte, error) {
	var base [2]byte
	if _, err := io.ReadFull(c.rw, base[:]); err != nil {
		return 0, nil, err
	}

	fin := base[0]&0x80 != 0
	opcode := base[0] & 0x0F
	masked := base[1]&0x80 != 0
	if base[0]&0x70 != 0 {
		return 0, nil, errors.New("websocket extensions were not negotiated")
	}
	if !fin {
		return 0, nil, errors.New("fragmented websocket messages are not supported")
	}
	if !masked {
		return 0, nil, errors.New("client websocket frames must be masked")
	}

	payloadLength := int64(base[1] & 0x7F)
	switch payloadLength {
	case 126:
		var size [2]byte
		if _, err := io.ReadFull(c.rw, size[:]); err != nil {
			return 0, nil, err
		}
		payloadLength = int64(binary.BigEndian.Uint16(size[:]))
	case 127:
		var size [8]byte
		if _, err := io.ReadFull(c.rw, size[:]); err != nil {
			return 0, nil, err
		}
		value := binary.BigEndian.Uint64(size[:])
		if value > uint64(^uint(0)>>1) {
			return 0, nil, errors.New("websocket payload is too large")
		}
		payloadLength = int64(value)
	}

	if c.readLimit > 0 && payloadLength > c.readLimit {
		return 0, nil, errors.New("websocket payload exceeds read limit")
	}
	if (opcode == wsCloseMessage || opcode == wsPingMessage || opcode == wsPongMessage) && payloadLength > 125 {
		return 0, nil, errors.New("invalid websocket control frame")
	}

	var mask [4]byte
	if _, err := io.ReadFull(c.rw, mask[:]); err != nil {
		return 0, nil, err
	}
	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(c.rw, payload); err != nil {
		return 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return opcode, payload, nil
}

func (c *wsConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		err = c.conn.Close()
	})
	return err
}
