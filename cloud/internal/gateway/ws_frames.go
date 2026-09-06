package gateway

import (
	"encoding/binary"
	"io"
	"unicode/utf8"
)

func writeWSDataFrame(w io.Writer, opcode int, payload []byte) error {
	if err := setWriteDeadline(w, wsBridgeWriteTimeout); err != nil {
		return err
	}
	header := make([]byte, 6)
	header[0] = wsFrameData
	header[1] = byte(opcode)
	binary.BigEndian.PutUint32(header[2:], uint32(len(payload)))
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
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
