// SPDX-License-Identifier: MIT

package main

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/sunspecctl/internal/socks5test"
)

// hostPort returns the host:port of a tcp:// URL.
func hostPort(url string) string { return strings.TrimPrefix(url, "tcp://") }

// wantProxied asserts that every CONNECT the proxy saw went to target.
func wantProxied(t *testing.T, px *socks5test.Server, target string) {
	t.Helper()
	reqs := px.Requests()
	if len(reqs) == 0 {
		t.Fatal("the proxy saw no CONNECT")
	}
	for _, r := range reqs {
		if got := net.JoinHostPort(r.Host, strconv.Itoa(r.Port)); got != target {
			t.Errorf("CONNECT %s, want %s", got, target)
		}
	}
}

// Every command that connects to a device goes through --proxy: the proxy
// sees a CONNECT to the device and the command succeeds.
func TestProxyEveryClientCommand(t *testing.T) {
	url := startDevice(t, inverter(t))
	commands := append(append([][]string{}, deviceCommands...), []string{"capture"})
	for _, args := range commands {
		t.Run(args[0], func(t *testing.T) {
			px := socks5test.Start(t)
			r := runCLI(t, append(args, "--url", url, "--proxy", px.URL(), "--timeout", "5s")...)
			if r.err != nil {
				t.Fatalf("%v\nstderr: %s", r.err, r.stderr)
			}
			wantProxied(t, px, hostPort(url))
		})
	}
}

// socks5h sends the host name of --url to the proxy, which resolves it.
func TestProxyRemoteName(t *testing.T) {
	url := startDevice(t, inverter(t))
	px := socks5test.Start(t)
	px.Forward = func(string) string { return hostPort(url) }
	_, port, _ := net.SplitHostPort(hostPort(url))
	r := runCLI(t, "detect", "--url", "tcp://plant-inverter.invalid:"+port, "--proxy", "socks5h://"+px.Addr)
	if r.err != nil {
		t.Fatalf("%v\nstderr: %s", r.err, r.stderr)
	}
	if reqs := px.Requests(); len(reqs) != 1 || reqs[0].AddrType != 3 || reqs[0].Host != "plant-inverter.invalid" {
		t.Errorf("proxy saw %+v, want the host name", reqs)
	}
}

// Over tcp+tls the TLS handshake runs on the proxied connection, and the
// device certificate is verified against the host of --url, not the proxy.
func TestProxyTLS(t *testing.T) {
	ca := newTestCA(t, "plant CA")
	serverCert, serverKey := ca.issue("device")
	clientCert, clientKey := ca.issue("client")
	url := "tcp+tls://127.0.0.1:" + strconv.Itoa(freePort(t))
	s := startServe(t, writeFile(t, exampleDevice),
		"--url", url, "--tls-cert", serverCert, "--tls-key", serverKey, "--tls-ca", ca.file)
	tlsArgs := []string{"--unit-id", "3", "--timeout", "5s", "--tls-cert", clientCert, "--tls-key", clientKey, "--tls-ca", ca.file}

	px := socks5test.Start(t)
	r := runCLI(t, append([]string{"read-point", "--model", "1", "--point", "Mn", "--url", url, "--proxy", px.URL()}, tlsArgs...)...)
	if r.err != nil {
		t.Fatalf("read-point over TLS through the proxy: %v\nstderr: %s", r.err, r.stderr)
	}
	if !strings.Contains(r.stdout, "Acme") {
		t.Errorf("stdout:\n%s", r.stdout)
	}
	wantProxied(t, px, strings.TrimPrefix(url, "tcp+tls://"))

	// The certificate is for 127.0.0.1: a URL naming another host fails the
	// handshake although the proxy forwards it to the same device.
	other := socks5test.Start(t)
	other.Forward = func(string) string { return strings.TrimPrefix(url, "tcp+tls://") }
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(url, "tcp+tls://"))
	r = runCLI(t, append([]string{"detect", "--url", "tcp+tls://device.invalid:" + port, "--proxy", "socks5h://" + other.Addr}, tlsArgs...)...)
	if r.err == nil || !strings.Contains(r.err.Error(), "certificate") {
		t.Errorf("TLS to a name the certificate does not hold: err = %v", r.err)
	}
	if err := s.stop(t); err != nil {
		t.Errorf("serve: %v", err)
	}
}

// A proxy that refuses is a connection failure (exit 4), naming the proxy
// and its reply; a proxy that does not answer is a timeout (exit 5).
func TestProxyRefusedIsATransportError(t *testing.T) {
	url := startDevice(t, inverter(t))
	px := socks5test.Start(t)
	px.Reply = 2
	r := runCLI(t, "detect", "--url", url, "--proxy", px.URL())
	if r.err == nil || !strings.Contains(r.err.Error(), "connection not allowed by ruleset") ||
		!strings.Contains(r.err.Error(), "socks5 proxy "+px.Addr) {
		t.Fatalf("err = %v", r.err)
	}
	if got := exitCode(r.err); got != exitTransport {
		t.Errorf("exit code %d, want %d", got, exitTransport)
	}
	wantNoCommandOutput(t, r)

	silent := socks5test.Start(t)
	silent.Silent = true
	r = runCLI(t, "detect", "--url", url, "--proxy", silent.URL(), "--timeout", "1s")
	if got := exitCode(r.err); got != exitTimeout {
		t.Errorf("silent proxy: exit code %d (err %v), want %d", got, r.err, exitTimeout)
	}
}

// --source-addr binds the local end of a TCP connection, and with --proxy
// the connection to the proxy.
func TestSourceAddrTCP(t *testing.T) {
	url := startDevice(t, inverter(t))
	if r := runCLI(t, "detect", "--url", url, "--source-addr", "127.0.0.1"); r.err != nil {
		t.Fatalf("%v\nstderr: %s", r.err, r.stderr)
	}
	px := socks5test.Start(t)
	if r := runCLI(t, "detect", "--url", url, "--source-addr", "127.0.0.1", "--proxy", px.URL()); r.err != nil {
		t.Fatalf("with --proxy: %v\nstderr: %s", r.err, r.stderr)
	}
	wantProxied(t, px, hostPort(url))
}

// --source-addr binds the local end of the udp and rtuoverudp transports: a
// silent UDP peer sees the requests come from that address.
func TestSourceAddrUDP(t *testing.T) {
	for _, scheme := range []string{"udp", "rtuoverudp"} {
		t.Run(scheme, func(t *testing.T) {
			pc, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = pc.Close() }()
			from := make(chan net.Addr, 1)
			go func() {
				buf := make([]byte, 512)
				if _, a, err := pc.ReadFrom(buf); err == nil {
					from <- a
				}
			}()
			r := runCLI(t, "detect", "--url", scheme+"://"+pc.LocalAddr().String(), "--source-addr", "127.0.0.1", "--timeout", "300ms")
			if got := exitCode(r.err); got == exitUsage || got == exitOK {
				t.Fatalf("exit code %d (err %v), want a device error", got, r.err)
			}
			select {
			case a := <-from:
				if ip := a.(*net.UDPAddr).IP; !ip.Equal(net.IPv4(127, 0, 0, 1)) {
					t.Errorf("request from %v, want 127.0.0.1", a)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no request arrived")
			}
		})
	}
}

// Invalid values, and flags that do not apply to the transport, are usage
// errors (exit 2) before anything is sent: the proxy sees no connection.
func TestDialFlagsUsageErrors(t *testing.T) {
	px := socks5test.Start(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--proxy", "http://127.0.0.1:8080"}, "the scheme must be socks5 or socks5h"},
		{[]string{"--proxy", "socks5://user:pw@127.0.0.1:1080"}, "proxy credentials are not supported"},
		{[]string{"--proxy", "socks5://127.0.0.1"}, "want socks5://host:port"},
		{[]string{"--source-addr", "eth0"}, `invalid --source-addr "eth0": want an IP address`},
		{[]string{"--url", "udp://127.0.0.1:502", "--proxy", px.URL()}, "--proxy does not apply to udp:// URLs"},
		{[]string{"--url", "rtuoverudp://127.0.0.1:502", "--proxy", px.URL()}, "--proxy does not apply to rtuoverudp:// URLs"},
		{[]string{"--url", "udp://127.0.0.1:502", "--proxy", px.URL(), "--source-addr", "127.0.0.1"}, "--proxy does not apply to udp:// URLs"},
		{[]string{"--url", "rtu:///dev/ttyUSB0", "--proxy", px.URL()}, "--proxy does not apply to rtu:// URLs"},
		{[]string{"--url", "ascii:///dev/ttyUSB0", "--proxy", px.URL()}, "--proxy does not apply to ascii:// URLs"},
		{[]string{"--url", "rtu:///dev/ttyUSB0", "--source-addr", "127.0.0.1"}, "--source-addr does not apply to rtu:// URLs"},
		{[]string{"--url", "ascii:///dev/ttyUSB0", "--source-addr", "127.0.0.1"}, "--source-addr does not apply to ascii:// URLs"},
	} {
		for _, cmd := range append(append([][]string{}, deviceCommands...), []string{"capture"}) {
			t.Run(cmd[0]+" "+strings.Join(tc.args, " "), func(t *testing.T) {
				r := runCLI(t, append(append([]string{}, cmd...), tc.args...)...)
				if r.err == nil || !strings.Contains(r.err.Error(), tc.want) {
					t.Fatalf("err = %v, want it to mention %q", r.err, tc.want)
				}
				if got := exitCode(r.err); got != exitUsage {
					t.Errorf("exit code %d, want %d", got, exitUsage)
				}
				wantNoCommandOutput(t, r)
			})
		}
	}
	if n := len(px.Requests()); n != 0 {
		t.Errorf("the proxy saw %d CONNECTs", n)
	}
}

// serve listens: --proxy and --source-addr are refused there, while the
// environment variables are ignored.
func TestDialFlagsServe(t *testing.T) {
	path := writeFile(t, exampleDevice)
	for _, f := range []string{"--proxy", "--source-addr"} {
		r := runCLI(t, "serve", "--file", path, "--check", f, "127.0.0.1")
		if r.err == nil || r.err.Error() != f+" does not apply to serve, which listens on --url" || exitCode(r.err) != exitUsage {
			t.Errorf("serve %s: err = %v", f, r.err)
		}
	}
	t.Setenv("SUNSPECCTL_PROXY", "socks5://127.0.0.1:1080")
	t.Setenv("SUNSPECCTL_SOURCE_ADDR", "127.0.0.1")
	if r := runCLI(t, "serve", "--file", path, "--check"); r.err != nil {
		t.Errorf("serve --check with the environment set: %v", r.err)
	}
}

// SUNSPECCTL_PROXY and SUNSPECCTL_SOURCE_ADDR set the flags when they are
// not given; a flag given wins.
func TestDialEnvironment(t *testing.T) {
	url := startDevice(t, inverter(t))
	px := socks5test.Start(t)
	t.Setenv("SUNSPECCTL_PROXY", px.URL())
	t.Setenv("SUNSPECCTL_SOURCE_ADDR", "127.0.0.1")
	if r := runCLI(t, "detect", "--url", url); r.err != nil {
		t.Fatalf("%v\nstderr: %s", r.err, r.stderr)
	}
	wantProxied(t, px, hostPort(url))
	if flagSourceAddr != "127.0.0.1" {
		t.Errorf("--source-addr = %q, want it from SUNSPECCTL_SOURCE_ADDR", flagSourceAddr)
	}

	refusing := socks5test.Start(t)
	refusing.Reply = 5
	t.Setenv("SUNSPECCTL_PROXY", refusing.URL())
	if r := runCLI(t, "detect", "--url", url, "--proxy", px.URL()); r.err != nil {
		t.Fatalf("--proxy over SUNSPECCTL_PROXY: %v", r.err)
	}
	if n := len(refusing.Requests()); n != 0 {
		t.Errorf("the proxy of SUNSPECCTL_PROXY saw %d CONNECTs", n)
	}

	t.Setenv("SUNSPECCTL_PROXY", "ftp://127.0.0.1:21")
	r := runCLI(t, "detect", "--url", url)
	if r.err == nil || exitCode(r.err) != exitUsage {
		t.Errorf("invalid SUNSPECCTL_PROXY: err = %v", r.err)
	}
}
