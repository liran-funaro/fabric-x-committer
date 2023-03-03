package workload

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
	"gopkg.in/yaml.v3"
)

type Profile struct {
	Name        string
	Description string

	Block BlockProfile

	Transaction TransactionProfile

	Conflicts struct {
		Scenario    *ScenarioConflicts
		Statistical *StatisticalConflicts
	}
}

func Always(size int) []test.DiscreteValue {
	return []test.DiscreteValue{{float64(size), 1}}
}

type BlockProfile struct {
	Count int64
	Size  int64
}
type TransactionProfile struct {
	SerialNumberSize []test.DiscreteValue
	OutputSize       []test.DiscreteValue
	Signature        sigverification_test.SignatureProfile
}
type ScenarioConflicts map[string]struct {
	InvalidSignature bool
	DoubleSpends     map[int]string
}
type ConflictProfile struct {
	Scenario    *ScenarioConflicts
	Statistical *StatisticalConflicts
}
type StatisticalConflicts struct {
	InvalidSignature test.Percentage
	DoubleSpends     test.Percentage
}

func LoadProfileFromYaml(yamlPath string) *Profile {
	pp := &Profile{}

	yamlPath, err := filepath.Abs(yamlPath)
	utils.Must(err)
	fmt.Printf("Loading profile from %s\n", yamlPath)

	yamlFile, err := ioutil.ReadFile(yamlPath)
	utils.Must(err)

	err = yaml.Unmarshal(yamlFile, pp)
	utils.Must(err)

	PrintProfile(pp)

	return pp
}

func PrintProfile(profile *Profile) {
	d, err := yaml.Marshal(profile)
	utils.Must(err)
	fmt.Printf("##############################\n")
	fmt.Printf("### Profile\n")
	fmt.Printf("##############################\n\n")
	fmt.Printf("%s\n\n", string(d))
	fmt.Printf("##############################\n\n")
}

func WriteProfileToBlockFile(writer io.Writer, profile *Profile) {
	data, err := json.Marshal(profile)
	utils.Must(err)
	WriteToFile(writer, data)
}

func ReadProfileFromBlockFile(reader io.Reader) *Profile {
	data := Next(reader)
	pp := &Profile{}
	err := json.Unmarshal(data, pp)
	utils.Must(err)

	//PrintProfile(pp)

	return pp
}
