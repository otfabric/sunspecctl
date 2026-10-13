// SPDX-License-Identifier: MIT

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// testCA is a certificate authority made for one test.
type testCA struct {
	t    *testing.T
	dir  string
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	// file is the PEM file with the CA certificate.
	file string
}

func writePEM(t *testing.T, path, typ string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newTestCA(t *testing.T, name string) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	ca := &testCA{t: t, dir: t.TempDir(), cert: cert, key: key}
	ca.file = filepath.Join(ca.dir, "ca.pem")
	writePEM(t, ca.file, "CERTIFICATE", der)
	return ca
}

// issue writes a certificate for 127.0.0.1 signed by the CA, usable by a
// server and by a client, and returns the certificate and key files.
func (ca *testCA) issue(name string) (certFile, keyFile string) {
	ca.t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		ca.t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		ca.t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		ca.t.Fatal(err)
	}
	certFile = filepath.Join(ca.dir, name+".pem")
	keyFile = filepath.Join(ca.dir, name+".key")
	writePEM(ca.t, certFile, "CERTIFICATE", der)
	writePEM(ca.t, keyFile, "EC PRIVATE KEY", keyDER)
	return certFile, keyFile
}

// A device served on tcp+tls:// is read by the client commands with a
// certificate of the same CA, and refuses a client of another CA.
func TestServeTLS(t *testing.T) {
	ca := newTestCA(t, "plant CA")
	serverCert, serverKey := ca.issue("device")
	clientCert, clientKey := ca.issue("client")
	url := "tcp+tls://127.0.0.1:" + strconv.Itoa(freePort(t))

	s := startServe(t, writeFile(t, exampleDevice),
		"--url", url, "--tls-cert", serverCert, "--tls-key", serverKey, "--tls-ca", ca.file)
	if got := s.stdout.String(); !strings.HasPrefix(got, "Serving SunSpec device on "+url+" (unit ID 3, base address 40000)\n") {
		t.Errorf("status:\n%s", got)
	}

	tlsArgs := []string{"--url", url, "--unit-id", "3", "--tls-cert", clientCert, "--tls-key", clientKey, "--tls-ca", ca.file}
	r := runCLI(t, append([]string{"read-point", "--model", "1", "--point", "Mn", "--json"}, tlsArgs...)...)
	if r.err != nil {
		t.Fatalf("read-point over TLS: %v\nstderr: %s", r.err, r.stderr)
	}
	if !strings.Contains(r.stdout, `"Acme"`) {
		t.Errorf("read-point over TLS did not return the manufacturer:\n%s", r.stdout)
	}

	// A plain TCP client cannot talk to it.
	r = runCLI(t, "models", "--url", strings.Replace(url, "tcp+tls://", "tcp://", 1), "--unit-id", "3", "--timeout", "2s")
	if r.err == nil {
		t.Errorf("a plain TCP client read the TLS device:\n%s", r.stdout)
	}

	// Nor can a client whose certificate comes from another CA.
	other := newTestCA(t, "another CA")
	otherCert, otherKey := other.issue("intruder")
	r = runCLI(t, "models", "--url", url, "--unit-id", "3", "--timeout", "2s",
		"--tls-cert", otherCert, "--tls-key", otherKey, "--tls-ca", ca.file)
	if r.err == nil {
		t.Errorf("a client with a certificate of another CA read the device:\n%s", r.stdout)
	}

	// And a client that does not trust the device's CA refuses the device.
	r = runCLI(t, "models", "--url", url, "--unit-id", "3", "--timeout", "2s",
		"--tls-cert", clientCert, "--tls-key", clientKey, "--tls-ca", other.file)
	if r.err == nil {
		t.Errorf("a client read a device whose CA it does not trust:\n%s", r.stdout)
	}

	// The device survived all of that.
	r = runCLI(t, append([]string{"models"}, tlsArgs...)...)
	if r.err != nil {
		t.Fatalf("models over TLS after the refused clients: %v\nstderr: %s", r.err, r.stderr)
	}
	if err := s.stop(t); err != nil {
		t.Errorf("serve: %v", err)
	}
}

func TestTLSFlagErrors(t *testing.T) {
	ca := newTestCA(t, "plant CA")
	cert, key := ca.issue("device")
	device := writeFile(t, exampleDevice)
	notPEM := writeFile(t, "not a certificate")
	missing := filepath.Join(t.TempDir(), "missing.pem")
	const url = "tcp+tls://127.0.0.1:1"

	t.Run("client without flags", func(t *testing.T) {
		wantFail(t, runCLI(t, "models", "--url", url), "a tcp+tls:// URL needs --tls-cert, --tls-key, --tls-ca")
	})
	t.Run("client without CA", func(t *testing.T) {
		wantFail(t, runCLI(t, "models", "--url", url, "--tls-cert", cert, "--tls-key", key), "a tcp+tls:// URL needs --tls-ca")
	})
	t.Run("serve without flags", func(t *testing.T) {
		wantFail(t, runCLI(t, "serve", "--file", device, "--url", url),
			"serve: a tcp+tls:// URL needs --tls-cert, --tls-key, --tls-ca")
	})
	t.Run("missing certificate file", func(t *testing.T) {
		r := runCLI(t, "models", "--url", url, "--tls-cert", missing, "--tls-key", key, "--tls-ca", ca.file)
		if r.err == nil || !strings.HasPrefix(r.err.Error(), "--tls-cert/--tls-key: ") {
			t.Errorf("error = %v", r.err)
		}
	})
	t.Run("key of another certificate", func(t *testing.T) {
		_, otherKey := ca.issue("other")
		r := runCLI(t, "models", "--url", url, "--tls-cert", cert, "--tls-key", otherKey, "--tls-ca", ca.file)
		if r.err == nil || !strings.HasPrefix(r.err.Error(), "--tls-cert/--tls-key: ") {
			t.Errorf("error = %v", r.err)
		}
	})
	t.Run("CA file without certificates", func(t *testing.T) {
		r := runCLI(t, "serve", "--file", device, "--url", url, "--tls-cert", cert, "--tls-key", key, "--tls-ca", notPEM)
		if r.err == nil || !strings.HasPrefix(r.err.Error(), "serve: --tls-ca: ") {
			t.Errorf("error = %v", r.err)
		}
	})
}

// The TLS flags are ignored for a URL that is not tcp+tls://, like the serial
// flags on a network URL: nothing is loaded, so a wrong path does no harm.
func TestTLSFlagsIgnoredWithoutTLSURL(t *testing.T) {
	s := startServe(t, writeFile(t, exampleDevice), "--tls-cert", "/nonexistent")
	r := runCLI(t, "models", "--url", s.url, "--unit-id", "3", "--tls-ca", "/nonexistent")
	if r.err != nil {
		t.Fatalf("models: %v\nstderr: %s", r.err, r.stderr)
	}
}
