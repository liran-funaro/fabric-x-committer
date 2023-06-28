module github.ibm.com/decentralized-trust-research/scalable-committer/utils

go 1.20

require (
	github.com/docker/docker v20.10.17+incompatible
	github.com/docker/go-connections v0.4.0
	github.com/golang/protobuf v1.5.2
	github.com/hyperledger/fabric v1.4.0-rc1.0.20221121030113-dd63f08100c7
	github.com/hyperledger/fabric-protos-go v0.3.0
	github.com/onsi/gomega v1.24.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.13.0
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.13.0
	go.opentelemetry.io/otel v1.11.2
	go.opentelemetry.io/otel/exporters/jaeger v1.11.2
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.8.0
	go.opentelemetry.io/otel/sdk v1.11.2
	go.opentelemetry.io/otel/trace v1.11.2
	go.uber.org/zap v1.23.0
	google.golang.org/grpc v1.48.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.ibm.com/decentralized-trust-research/scalable-committer/protos/token => ../protos/token

require (
	github.com/IBM/idemix v0.0.2-0.20230403120754-d7dbe0340c4a // indirect
	github.com/IBM/mathlib v0.0.3-0.20230403084452-40ed1be38cf2 // indirect
	github.com/Microsoft/go-winio v0.5.1 // indirect
	github.com/benbjohnson/clock v1.3.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/consensys/bavard v0.1.13 // indirect
	github.com/consensys/gnark-crypto v0.9.1 // indirect
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/fsnotify/fsnotify v1.5.4 // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/hyperledger/fabric-amcl v0.0.0-20210603140002-2670f91851c8 // indirect
	github.com/kilic/bls12-381 v0.1.0 // indirect
	github.com/magiconair/properties v1.8.6 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/miekg/pkcs11 v1.1.1 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/mmcloughlin/addchain v0.4.0 // indirect
	github.com/moby/term v0.0.0-20210619224110-3f7ff695adc6 // indirect
	github.com/onsi/ginkgo v1.16.5 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.2 // indirect
	github.com/pelletier/go-toml v1.9.5 // indirect
	github.com/pelletier/go-toml/v2 v2.0.5 // indirect
	github.com/prometheus/client_model v0.2.1-0.20210607210712-147c58e9608a // indirect
	github.com/prometheus/common v0.37.0 // indirect
	github.com/prometheus/procfs v0.8.0 // indirect
	github.com/rogpeppe/go-internal v1.8.1 // indirect
	github.com/sirupsen/logrus v1.8.1 // indirect
	github.com/spf13/afero v1.8.2 // indirect
	github.com/spf13/cast v1.5.0 // indirect
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/subosito/gotenv v1.4.1 // indirect
	github.com/sykesm/zap-logfmt v0.0.4 // indirect
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/goleak v1.1.12 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	golang.org/x/crypto v0.1.0 // indirect
	golang.org/x/net v0.1.0 // indirect
	golang.org/x/sys v0.2.0 // indirect
	golang.org/x/text v0.4.0 // indirect
	golang.org/x/time v0.0.0-20220722155302-e5dcc9cfc0b9 // indirect
	google.golang.org/genproto v0.0.0-20220808204814-fd01256a5276 // indirect
	google.golang.org/protobuf v1.28.1 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gotest.tools/v3 v3.0.3 // indirect
	rsc.io/tmplfunc v0.0.3 // indirect
)
