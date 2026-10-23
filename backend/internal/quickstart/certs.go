package quickstart

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// CA is the Hub's own certificate authority, made at first start. Agents
// trust it through the import manifest; browsers warn until you bring your
// own certificate.
type CA struct {
	// PEM is the CA certificate.
	PEM  []byte
	cert *x509.Certificate
	key  crypto.Signer
}

const (
	caCertFile = "ca.crt"
	caKeyFile  = "ca.key"
	// backdate is how long before now certificates start: an agent whose
	// clock is behind the Hub's (an hour is common in labs) still trusts them.
	backdate = 24 * time.Hour
)

// LoadOrCreateCA reads the CA in dir, or makes one.
func LoadOrCreateCA(dir string) (*CA, error) {
	certPEM, err := os.ReadFile(filepath.Join(dir, caCertFile)) //nolint:gosec // dir is the data directory, set by the operator
	if errors.Is(err, fs.ErrNotExist) {
		return createCA(dir)
	}
	if err != nil {
		return nil, fmt.Errorf("read the CA: %w", err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, caKeyFile)) //nolint:gosec // as above
	if err != nil {
		return nil, fmt.Errorf("read the CA key: %w", err)
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load the CA: %w", err)
	}
	signer, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, errors.New("the CA key can't sign")
	}
	return &CA{PEM: certPEM, cert: pair.Leaf, key: signer}, nil
}

// CAPath is where LoadOrCreateCA keeps the CA certificate in dir.
func CAPath(dir string) string { return filepath.Join(dir, caCertFile) }

func createCA(dir string) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("make the CA key: %w", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "Glowpod Hub CA"},
		NotBefore:             now.Add(-backdate),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("make the CA: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("read the new CA: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("encode the CA key: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("make %s: %w", dir, err)
	}
	// The key first: a CA certificate without its key would be read back as
	// a broken CA.
	if err := os.WriteFile(filepath.Join(dir, caKeyFile), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return nil, fmt.Errorf("save the CA key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, caCertFile), certPEM, 0o600); err != nil {
		return nil, fmt.Errorf("save the CA: %w", err)
	}
	return &CA{PEM: certPEM, cert: cert, key: key}, nil
}

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	return n
}

// Certs mints server certificates from the CA for the names the Hub is
// reached by: the server URL's host (agents connecting by IP send no name),
// the name the client asked for, and localhost.
type Certs struct {
	CA *CA
	// Names returns more names to cover, such as the server URL's host.
	Names func() []string

	mu    sync.Mutex
	cache map[string]*tls.Certificate
	now   func() time.Time
}

// maxCached bounds the cache: clients choose the names they ask for.
const maxCached = 64

// GetCertificate is for tls.Config.
func (c *Certs) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	names := []string{"localhost", "127.0.0.1"}
	if c.Names != nil {
		names = append(names, c.Names()...)
	}
	if hello.ServerName != "" {
		names = append(names, hello.ServerName)
	}
	for i, n := range names {
		names[i] = strings.ToLower(strings.TrimSuffix(n, "."))
	}
	slices.Sort(names)
	names = slices.Compact(names)
	key := strings.Join(names, ",")

	now := time.Now
	if c.now != nil {
		now = c.now
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cert, ok := c.cache[key]; ok && cert.Leaf.NotAfter.Sub(now()) > 30*24*time.Hour {
		return cert, nil
	}
	cert, err := c.mint(names, now())
	if err != nil {
		return nil, err
	}
	if c.cache == nil || len(c.cache) >= maxCached {
		c.cache = map[string]*tls.Certificate{}
	}
	c.cache[key] = cert
	return cert, nil
}

func (c *Certs) mint(names []string, now time.Time) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("make a server key: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: names[0]},
		NotBefore:    now.Add(-backdate),
		NotAfter:     now.AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, n)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.CA.cert, &key.PublicKey, c.CA.key)
	if err != nil {
		return nil, fmt.Errorf("make a server certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("read the server certificate: %w", err)
	}
	return &tls.Certificate{Certificate: [][]byte{der, c.CA.cert.Raw}, PrivateKey: key, Leaf: leaf}, nil
}

// Redirect sends plain-HTTP visitors to HTTPS: to publicURL when it's set,
// otherwise to the same host on the default port.
func Redirect(publicURL string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := strings.TrimSuffix(publicURL, "/")
		if target == "" {
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
			target = "https://" + host
		}
		//nolint:gosec // to the public URL, or to the host the visitor asked for: nowhere they didn't already go
		http.Redirect(w, r, target+r.URL.RequestURI(), http.StatusMovedPermanently)
	})
}
