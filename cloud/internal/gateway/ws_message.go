package gateway

import (
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var wsCopyBuffers = sync.Pool{New: func() any { return new([32 << 10]byte) }}

var wsSmallBuffers = [...]struct {
	size int64
	pool sync.Pool
}{
	{size: 4 << 10},
	{size: 32 << 10},
	{size: 256 << 10},
	{size: 1 << 20},
}

func writePublicMessage(conn *websocket.Conn, opcode int, payload *io.LimitedReader) error {
	// Small messages benefit from one write instead of many WebSocket frames.
	// Reuse bounded size classes; a tiny message never reserves a 1 MiB buffer.
	for i := range wsSmallBuffers {
		class := &wsSmallBuffers[i]
		if payload.N > class.size {
			continue
		}
		buffer, ok := class.pool.Get().(*[]byte)
		if !ok {
			data := make([]byte, class.size)
			buffer = &data
		}
		defer class.pool.Put(buffer)
		data := (*buffer)[:payload.N]
		if _, err := io.ReadFull(payload, data); err != nil {
			return err
		}
		if err := conn.SetWriteDeadline(time.Now().Add(wsBridgeWriteTimeout)); err != nil {
			return err
		}
		return conn.WriteMessage(opcode, data)
	}

	writer, err := conn.NextWriter(opcode)
	if err != nil {
		return err
	}
	buffer := wsCopyBuffers.Get().(*[32 << 10]byte)
	defer wsCopyBuffers.Put(buffer)
	_, err = io.CopyBuffer(wsDeadlineWriter{conn: conn, writer: writer}, payload, buffer[:])
	if err != nil {
		return err
	}
	if payload.N != 0 {
		return io.ErrUnexpectedEOF
	}
	// Close sends FIN. On any incomplete read the caller instead closes the
	// connection, so a truncated message can never appear complete to the peer.
	if err := conn.SetWriteDeadline(time.Now().Add(wsBridgeWriteTimeout)); err != nil {
		return err
	}
	return writer.Close()
}

type wsDeadlineWriter struct {
	conn   *websocket.Conn
	writer io.Writer
}

func (w wsDeadlineWriter) Write(p []byte) (int, error) {
	// Give each write its own bound. Time spent waiting for a slow node to
	// produce the next chunk must not consume the client's write deadline.
	if err := w.conn.SetWriteDeadline(time.Now().Add(wsBridgeWriteTimeout)); err != nil {
		return 0, err
	}
	return w.writer.Write(p)
}
