// Package origin decides where a call to Onplo goes. The command line and the OpenTofu provider
// both send their calls through it, so the rule about plain http has one home.
package origin

import (
	"fmt"
	"net"
	"net/url"
)

// Server is the contract's server, or the same path on another origin when override names one
// (staging, or a stand-in on this machine). Plain http is refused except on loopback, so a key is
// never sent where anyone on the network can read it.
func Server(contract, override string) (string, error) {
	if override == "" {
		return contract, nil
	}
	o, err := url.Parse(override)
	if err != nil || o.Host == "" || (o.Path != "" && o.Path != "/") {
		return "", fmt.Errorf("%q is not an origin such as https://api.staging.onplo.com", override)
	}
	if o.Scheme != "https" && !(o.Scheme == "http" && isLoopback(o.Hostname())) {
		return "", fmt.Errorf("%q is not https; plain http is only for this machine", override)
	}
	c, err := url.Parse(contract)
	if err != nil {
		return "", err
	}
	return o.Scheme + "://" + o.Host + c.Path, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
