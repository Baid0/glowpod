package terminal

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// A kubeconfig's config has a dialer (for its connect timeout) and a client
// certificate. Exec must still present the certificate over HTTPS.
func TestRemoteWithADialerSendsTheClientCertificate(t *testing.T) {
	certPEM, keyPEM := selfSigned(t)
	var sawCert atomic.Bool
	api := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			sawCert.Store(true)
		}
		http.Error(w, "no exec here", http.StatusTeapot)
	}))
	api.TLS = &tls.Config{ClientAuth: tls.RequestClientCert, MinVersion: tls.VersionTLS12}
	api.StartTLS()
	t.Cleanup(api.Close)

	var dialed atomic.Bool
	cfg := &rest.Config{
		Host: api.URL,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true, CertData: certPEM, KeyData: keyPEM,
		},
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialed.Store(true)
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
		},
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = Remote(cfg, kube)(t.Context(), Request{Namespace: "ns", Pod: "p", Container: "c"},
		Streams{Stdin: strings.NewReader(""), Stdout: &strings.Builder{}, Sizes: newSizeQueue()})
	if err == nil {
		t.Fatal("the fake API server doesn't do exec")
	}
	if !dialed.Load() || !sawCert.Load() {
		t.Fatalf("dialed with the config's dialer: %v; the API server saw the client certificate: %v", dialed.Load(), sawCert.Load())
	}
}

func selfSigned(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "kubernetes-admin"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}
