// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package ping

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// echoHeaderSize is the fixed length of the id/seq prefix we attach to every
// outbound ICMP echo payload. Keeping the prefix tiny keeps the wire format
// compatible with the operating system's `ping` utility which embeds a
// 16-bit identifier and 16-bit sequence number followed by a padding block.
const echoHeaderSize = 4

// buildEchoMessage constructs a wire-format echo payload. id and seq are
// packed as little-endian uint16 values followed by a padding block of
// zeros sized to produce the requested total payload length. The returned
// slice has length payloadSize, and the trailing padding is recoverable
// through parseEchoMessage.
//
// payloadSize must be at least echoHeaderSize; smaller values have no
// padding and cannot be distinguished from the header alone.
func buildEchoMessage(id, seq, payloadSize int) ([]byte, error) {
	if payloadSize < echoHeaderSize {
		return nil, fmt.Errorf("ping: payload size %d below minimum %d", payloadSize, echoHeaderSize)
	}
	if id < 0 || id > 0xFFFF {
		return nil, fmt.Errorf("ping: id %d outside uint16 range", id)
	}
	if seq < 0 || seq > 0xFFFF {
		return nil, fmt.Errorf("ping: seq %d outside uint16 range", seq)
	}

	buf := make([]byte, payloadSize)
	binary.LittleEndian.PutUint16(buf[0:2], uint16(id))
	binary.LittleEndian.PutUint16(buf[2:4], uint16(seq))
	// Remaining bytes are zero-filled by make.
	return buf, nil
}

// parseEchoMessage is the inverse of buildEchoMessage. It recovers the
// identifier, sequence number, and total payload size from a previously
// built echo message. Messages shorter than echoHeaderSize are rejected.
func parseEchoMessage(msg []byte) (id, seq, size int, err error) {
	if len(msg) < echoHeaderSize {
		return 0, 0, 0, fmt.Errorf("ping: echo message shorter than header: %d bytes", len(msg))
	}
	return int(binary.LittleEndian.Uint16(msg[0:2])),
		int(binary.LittleEndian.Uint16(msg[2:4])),
		len(msg),
		nil
}

// errEmptySamples is returned by aggregateResults when no round-trip
// samples are provided. Callers can treat it as a fatal condition for
// reporting purposes.
var errEmptySamples = errors.New("ping: cannot aggregate zero samples")
