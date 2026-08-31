// +build ignore

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"time"
)

func main() {
	// Generate self-signed cert
	cert, key, err := generateSelfSignedCert()
	if err != nil {
		log.Fatal(err)
	}

	tlsCert, err := tls.X509KeyPair(cert, key)
	if err != nil {
		log.Fatal(err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:9443", &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	log.Printf("[INFO] Backend TLS server listening on %s", ln.Addr())

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		go func(c net.Conn) {
			defer c.Close()
			io.Copy(c, c) // Echo
		}(conn)
	}
}

func generateSelfSignedCert() ([]byte, []byte, error) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "127.0.0.1",
		},
		NotBefore:   now,
		NotAfter:    now.Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}

	certBytes, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM, nil
}
