package connector

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var ErrNonBinaryTransport = errors.New("connector transport message must be binary")

type WebSocketNetConn struct {
	conn *websocket.Conn

	readMu  sync.Mutex
	writeMu sync.Mutex
	reader  io.Reader
}

func NewWebSocketNetConn(conn *websocket.Conn) net.Conn {
	return &WebSocketNetConn{conn: conn}
}

func (c *WebSocketNetConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for {
		if c.reader == nil {
			messageType, reader, err := c.conn.NextReader()
			if err != nil {
				return 0, err
			}
			if messageType != websocket.BinaryMessage {
				return 0, ErrNonBinaryTransport
			}
			c.reader = reader
		}
		n, err := c.reader.Read(p)
		if errors.Is(err, io.EOF) {
			c.reader = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (c *WebSocketNetConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	writer, err := c.conn.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return 0, err
	}
	n, writeErr := writer.Write(p)
	closeErr := writer.Close()
	if writeErr != nil {
		return n, writeErr
	}
	return n, closeErr
}

func (c *WebSocketNetConn) Close() error                       { return c.conn.Close() }
func (c *WebSocketNetConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *WebSocketNetConn) RemoteAddr() net.Addr               { return c.conn.RemoteAddr() }
func (c *WebSocketNetConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *WebSocketNetConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

func (c *WebSocketNetConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}
