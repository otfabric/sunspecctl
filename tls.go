// SPDX-License-Identifier: MIT

package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"

	"github.com/otfabric/go-modbus"
)

// TLS settings, used with tcp+tls:// URLs only (Modbus/TCP Security, which is
// mutual TLS: both sides present a certificate).
var (
	flagTLSCert string
	flagTLSKey  string
	flagTLSCA   string
)

// isTLSURL reports whether url selects Modbus over TLS.
func isTLSURL(url string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(url)), "tcp+tls://")
}

// loadTLS loads the certificate, key and CA bundle named by the --tls-* flags
// for a tcp+tls:// URL. For any other URL it returns nil, nil, nil: the flags
// are ignored, like the serial flags on a network URL.
//
// The certificate and key are this side's identity. The CA bundle holds the
// certificates that the other side's certificate must chain to.
func loadTLS(url string) (*tls.Certificate, *x509.CertPool, error) {
	if !isTLSURL(url) {
		return nil, nil, nil
	}
	var missing []string
	if flagTLSCert == "" {
		missing = append(missing, "--tls-cert")
	}
	if flagTLSKey == "" {
		missing = append(missing, "--tls-key")
	}
	if flagTLSCA == "" {
		missing = append(missing, "--tls-ca")
	}
	if len(missing) > 0 {
		return nil, nil, withExit(exitUsage, fmt.Errorf("a tcp+tls:// URL needs %s", strings.Join(missing, ", ")))
	}
	cert, err := tls.LoadX509KeyPair(flagTLSCert, flagTLSKey)
	if err != nil {
		return nil, nil, withExit(exitInvalidInput, fmt.Errorf("--tls-cert/--tls-key: %w", err))
	}
	pool, err := modbus.LoadCertPool(flagTLSCA)
	if err != nil {
		return nil, nil, withExit(exitInvalidInput, fmt.Errorf("--tls-ca: %w", err))
	}
	return &cert, pool, nil
}
