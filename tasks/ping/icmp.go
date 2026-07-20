// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package ping

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/tasks/probe"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// icmpBackendTag is the value attached to probe metrics so downstream
// consumers can distinguish command-driven samples from raw-socket
// samples without re-deriving it from context.
const icmpBackendTag = 1

// icmpReadDeadline bounds how long the listener waits for a single
// reply before treating the packet as lost. Defaults to MaxRTT from
// the config or one second.
func icmpReadDeadline(cfg *configs.PingConfig) time.Duration {
	if cfg.MaxRTT > 0 {
		return cfg.MaxRTT
	}
	return time.Second
}

// canListenICMP reports whether the current process can open a raw
// ICMP socket. The probe is cheap so it can run on every scheduler
// tick; the kernel allocates the socket only for the lifetime of the
// probe so there is no persistent resource cost.
func canListenICMP() bool {
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// runICMPBackend sends `count` echo requests through a raw ICMP socket
// and waits up to cfg.MaxRTT for each reply. Samples that did not come
// back in time are recorded as negative durations so aggregateResults
// produces the correct loss statistics.
//
// The function is best-effort with respect to privilege: hosts without
// CAP_NET_RAW will surface an error result rather than crashing the
// scheduler.
func runICMPBackend(ctx context.Context, start time.Time, cfg *configs.PingConfig) probe.Result {
	host, err := resolveTarget(cfg.Target)
	if err != nil {
		return failureResult(start, fmt.Errorf("ping: resolve target %q: %w", cfg.Target, err))
	}

	count := cfg.Count
	if count <= 0 {
		count = 2
	}
	payloadSize := cfg.PayloadSize
	if payloadSize < 8 {
		payloadSize = 56
	}

	listener, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		if isPermissionError(err) {
			return failureResult(start, fmt.Errorf("ping: icmp backend requires CAP_NET_RAW: %w", err))
		}
		return failureResult(start, fmt.Errorf("ping: icmp listen: %w", err))
	}
	defer listener.Close()

	id := os.Getpid() & 0xffff
	deadline := icmpReadDeadline(cfg)
	samples := make([]time.Duration, count)

	for seq := 0; seq < count; seq++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			for j := seq; j < count; j++ {
				samples[j] = -1
			}
			break
		}

		payload, err := buildEchoMessage(id, seq+1, payloadSize)
		if err != nil {
			samples[seq] = -1
			continue
		}
		msg := icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID: id, Seq: seq + 1,
				Data: payload,
			},
		}
		binary, err := msg.Marshal(nil)
		if err != nil {
			samples[seq] = -1
			continue
		}

		sendAt := time.Now()
		if _, err := listener.WriteTo(binary, &net.IPAddr{IP: host}); err != nil {
			samples[seq] = -1
			continue
		}

		reply, err := readOneEcho(ctx, listener, id, seq+1, deadline)
		if err != nil {
			samples[seq] = -1
			continue
		}
		samples[seq] = reply.Sub(sendAt)

		if cfg.SendInterval > 0 && seq+1 < count {
			select {
			case <-ctx.Done():
				for j := seq + 1; j < count; j++ {
					samples[j] = -1
				}
				seq = count
			case <-time.After(cfg.SendInterval):
			}
		}
	}

	duration := time.Since(start)
	metrics := aggregateResults(samples)
	if metrics == nil {
		metrics = map[string]float64{}
	}
	metrics["backend"] = float64(icmpBackendTag)

	success := metrics["packets_received"] > 0
	return probe.Result{Success: success, Duration: duration, Metrics: metrics}
}

// readOneEcho waits for an Echo Reply matching id and seq. Raw IPv4 sockets
// receive outbound Echo Requests and unrelated ICMP packets as well as replies,
// so packets that do not match the current probe are ignored until the deadline.
func readOneEcho(ctx context.Context, listener *icmp.PacketConn, id, seq int, deadline time.Duration) (time.Time, error) {
	expiresAt := time.Now().Add(deadline)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(expiresAt) {
		expiresAt = ctxDeadline
	}

	buf := make([]byte, 1500)
	for {
		if err := ctx.Err(); err != nil {
			return time.Time{}, err
		}
		if err := listener.SetReadDeadline(expiresAt); err != nil {
			return time.Time{}, fmt.Errorf("set ICMP read deadline: %w", err)
		}

		n, _, err := listener.ReadFrom(buf)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return time.Time{}, ctxErr
			}
			return time.Time{}, err
		}
		if isMatchingEchoReply(buf[:n], id, seq) {
			return time.Now(), nil
		}
	}
}

// isMatchingEchoReply reports whether raw is the Echo Reply for a particular
// probe. Invalid, outbound, and unrelated packets are intentionally ignored by
// readOneEcho because raw sockets observe all of them.
func isMatchingEchoReply(raw []byte, id, seq int) bool {
	msg, err := parseICMPv4Message(raw)
	if err != nil || msg.Type != ipv4.ICMPTypeEchoReply {
		return false
	}
	echo, ok := msg.Body.(*icmp.Echo)
	return ok && echo.ID == id && echo.Seq == seq
}

// parseICMPv4Message converts a packet received from an IPv4 raw socket into
// an ICMP message. Linux supplies the IPv4 header for ip4:icmp sockets, while
// other environments may supply only the ICMP payload, so both forms are
// accepted.
func parseICMPv4Message(raw []byte) (*icmp.Message, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty ICMP packet")
	}

	icmpPayload := raw
	if raw[0]>>4 == 4 {
		headerLen := int(raw[0]&0x0f) * 4
		if headerLen < ipv4.HeaderLen || headerLen >= len(raw) {
			return nil, fmt.Errorf("invalid IPv4 header length %d for %d-byte packet", headerLen, len(raw))
		}
		icmpPayload = raw[headerLen:]
	}
	return icmp.ParseMessage(1, icmpPayload)
}

// resolveTarget turns a hostname or IP literal into a net.IP suitable
// for the ICMP peer address. We force IPv4 because the listener binds
// ip4:icmp; an IPv6 target would silently never match.
func resolveTarget(target string) (net.IP, error) {
	if ip := net.ParseIP(target); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4, nil
		}
		return nil, fmt.Errorf("ping: %q is not an IPv4 address", target)
	}
	ips, err := net.LookupIP(target)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4, nil
		}
	}
	return nil, fmt.Errorf("ping: no IPv4 address found for %q", target)
}

// failureResult centralises the error-result shape so callers do not
// duplicate the metric map allocation on every failure path.
func failureResult(start time.Time, err error) probe.Result {
	return probe.Result{
		Duration: time.Since(start),
		Error:    err.Error(),
		Metrics:  map[string]float64{"backend": float64(icmpBackendTag)},
	}
}

// isPermissionError recognises the kernel's "operation not permitted"
// outcome across the variants emitted by Linux/BSD.
func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Err != nil && opErr.Err.Error() == "operation not permitted" {
			return true
		}
	}
	return err.Error() == "operation not permitted"
}
