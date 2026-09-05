package dnsclient

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// mockReply builds a DNS response per query-name prefix:
//
//	servfail.* / nxdomain.* / refused.* -> matching rcode
//	empty.*                             -> NOERROR without answers
//	anything else (ok.*)                -> NOERROR with A 93.184.216.34 / AAAA 2001:db8::1
func mockReply(req *dns.Msg) *dns.Msg {
	q := req.Question[0]
	name := strings.ToLower(q.Name)
	m := new(dns.Msg)
	switch {
	case strings.HasPrefix(name, "servfail."):
		m.SetRcode(req, dns.RcodeServerFailure)
	case strings.HasPrefix(name, "nxdomain."):
		m.SetRcode(req, dns.RcodeNameError)
	case strings.HasPrefix(name, "refused."):
		m.SetRcode(req, dns.RcodeRefused)
	case strings.HasPrefix(name, "empty."):
		m.SetReply(req)
	default:
		m.SetReply(req)
		hdr := dns.RR_Header{Name: q.Name, Class: dns.ClassINET, Ttl: 60}
		if q.Qtype == dns.TypeAAAA {
			hdr.Rrtype = dns.TypeAAAA
			m.Answer = append(m.Answer, &dns.AAAA{Hdr: hdr, AAAA: net.ParseIP("2001:db8::1")})
		} else {
			hdr.Rrtype = dns.TypeA
			m.Answer = append(m.Answer, &dns.A{Hdr: hdr, A: net.ParseIP("93.184.216.34")})
		}
	}
	return m
}

func isSlowQuery(req *dns.Msg) bool {
	return strings.HasPrefix(strings.ToLower(req.Question[0].Name), "slow.")
}

// genSelfSignedCert creates a throwaway self-signed certificate for local
// TLS endpoints together with a pool trusting it (tests only).
func genSelfSignedCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}, pool
}
