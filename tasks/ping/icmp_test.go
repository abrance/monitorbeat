// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package ping

import (
	"testing"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

func TestParseICMPv4Message_stripsIPv4Header(t *testing.T) {
	raw := marshalEchoForTest(t, ipv4.ICMPTypeEchoReply, 31, 7)
	packet := append(ipv4HeaderForTest(len(raw)), raw...)

	message, err := parseICMPv4Message(packet)
	if err != nil {
		t.Fatalf("parse ICMPv4 message: %v", err)
	}
	if message.Type != ipv4.ICMPTypeEchoReply {
		t.Fatalf("message type = %v, want echo reply", message.Type)
	}
	echo, ok := message.Body.(*icmp.Echo)
	if !ok {
		t.Fatalf("message body = %T, want *icmp.Echo", message.Body)
	}
	if echo.ID != 31 || echo.Seq != 7 {
		t.Fatalf("echo = id=%d seq=%d, want id=31 seq=7", echo.ID, echo.Seq)
	}
}

func TestIsMatchingEchoReply_ignoresUnrelatedPackets(t *testing.T) {
	const (
		id  = 31
		seq = 7
	)

	tests := []struct {
		name string
		raw  []byte
		want bool
	}{
		{
			name: "outbound echo request",
			raw:  marshalEchoForTest(t, ipv4.ICMPTypeEcho, id, seq),
		},
		{
			name: "echo reply with different identifier",
			raw:  marshalEchoForTest(t, ipv4.ICMPTypeEchoReply, id+1, seq),
		},
		{
			name: "echo reply with different sequence",
			raw:  marshalEchoForTest(t, ipv4.ICMPTypeEchoReply, id, seq+1),
		},
		{
			name: "matching echo reply with IPv4 header",
			raw:  append(ipv4HeaderForTest(len(marshalEchoForTest(t, ipv4.ICMPTypeEchoReply, id, seq))), marshalEchoForTest(t, ipv4.ICMPTypeEchoReply, id, seq)...),
			want: true,
		},
		{
			name: "matching bare echo reply",
			raw:  marshalEchoForTest(t, ipv4.ICMPTypeEchoReply, id, seq),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMatchingEchoReply(tt.raw, id, seq); got != tt.want {
				t.Fatalf("isMatchingEchoReply() = %t, want %t", got, tt.want)
			}
		})
	}
}

func marshalEchoForTest(t *testing.T, typ icmp.Type, id, seq int) []byte {
	t.Helper()

	message := icmp.Message{
		Type: typ,
		Body: &icmp.Echo{ID: id, Seq: seq, Data: []byte("monitorbeat")},
	}
	raw, err := message.Marshal(nil)
	if err != nil {
		t.Fatalf("marshal echo: %v", err)
	}
	return raw
}

func ipv4HeaderForTest(payloadLen int) []byte {
	header := make([]byte, ipv4.HeaderLen)
	header[0] = 0x45 // IPv4 with a 20-byte header.
	header[2] = byte((ipv4.HeaderLen + payloadLen) >> 8)
	header[3] = byte(ipv4.HeaderLen + payloadLen)
	header[8] = 64
	header[9] = 1 // ICMP
	return header
}
