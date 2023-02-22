package fsc

const nodeTemplate = `/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	fscnode "github.com/hyperledger-labs/fabric-smart-client/node"
	{{- if ne (.NodePort .Peer "Operations") 0 }}
	backend2 "github.ibm.com/decentralized-trust-research/fts-sc/demo/app/backend"
	server2 "github.ibm.com/decentralized-trust-research/fts-sc/demo/app/server"
	{{ end }}
	//views "github.ibm.com/decentralized-trust-research/fts-sc/demo/views"
	//sdk1 "github.ibm.com/decentralized-trust-research/fts-sc/platform/fabric/sdk"
	//sdk "github.ibm.com/decentralized-trust-research/fts-sc/token/sdk"
	{{ if InstallView }}viewregistry "github.com/hyperledger-labs/fabric-smart-client/platform/view"{{ end }}
	{{- range .Imports }}
	{{ Alias . }} "{{ . }}"{{ end }}
)

func main() {
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
			backend := backend2.New(backend2.NewClient(viewregistry.GetManager(n)))
			server := server2.NewServer(":{{ .NodePort .Peer "Operations" }}", backend)
			if err := server.Start(); err != nil {
				panic(err)
			}
		}()
		{{ end }}
		return nil
	})
}
`
