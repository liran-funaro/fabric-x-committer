package fsc

const nodeTemplate = `/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	fscnode "github.com/hyperledger-labs/fabric-smart-client/node"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/config"
	{{- if ne (.NodePort .Peer "Operations") 0 }}
	backend2 "github.ibm.com/decentralized-trust-research/fts-sc/demo/app/backend"
	server2 "github.ibm.com/decentralized-trust-research/fts-sc/demo/app/server"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup/fsc"
	{{ end }}
	{{ if InstallView }}viewregistry "github.com/hyperledger-labs/fabric-smart-client/platform/view"{{ end }}
	{{- range .Imports }}
	{{ Alias . }} "{{ . }}"{{ end }}
)

func main() {
	config.ParseFlags()

	n := fscnode.New()
	{{- range .SDKs }}
	n.InstallSDK({{ .Type }})
	{{ end }}
	n.Execute(func() error {
		{{- if InstallView }}
		registry := viewregistry.GetRegistry(n)

		{{- range .Factories }}
		if err := registry.RegisterFactory("{{ .Id }}", {{ .Type }}); err != nil {
			return err
		}
		{{- end }}

		{{ $initializedManager := false }}
		{{- range .Factories }}
		{{- if and (gt (len .Id) 4) (eq (slice .Id 0 4) "init") }}
		{{- if eq $initializedManager false }}
		// Instantiate factory views with ID starting with 'init'
		manager := viewregistry.GetManager(n)

		{{- $initializedManager = true }}
		{{- end }}
		if view, err := manager.NewView("{{ .Id }}", nil); err != nil {
			return err
		} else if _, err = manager.InitiateView(view); err != nil {
			return err
		}
		{{- end }}
		{{- end }}

		{{- range .Responders }}
		registry.RegisterResponder({{ .Responder }}, {{ .Initiator }}){{ end }}
		{{ end }}

		{{- if ne (.NodePort .Peer "Operations") 0 }}
		// Launch web server
		go func() {
			c := fsc.ReadNodeConfig()
			backend := backend2.New(backend2.NewClient(viewregistry.GetManager(n)))
			var server *server2.Server
			if c == nil || c.TLSCert == "" || c.TLSKey == "" {
				server = server2.NewServer(":{{ .NodePort .Peer "Operations" }}", backend)
			} else {
				server = server2.NewTLSServer(":{{ .NodePort .Peer "Operations" }}", backend, c.TLSCert, c.TLSKey)
			}
			
			if err := server.Start(); err != nil {
				panic(err)
			}
		}()
		{{ end }}
		return nil
	})
}
`
