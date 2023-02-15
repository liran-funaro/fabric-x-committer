package fsc

import (
	"strings"

	"github.com/hyperledger-labs/fabric-smart-client/pkg/api"
	"github.com/hyperledger-labs/fabric-smart-client/platform/view"
)

type Ref string

func (r *Ref) Pkg() string {
	pkg, _ := r.split()
	return strings.Join(pkg, "/")
}

func (r *Ref) Name() string {
	_, name := r.split()
	return name
}

func (r *Ref) split() ([]string, string) {
	path := strings.Split(string(*r), "/")
	return path[0 : len(path)-1], path[len(path)-1]
}

type Provider interface {
	GetSDK(Ref) api.SDK
	GetViewFactory(Ref) view.Factory
	GetView(Ref) view.View
}
