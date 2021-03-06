package test

import (
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/coredns/coredns/middleware/proxy"
	"github.com/coredns/coredns/middleware/test"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
)

func TestProxyWithHTTPCheckOK(t *testing.T) {
	log.SetOutput(ioutil.Discard)

	healthCheckServer := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "OK\n")
		}))
	defer healthCheckServer.Close()

	healthCheckURL, err := url.Parse(healthCheckServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	healthCheckPort := healthCheckURL.Port()

	name, rm, err := test.TempFile(".", exampleOrg)
	if err != nil {
		t.Fatalf("failed to create zone: %s", err)
	}
	defer rm()

	// We have to bind to 127.0.0.1 because the server started by
	// httptest.NewServer does, and the IP addresses of the backend
	// DNS and HTTP servers must match.
	authoritativeCorefile := `example.org:0 {
	   bind 127.0.0.1
       file ` + name + `
}
`

	authoritativeInstance, err := CoreDNSServer(authoritativeCorefile)
	if err != nil {
		t.Fatalf("Could not get CoreDNS authoritative instance: %s", err)
	}

	authoritativeAddr, _ := CoreDNSServerPorts(authoritativeInstance, 0)
	if authoritativeAddr == "" {
		t.Fatalf("Could not get CoreDNS authoritative instance UDP listening port")
	}
	defer authoritativeInstance.Stop()

	proxyCorefile := `example.org:0 {
    proxy . ` + authoritativeAddr + ` {
		health_check /health:` + healthCheckPort + ` 1s

	}
}
`

	proxyInstance, err := CoreDNSServer(proxyCorefile)
	if err != nil {
		t.Fatalf("Could not get CoreDNS proxy instance: %s", err)
	}

	proxyAddr, _ := CoreDNSServerPorts(proxyInstance, 0)
	if proxyAddr == "" {
		t.Fatalf("Could not get CoreDNS proxy instance UDP listening port")
	}
	defer proxyInstance.Stop()

	p := proxy.NewLookup([]string{proxyAddr})
	state := request.Request{W: &test.ResponseWriter{}, Req: new(dns.Msg)}
	resp, err := p.Lookup(state, "example.org.", dns.TypeA)
	if err != nil {
		t.Fatal("Expected to receive reply, but didn't")
	}
	// expect answer section with A record in it
	if len(resp.Answer) == 0 {
		t.Fatalf("Expected to at least one RR in the answer section, got none: %s", resp)
	}
	if resp.Answer[0].Header().Rrtype != dns.TypeA {
		t.Errorf("Expected RR to A, got: %d", resp.Answer[0].Header().Rrtype)
	}
	if resp.Answer[0].(*dns.A).A.String() != "127.0.0.1" {
		t.Errorf("Expected 127.0.0.1, got: %s", resp.Answer[0].(*dns.A).A.String())
	}
}
