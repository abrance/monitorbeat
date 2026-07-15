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
	"sync"
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
			Type: ipv4.ICMPTypeEcho, Code: 0,
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

// readOneEcho waits for a single Echo Reply matching id/seq. It enforces
// both the per-packet deadline and the surrounding context so callers
// can cancel the sweep without leaking the listener.
func readOneEcho(ctx context.Context, listener *icmp.PacketConn, id, seq int, deadline time.Duration) (time.Time, error) {
	type result struct {
		at  time.Time
		err error
	}
	ch := make(chan result, 1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1500)
		_ = listener.SetReadDeadline(time.Now().Add(deadline))
		n, peer, err := listener.ReadFrom(buf)
		if err != nil {
			ch <- result{err: err}
			return
		}
		msg, err := icmp.ParseMessage(1, buf[:n])
		if err != nil {
			ch <- result{err: err}
			return
		}
		if msg.Type != ipv4.ICMPTypeEchoReply {
			ch <- result{err: fmt.Errorf("unexpected icmp type %v from %s", msg.Type, peer)}
			return
		}
		echo, ok := msg.Body.(*icmp.Echo)
		if !ok || echo.ID != id || echo.Seq != seq {
			ch <- result{err: fmt.Errorf("mismatched echo reply (id=%d seq=%d) from %s", echo.ID, echo.Seq, peer)}
			return
		}
		ch <- result{at: time.Now()}
	}()

	select {
	case r := <-ch:
		wg.Wait()
		return r.at, r.err
	case <-ctx.Done():
		wg.Wait()
		return time.Time{}, ctx.Err()
	}
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
