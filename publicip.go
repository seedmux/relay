package main

import (
	"errors"
	"net"
	"strings"
	"time"

	"github.com/pion/stun/v4"
)

// detectPublicIP asks public STUN servers what address this server's traffic
// comes from, so a self-hosted relay needs no -turn-public-ip.
func detectPublicIP() (net.IP, error) {
	for _, server := range []string{"stun.cloudflare.com:3478", "stun.l.google.com:19302"} {
		if ip := stunPublicIP(server); ip != nil {
			return ip, nil
		}
	}
	return nil, errors.New("could not find this server's public IP over STUN; pass -turn-public-ip")
}

func stunPublicIP(server string) net.IP {
	conn, err := net.DialTimeout("udp4", server, 3*time.Second)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	if _, err := conn.Write(request.Raw); err != nil {
		return nil
	}
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return nil
	}
	response := &stun.Message{Raw: buf[:n]}
	if response.Decode() != nil {
		return nil
	}
	var mapped stun.XORMappedAddress
	if mapped.GetFrom(response) != nil {
		return nil
	}
	return mapped.IP
}

// placeholderDomain catches example domains copied from documentation
func placeholderDomain(domain string) bool {
	d := strings.ToLower(strings.TrimSuffix(domain, "."))
	if d == "localhost" || strings.ContainsAny(d, "<>") {
		return true
	}
	for _, suffix := range []string{"example.com", "example.org", "example.net"} {
		if d == suffix || strings.HasSuffix(d, "."+suffix) {
			return true
		}
	}
	for _, tld := range []string{".example", ".test", ".invalid", ".localhost"} {
		if strings.HasSuffix(d, tld) {
			return true
		}
	}
	return false
}
