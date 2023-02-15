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
// Here are some random comments

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
		}{{ end }}
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
