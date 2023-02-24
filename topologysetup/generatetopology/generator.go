package generatetopology

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/template"

	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup/fsc"
)

type TemplateInput struct {
	Views         []fsc.Ref `yaml:"views"`
	ViewFactories []fsc.Ref `yaml:"view-factories"`
}

type TemplateConfig struct {
	input      *TemplateInput
	aliases    map[string]string
	suffixSeen map[string]int
}

func NewTemplateConfig(input *TemplateInput) *TemplateConfig {
	return &TemplateConfig{
		input:      input,
		aliases:    map[string]string{},
		suffixSeen: map[string]int{},
	}
}

func (a *TemplateConfig) Imports() []string {
	pkgs := make(map[string]bool)
	for _, imp := range append(a.input.ViewFactories, a.input.Views...) {
		pkgs[imp.Pkg()] = true
	}
	imps := make([]string, 0)
	for pkg := range pkgs {
		alias := a.alias(pkg)
		imps = append(imps, fmt.Sprintf("%s \"%s\"", alias, pkg))
	}
	return imps
}

func (a *TemplateConfig) ViewFactories() map[fsc.Ref]string {
	return a.getMap(a.input.ViewFactories)
}

func (a *TemplateConfig) Views() map[fsc.Ref]string {
	return a.getMap(a.input.Views)
}

func (a *TemplateConfig) getMap(m []fsc.Ref) map[fsc.Ref]string {
	refs := make(map[fsc.Ref]string, len(m))
	for _, view := range m {
		refs[view] = fmt.Sprintf("&%s.%s{}", a.alias(view.Pkg()), view.Name())
	}
	return refs
}

func (a *TemplateConfig) alias(pkg string) string {
	if alias, ok := a.aliases[pkg]; ok {
		return alias
	}
	suffix := suffix(pkg)
	if count, seen := a.suffixSeen[suffix]; seen {
		a.suffixSeen[suffix] = count + 1
		a.aliases[pkg] = suffix + strconv.Itoa(count+1)
		return a.aliases[pkg]
	}
	a.suffixSeen[suffix] = 0
	a.aliases[pkg] = suffix
	return suffix
}

func suffix(pkg string) string {
	path := strings.Split(pkg, "/")
	return path[len(path)-1]
}

func Generate(config *fsc.Config, out string) error {
	os.Remove(out)
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	generated, err := os.Create(out + "/main.go")
	if err != nil {
		return err
	}
	defer generated.Close()

	t, err := template.New("topology-setup").Parse(topologyTemplate)
	if err != nil {
		return err
	}

	return t.Execute(io.MultiWriter(generated), NewTemplateConfig(&TemplateInput{
		ViewFactories: config.AllViewFactories(),
		Views:         config.AllViews(),
	}))
}

const topologyTemplate = `
package main

import (
	"os"

	"github.com/spf13/pflag"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup/fsc"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup/generatetopology"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.com/hyperledger-labs/fabric-smart-client/platform/view"
{{ range .Imports }}
	{{ . }}
{{- end }}
)

func main() {
	outputDir := pflag.String("output-dir", os.Getenv("PWD")+"/out", "Output dir for config tree.")
	fabBinsDir := pflag.String("fab-bin-dir", os.Getenv("GOPATH")+"/src/github.com/decentralized-trust-research/scalable-committer/eval/deployments/bins/osx/", "Path to directory with the fabric executables (configtx, peer, etc.)")

	config.ParseFlags()
	c := generatetopology.ReadConfig()

	generatetopology.GenerateAll(c.Fabric, c.Fsc, NewProvider(), *outputDir, *fabBinsDir)
}

type MyProvider struct{
	viewFactories map[fsc.Ref]view.Factory
	views map[fsc.Ref]view.View
}

func NewProvider() fsc.Provider {
	return &MyProvider{
		viewFactories: map[fsc.Ref]view.Factory{
{{ range $Key, $Value := .ViewFactories }}
			"{{ $Key }}": {{ $Value }},
{{- end }}
		},
		views: map[fsc.Ref]view.View{
{{ range $Key, $Value := .Views }}
			"{{ $Key }}": {{ $Value }},
{{- end }}
		},
	}
}

func (p *MyProvider) GetViewFactory(ref fsc.Ref) view.Factory {
	return p.viewFactories[ref]
}
func (p *MyProvider) GetView(ref fsc.Ref) view.View {
	return p.views[ref]
}
`
