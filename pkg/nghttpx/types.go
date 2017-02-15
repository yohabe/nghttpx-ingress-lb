/*
Copyright 2015 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/**
 * Copyright 2016, Z Lab Corporation. All rights reserved.
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package nghttpx

import (
	"runtime"
	"strconv"
)

// Interface is the API to update underlying load balancer.
type Interface interface {
	// Start starts a nghttpx process, and wait.  If stopCh becomes readable, kill nghttpx process, and return.
	Start(stopCh <-chan struct{})
	// CheckAndReload checks whether the nghttpx configuration changed, and if so, make nghttpx reload its configuration.  If reloading
	// is required, and it successfully issues reloading, returns true.  If there is no need to reloading, it returns false.  On error,
	// it returns false, and non-nil error.
	CheckAndReload(ingressCfg *IngressConfig) (bool, error)
}

// IngressConfig describes an nghttpx configuration
type IngressConfig struct {
	Upstreams      []*Upstream
	TLS            bool
	DefaultTLSCred *TLSCred
	SubTLSCred     []*TLSCred
	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx-n
	// Set the number of worker threads.
	Workers string
	// ExtraConfig is the extra configurations in a format that nghttpx accepts in --conf.
	ExtraConfig string
	// TLSCertificate    string
	// TLSCertificateKey string
}

// NewIngressConfig returns new IngressConfig.  Workers is initialized as the number of CPU cores.
func NewIngressConfig() *IngressConfig {
	return &IngressConfig{
		Workers: strconv.Itoa(runtime.NumCPU()),
	}
}

// Upstream describes an nghttpx upstream
type Upstream struct {
	Name     string
	Host     string
	Path     string
	Backends []UpstreamServer
}

// UpstreamByNameServers sorts upstreams by name
type UpstreamByNameServers []*Upstream

func (c UpstreamByNameServers) Len() int      { return len(c) }
func (c UpstreamByNameServers) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c UpstreamByNameServers) Less(i, j int) bool {
	return c[i].Name < c[j].Name
}

type Affinity string

const (
	AffinityNone = "none"
	AffinityIP   = "ip"
)

type Protocol string

const (
	// HTTP/2 protocol
	ProtocolH2 = "h2"
	// HTTP/1.1 protocol
	ProtocolH1 = "http/1.1"
)

// UpstreamServer describes a server in an nghttpx upstream
type UpstreamServer struct {
	Address  string
	Port     string
	Protocol Protocol
	TLS      bool
	SNI      string
	DNS      bool
	Affinity Affinity
}

// UpstreamServerByAddrPort sorts upstream servers by address and port
type UpstreamServerByAddrPort []UpstreamServer

func (c UpstreamServerByAddrPort) Len() int      { return len(c) }
func (c UpstreamServerByAddrPort) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c UpstreamServerByAddrPort) Less(i, j int) bool {
	iName := c[i].Address
	jName := c[j].Address
	if iName != jName {
		return iName < jName
	}

	iU := c[i].Port
	jU := c[j].Port
	return iU < jU
}

// TLS server private key and certificate file path
type TLSCred struct {
	Key  ChecksumFile
	Cert ChecksumFile
}

type TLSCredKeyLess []*TLSCred

func (c TLSCredKeyLess) Len() int      { return len(c) }
func (c TLSCredKeyLess) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c TLSCredKeyLess) Less(i, j int) bool {
	return c[i].Key.Path < c[j].Key.Path
}

// NewDefaultServer return an UpstreamServer to be use as default server that returns 503.
func NewDefaultServer() UpstreamServer {
	return UpstreamServer{
		Address: "127.0.0.1",
		Port:    "8181",
		// Update DefaultPortBackendConfig() too.
		Protocol: ProtocolH1,
		Affinity: AffinityNone,
	}
}

// backend configuration obtained from ingress annotation, specified per service port
type PortBackendConfig struct {
	// backend application protocol.  At the moment, this should be either ProtocolH2 or ProtocolH1.
	Proto Protocol `json:"proto,omitempty"`
	// true if backend connection requires TLS
	TLS bool `json:"tls,omitempty"`
	// SNI hostname for backend TLS connection
	SNI string `json:"sni,omitempty"`
	// DNS is true if backend hostname is resolved dynamically rather than start up or configuration reloading.
	DNS bool `json:"dns,omitempty"`
	// Affinity is session affinity method nghttpx supports.  See affinity parameter in backend option of nghttpx.
	Affinity Affinity `json:"affinity,omitempty"`
}

// ChecksumFile represents a file with path, its arbitrary content, and its checksum.
type ChecksumFile struct {
	Path     string
	Content  []byte
	Checksum string
}
