// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/otfabric/go-modbus"
	"github.com/otfabric/sunspecctl/internal/netdial"
	"github.com/spf13/pflag"
)

// How the client commands open their network connection: --proxy and
// --source-addr, or SUNSPECCTL_PROXY and SUNSPECCTL_SOURCE_ADDR.
var (
	flagProxy      string
	flagSourceAddr string
)

// applyDialEnv sets --proxy and --source-addr, when not given, from
// SUNSPECCTL_PROXY and SUNSPECCTL_SOURCE_ADDR.
func applyDialEnv(flags *pflag.FlagSet) {
	if !flags.Changed("proxy") {
		flagProxy = os.Getenv("SUNSPECCTL_PROXY")
	}
	if !flags.Changed("source-addr") {
		flagSourceAddr = os.Getenv("SUNSPECCTL_SOURCE_ADDR")
	}
}

// dialer returns the go-modbus dialer of --proxy and --source-addr for url,
// or nil when neither is set (go-modbus dials itself). An invalid value, or
// one that does not apply to the transport of url, is a usage error: nothing
// has been sent.
//
// --proxy carries TCP only (SOCKS5 CONNECT): tcp, tcp+tls, rtuovertcp and
// asciiovertcp. --source-addr binds TCP and UDP. Neither applies to a serial
// line. A tcp+tls handshake runs on the proxied connection and verifies the
// device certificate against the host of url, as without a proxy.
func dialer(url string) (modbus.Dialer, error) {
	d, err := netdial.New(flagSourceAddr, flagProxy)
	if err != nil {
		return nil, withExit(exitUsage, err)
	}
	if d == nil {
		return nil, nil
	}
	flag := "--source-addr"
	if flagProxy != "" {
		flag = "--proxy"
	}
	// The scheme as go-modbus reads it: what precedes "://", as written.
	scheme, _, _ := strings.Cut(url, "://")
	switch scheme {
	case "rtu", "ascii":
		return nil, withExit(exitUsage, fmt.Errorf("%s does not apply to %s:// URLs: a serial line has no network connection", flag, scheme))
	case "udp", "rtuoverudp":
		if flagProxy != "" {
			return nil, withExit(exitUsage, fmt.Errorf("--proxy does not apply to %s:// URLs: the SOCKS5 proxy carries TCP only (UDP ASSOCIATE is not supported)", scheme))
		}
	}
	return d, nil
}
