package workload

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"

	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"gopkg.in/yaml.v3"
)

type Profile struct {
	Name        string
	Description string

	Block BlockProfile

	Transaction TransactionProfile

	Conflicts *ConflictProfile
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
	Signature        signature.Profile
}
type TxAbsoluteOrder = uint64               // The absolute order of the TX since the beginning
type SNRelativeOrder = uint32               // The order of the SN within this TX
type TxSnAbsoluteOrder = string             // TxAbsoluteOrder:SNRelativeOrder
type DoubleSpendGroup = []TxSnAbsoluteOrder // TXs that share the same SN
type ScenarioConflicts struct {
	InvalidSignatures []TxAbsoluteOrder
	DoubleSpends      []DoubleSpendGroup
}
type ConflictProfile struct {
	Scenario    *ScenarioConflicts
	Statistical *StatisticalConflicts
}
type StatisticalConflicts struct {
	InvalidSignatures test.Percentage
	DoubleSpends      test.Percentage //TODO: AF Fix
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
