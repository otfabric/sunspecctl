// SPDX-License-Identifier: MIT

package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"
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
	// The files are read with a size limit, then parsed as
	// tls.LoadX509KeyPair and modbus.LoadCertPool would.
	certPEM, err := readFileLimited(flagTLSCert, maxPEMFileSize)
	if err != nil {
		return nil, nil, withExit(exitInvalidInput, fmt.Errorf("--tls-cert/--tls-key: %w", err))
	}
	keyPEM, err := readFileLimited(flagTLSKey, maxPEMFileSize)
	if err != nil {
		return nil, nil, withExit(exitInvalidInput, fmt.Errorf("--tls-cert/--tls-key: %w", err))
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, nil, withExit(exitInvalidInput, fmt.Errorf("--tls-cert/--tls-key: %w", err))
	}
	caPEM, err := readFileLimited(flagTLSCA, maxPEMFileSize)
	if err != nil {
		return nil, nil, withExit(exitInvalidInput, fmt.Errorf("--tls-ca: %w", err))
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, nil, withExit(exitInvalidInput, fmt.Errorf("--tls-ca: %s: no certificate found", flagTLSCA))
	}
	return &cert, pool, nil
}
