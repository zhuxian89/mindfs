package gateway

import (
	"encoding/binary"
	"io"
	"math"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

const wsDataHeaderSize = 6

// The tunnel needs the final message length before it can accept the header.
// Stage small browser messages with room for that header. Only larger messages
// need a second buffer, which is released after the write.
func readBrowserFrame(conn *websocket.Conn, scratch []byte, maxMessageBytes int64) (frame, tail []byte, err error) {
	opcode, reader, err := conn.NextReader()
	if err != nil {
		return nil, nil, err
	}
	frame = scratch[:wsDataHeaderSize]
	for len(frame) < len(scratch) {
		n, readErr := reader.Read(scratch[len(frame):])
		frame = scratch[:len(frame)+n]
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, nil, readErr
		}
	}
	if len(frame) == len(scratch) {
		tail, err = io.ReadAll(reader)
		if err != nil {
			return nil, nil, err
		}
	}
	size := int64(len(frame)-wsDataHeaderSize) + int64(len(tail))
	if size > maxMessageBytes || size > math.MaxUint32 {
		return nil, nil, errWSTooLarge
	}
	frame[0] = wsFrameData
	frame[1] = byte(opcode)
	binary.BigEndian.PutUint32(frame[2:wsDataHeaderSize], uint32(size))
	return frame, tail, nil
}

func writeWSCloseFrame(w io.Writer, code int, reason string) error {
	if err := setWriteDeadline(w, wsBridgeWriteTimeout); err != nil {
		return err
	}
	reasonBytes := []byte(reason)
	if len(reasonBytes) > 65535 {
		reasonBytes = reasonBytes[:65535]
	}
	header := make([]byte, 7)
	header[0] = wsFrameClose
	binary.BigEndian.PutUint16(header[1:], uint16(code))
	binary.BigEndian.PutUint32(header[3:], uint32(len(reasonBytes)))
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(reasonBytes)
	return err
}

func readWSFrame(r io.Reader, maxMessageBytes int64) (byte, int, *io.LimitedReader, int, string, error) {
	var frameType [1]byte
	if _, err := io.ReadFull(r, frameType[:]); err != nil {
		return 0, 0, nil, 0, "", err
	}
	switch frameType[0] {
	case wsFrameData:
		var header [5]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return 0, 0, nil, 0, "", err
		}
		size := int64(binary.BigEndian.Uint32(header[1:]))
		if size > maxMessageBytes {
			return 0, 0, nil, 0, "", errWSTooLarge
		}
		return wsFrameData, int(header[0]), &io.LimitedReader{R: r, N: size}, 0, "", nil
	case wsFrameClose:
		var header [6]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return 0, 0, nil, 0, "", err
		}
		size := int64(binary.BigEndian.Uint32(header[2:]))
		if size > 65535 {
			return 0, 0, nil, 0, "", errWSProtocol
		}
		reason := make([]byte, size)
		if _, err := io.ReadFull(r, reason); err != nil {
			return 0, 0, nil, 0, "", err
		}
		if !utf8.Valid(reason) {
			return 0, 0, nil, 0, "", errWSProtocol
		}
		return wsFrameClose, 0, nil, int(binary.BigEndian.Uint16(header[:2])), string(reason), nil
	default:
		return 0, 0, nil, 0, "", errWSProtocol
	}
}
