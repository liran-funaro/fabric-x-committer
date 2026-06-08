# Unmaintained Imports Report

---

## ⚠️ DIRECT UNMAINTAINED IMPORTS (1 found)

- **📦 github.com/mitchellh/mapstructure**

  **📝 Imported in your code at:**

  - `cmd/config/config_decoder.go:19`
  - `loadgen/workload/distributions_test.go:16`

---

## 🎯 BLAME POINT: `github.com/cockroachdb/errors`
Responsible for 3 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/gogo/protobuf**
    - `github.com/cockroachdb/errors` -> `github.com/gogo/protobuf`

  - **📦 github.com/kr/pretty**
    - `github.com/cockroachdb/errors` -> `github.com/kr/pretty`
    - `github.com/cockroachdb/errors` -> `github.com/getsentry/sentry-go` *(indirect)* -> `github.com/kr/pretty`
    - ... and 4 more

  - **📦 github.com/kr/text**
    - `github.com/cockroachdb/errors` -> `github.com/kr/pretty` -> `github.com/kr/text`
    - `github.com/cockroachdb/errors` -> `github.com/getsentry/sentry-go` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`
    - ... and 4 more

- **📝 Imported in your code at:**

  - `cmd/committer/config.go:10`
  - `cmd/committer/healthcheck_cmd_test.go:13`
  - `cmd/committer/start_cmd.go:13`
  - ... and 84 more location(s)

---

## 🎯 BLAME POINT: `github.com/consensys/gnark-crypto`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/kr/pretty**
    - `github.com/consensys/gnark-crypto` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - `github.com/consensys/gnark-crypto` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`

- **📝 Imported in your code at:**

  - `utils/signature/verify_bls.go:11`
  - `utils/signature/verify_schemes_test.go:20`
  - `utils/testsig/digest_signer.go:16`
  - ... and 3 more location(s)

---

## 🎯 BLAME POINT: `github.com/fsouza/go-dockerclient`
Responsible for 4 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-logr/stdr**
    - `github.com/fsouza/go-dockerclient` -> `github.com/moby/moby/client` *(indirect)* -> `go.opentelemetry.io/auto/sdk` -> `go.opentelemetry.io/otel` -> `github.com/go-logr/stdr`
    - `github.com/fsouza/go-dockerclient` -> `github.com/moby/moby/client` -> `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` -> `go.opentelemetry.io/otel` -> `github.com/go-logr/stdr`
    - ... and 16 more

  - **📦 github.com/gogo/protobuf**
    - `github.com/fsouza/go-dockerclient` -> `github.com/moby/moby/client` -> `github.com/containerd/errdefs/pkg` *(indirect)* -> `github.com/gogo/protobuf`

  - **📦 github.com/kr/pretty**
    - `github.com/fsouza/go-dockerclient` -> `github.com/moby/moby/client` -> `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` -> `go.opentelemetry.io/otel` -> `go.opentelemetry.io/auto/sdk` *(indirect)* -> `github.com/kr/pretty`
    - `github.com/fsouza/go-dockerclient` -> `github.com/moby/moby/client` -> `go.opentelemetry.io/otel/trace` -> `go.opentelemetry.io/otel` -> `go.opentelemetry.io/auto/sdk` *(indirect)* -> `github.com/kr/pretty`
    - ... and 22 more

  - **📦 github.com/kr/text**
    - `github.com/fsouza/go-dockerclient` -> `github.com/moby/moby/client` -> `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` -> `go.opentelemetry.io/otel` *(indirect)* -> `github.com/kr/text`
    - `github.com/fsouza/go-dockerclient` -> `github.com/moby/moby/client` -> `go.opentelemetry.io/otel/trace` -> `go.opentelemetry.io/otel` *(indirect)* -> `github.com/kr/text`
    - ... and 30 more

- **📝 Imported in your code at:**

  - `integration/runner/cluster_controllers_test.go:14`
  - `utils/test/docker.go:13`
  - `utils/testdb/container.go:22`
  - ... and 1 more location(s)

---

## 🎯 BLAME POINT: `github.com/gavv/httpexpect/v2`
Responsible for 5 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/kr/pretty**
    - `github.com/gavv/httpexpect/v2` -> `moul.io/http2curl/v2` -> `github.com/tailscale/depaware` -> `github.com/pkg/diff` -> `github.com/sergi/go-diff` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - `github.com/gavv/httpexpect/v2` -> `moul.io/http2curl/v2` -> `github.com/tailscale/depaware` -> `github.com/pkg/diff` -> `github.com/sergi/go-diff` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`

  - **📦 github.com/xeipuuv/gojsonpointer**
    - `github.com/gavv/httpexpect/v2` *(indirect)* -> `github.com/xeipuuv/gojsonpointer`
    - `github.com/gavv/httpexpect/v2` -> `github.com/xeipuuv/gojsonschema` *(indirect)* -> `github.com/xeipuuv/gojsonpointer`

  - **📦 github.com/xeipuuv/gojsonreference**
    - `github.com/gavv/httpexpect/v2` -> `github.com/xeipuuv/gojsonschema` -> `github.com/xeipuuv/gojsonreference`

  - **📦 github.com/xeipuuv/gojsonschema**
    - `github.com/gavv/httpexpect/v2` -> `github.com/xeipuuv/gojsonschema`

- **📝 Imported in your code at:**

  - `loadgen/client_test.go:19`

---

## 🎯 BLAME POINT: `github.com/go-playground/validator/v10`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-playground/locales**
    - `github.com/go-playground/validator/v10` -> `github.com/go-playground/locales`
    - `github.com/go-playground/validator/v10` -> `github.com/go-playground/universal-translator` -> `github.com/go-playground/locales`

  - **📦 github.com/go-playground/universal-translator**
    - `github.com/go-playground/validator/v10` -> `github.com/go-playground/universal-translator`

- **📝 Imported in your code at:**

  - `cmd/config/app_config.go:18`

---

## 🎯 BLAME POINT: `github.com/grpc-ecosystem/grpc-gateway/v2`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/kr/pretty**
    - `github.com/grpc-ecosystem/grpc-gateway/v2` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - `github.com/grpc-ecosystem/grpc-gateway/v2` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`

- **📝 Imported in your code at:**

  - `api/servicepb/loadgen.pb.gw.go:17`
  - `api/servicepb/loadgen.pb.gw.go:18`
  - `loadgen/client.go:14`

---

## 🎯 BLAME POINT: `github.com/hyperledger/fabric-lib-go`
Responsible for 1 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/sykesm/zap-logfmt**
    - `github.com/hyperledger/fabric-lib-go` -> `github.com/sykesm/zap-logfmt`

- **📝 Imported in your code at:**

  - `cmd/cliutil/test_exports.go:18`
  - `cmd/config/app_config.go:19`
  - `cmd/config/app_config_test.go:16`
  - ... and 40 more location(s)

---

## 🎯 BLAME POINT: `github.com/hyperledger/fabric-x-common`
Responsible for 7 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/Knetic/govaluate**
    - `github.com/hyperledger/fabric-x-common` -> `github.com/Knetic/govaluate`

  - **📦 github.com/davecgh/go-spew**
    - `github.com/hyperledger/fabric-x-common` -> `github.com/davecgh/go-spew`

  - **📦 github.com/go-logr/stdr**
    - `github.com/hyperledger/fabric-x-common` *(indirect)* -> `cloud.google.com/go/longrunning` *(indirect)* -> `go.opentelemetry.io/auto/sdk` -> `go.opentelemetry.io/otel` -> `github.com/go-logr/stdr`
    - `github.com/hyperledger/fabric-x-common` *(indirect)* -> `cloud.google.com/go/longrunning` *(indirect)* -> `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` -> `go.opentelemetry.io/otel` -> `github.com/go-logr/stdr`
    - ... and 30 more

  - **📦 github.com/kilic/bls12-381**
    - `github.com/hyperledger/fabric-x-common` -> `github.com/IBM/idemix` -> `github.com/IBM/mathlib` -> `github.com/kilic/bls12-381`
    - `github.com/hyperledger/fabric-x-common` -> `github.com/IBM/idemix` -> `github.com/IBM/idemix/bccsp/schemes/aries` -> `github.com/IBM/mathlib` -> `github.com/kilic/bls12-381`
    - ... and 6 more

  - **📦 github.com/kr/pretty**
    - `github.com/hyperledger/fabric-x-common` *(indirect)* -> `github.com/bitfield/gotestdox` -> `github.com/rogpeppe/go-internal` -> `gopkg.in/errgo.v2` *(indirect)* -> `github.com/kr/pretty`
    - `github.com/hyperledger/fabric-x-common` *(indirect)* -> `bitbucket.org/creachadair/stringset` -> `honnef.co/go/tools` -> `github.com/rogpeppe/go-internal` -> `gopkg.in/errgo.v2` *(indirect)* -> `github.com/kr/pretty`
    - ... and 41 more

  - **📦 github.com/kr/text**
    - `github.com/hyperledger/fabric-x-common` -> `github.com/IBM/idemix` -> `github.com/IBM/mathlib` *(indirect)* -> `github.com/kr/text`
    - `github.com/hyperledger/fabric-x-common` *(indirect)* -> `cloud.google.com/go/longrunning` *(indirect)* -> `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` -> `go.opentelemetry.io/otel` *(indirect)* -> `github.com/kr/text`
    - ... and 59 more

  - **📦 github.com/sykesm/zap-logfmt**
    - `github.com/hyperledger/fabric-x-common` -> `github.com/IBM/idemix` -> `github.com/sykesm/zap-logfmt`

- **📝 Imported in your code at:**

  - `api/servicepb/common.pb.go:15`
  - `api/servicepb/common.pb.go:16`
  - `api/servicepb/coordinator.pb.go:15`
  - ... and 265 more location(s)

---

## 🎯 BLAME POINT: `github.com/prometheus/client_golang`
Responsible for 4 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/beorn7/perks**
    - `github.com/prometheus/client_golang` -> `github.com/beorn7/perks`
    - `github.com/prometheus/client_golang` -> `github.com/prometheus/common` *(indirect)* -> `github.com/beorn7/perks`

  - **📦 github.com/kr/pretty**
    - `github.com/prometheus/client_golang` *(indirect)* -> `github.com/kr/pretty`
    - `github.com/prometheus/client_golang` -> `github.com/prometheus/common` *(indirect)* -> `github.com/rogpeppe/go-internal` -> `gopkg.in/errgo.v2` *(indirect)* -> `github.com/kr/pretty`
    - ... and 1 more

  - **📦 github.com/kr/text**
    - `github.com/prometheus/client_golang` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`
    - `github.com/prometheus/client_golang` -> `github.com/prometheus/common` *(indirect)* -> `github.com/rogpeppe/go-internal` -> `gopkg.in/errgo.v2` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`
    - ... and 1 more

  - **📦 github.com/munnerz/goautoneg**
    - `github.com/prometheus/client_golang` -> `github.com/prometheus/common` -> `github.com/munnerz/goautoneg`

- **📝 Imported in your code at:**

  - `loadgen/metrics/metrics.go:13`
  - `service/coordinator/dependencygraph/metrics.go:10`
  - `service/coordinator/metrics.go:10`
  - ... and 13 more location(s)

---

## 🎯 BLAME POINT: `github.com/spf13/viper`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/kr/pretty**
    - `github.com/spf13/viper` -> `github.com/spf13/cast` *(indirect)* -> `github.com/kr/pretty`
    - `github.com/spf13/viper` -> `github.com/spf13/cast` *(indirect)* -> `github.com/rogpeppe/go-internal` -> `gopkg.in/errgo.v2` *(indirect)* -> `github.com/kr/pretty`
    - ... and 1 more

  - **📦 github.com/kr/text**
    - `github.com/spf13/viper` -> `github.com/spf13/cast` *(indirect)* -> `github.com/kr/text`
    - `github.com/spf13/viper` -> `github.com/spf13/cast` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`
    - ... and 2 more

- **📝 Imported in your code at:**

  - `cmd/config/app_config.go:20`
  - `cmd/config/config_decoder.go:20`
  - `cmd/config/config_decoder_test.go:15`
  - ... and 2 more location(s)

---

## 🎯 BLAME POINT: `github.com/stretchr/testify`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - `github.com/stretchr/testify` -> `github.com/davecgh/go-spew`

  - **📦 github.com/pmezard/go-difflib**
    - `github.com/stretchr/testify` -> `github.com/pmezard/go-difflib`

- **📝 Imported in your code at:**

  - `api/servicepb/height_test.go:12`
  - `cmd/cliutil/test_exports.go:21`
  - `cmd/cliutil/test_exports.go:22`
  - ... and 151 more location(s)

---

## 🎯 BLAME POINT: `github.com/yugabyte/pgx/v5`
Responsible for 3 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/jackc/pgpassfile**
    - `github.com/yugabyte/pgx/v5` -> `github.com/jackc/pgpassfile`

  - **📦 github.com/kr/pretty**
    - `github.com/yugabyte/pgx/v5` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - `github.com/yugabyte/pgx/v5` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`

- **📝 Imported in your code at:**

  - `service/query/batcher.go:16`
  - `service/query/batcher.go:17`
  - `service/query/query.go:18`
  - ... and 10 more location(s)

---

## 🎯 BLAME POINT: `go.uber.org/zap`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/kr/pretty**
    - `go.uber.org/zap` -> `go.uber.org/multierr` -> `honnef.co/go/tools` -> `github.com/rogpeppe/go-internal` -> `gopkg.in/errgo.v2` *(indirect)* -> `github.com/kr/pretty`
    - `go.uber.org/zap` -> `go.uber.org/multierr` -> `honnef.co/go/tools` -> `github.com/rogpeppe/go-internal` -> `github.com/pkg/diff` -> `github.com/sergi/go-diff` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - `go.uber.org/zap` *(indirect)* -> `github.com/kr/text`
    - `go.uber.org/zap` -> `go.uber.org/multierr` -> `honnef.co/go/tools` -> `github.com/rogpeppe/go-internal` -> `gopkg.in/errgo.v2` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`
    - ... and 1 more

- **📝 Imported in your code at:**

  - `service/sidecar/mapping.go:18`
  - `service/vc/validator.go:15`
  - `utils/retry/executor.go:16`

---

## 🎯 BLAME POINT: `google.golang.org/grpc`
Responsible for 3 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-logr/stdr**
    - `google.golang.org/grpc` -> `go.opentelemetry.io/otel` -> `github.com/go-logr/stdr`
    - `google.golang.org/grpc` *(indirect)* -> `go.opentelemetry.io/auto/sdk` -> `go.opentelemetry.io/otel` -> `github.com/go-logr/stdr`
    - ... and 10 more

  - **📦 github.com/kr/pretty**
    - `google.golang.org/grpc` -> `go.opentelemetry.io/otel` -> `go.opentelemetry.io/auto/sdk` *(indirect)* -> `github.com/kr/pretty`
    - `google.golang.org/grpc` -> `go.opentelemetry.io/otel/metric` -> `go.opentelemetry.io/otel` -> `go.opentelemetry.io/auto/sdk` *(indirect)* -> `github.com/kr/pretty`
    - ... and 16 more

  - **📦 github.com/kr/text**
    - `google.golang.org/grpc` -> `go.opentelemetry.io/otel` *(indirect)* -> `github.com/kr/text`
    - `google.golang.org/grpc` -> `go.opentelemetry.io/otel/metric` -> `go.opentelemetry.io/otel` *(indirect)* -> `github.com/kr/text`
    - ... and 28 more

- **📝 Imported in your code at:**

  - `api/servicepb/coordinator_grpc.pb.go:17`
  - `api/servicepb/coordinator_grpc.pb.go:18`
  - `api/servicepb/coordinator_grpc.pb.go:19`
  - ... and 97 more location(s)

---

## 🎯 BLAME POINT: `gotest.tools/gotestsum` (tool)
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/kr/pretty**
    - `gotest.tools/gotestsum` -> `github.com/bitfield/gotestdox` -> `github.com/rogpeppe/go-internal` -> `gopkg.in/errgo.v2` *(indirect)* -> `github.com/kr/pretty`
    - `gotest.tools/gotestsum` -> `github.com/bitfield/gotestdox` -> `github.com/rogpeppe/go-internal` -> `github.com/pkg/diff` -> `github.com/sergi/go-diff` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - `gotest.tools/gotestsum` -> `github.com/bitfield/gotestdox` -> `github.com/rogpeppe/go-internal` -> `gopkg.in/errgo.v2` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`
    - `gotest.tools/gotestsum` -> `github.com/bitfield/gotestdox` -> `github.com/rogpeppe/go-internal` -> `github.com/pkg/diff` -> `github.com/sergi/go-diff` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`

- *(Import location not found in code)*

---

## 🎯 BLAME POINT: `mvdan.cc/gofumpt` (tool)
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/kr/pretty**
    - `mvdan.cc/gofumpt` *(indirect)* -> `github.com/kr/pretty`
    - `mvdan.cc/gofumpt` -> `github.com/rogpeppe/go-internal` -> `gopkg.in/errgo.v2` *(indirect)* -> `github.com/kr/pretty`
    - ... and 1 more

  - **📦 github.com/kr/text**
    - `mvdan.cc/gofumpt` *(indirect)* -> `github.com/kr/text`
    - `mvdan.cc/gofumpt` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`
    - ... and 2 more

- *(Import location not found in code)*

---

## NOT IN GO.MOD (17 found)

- `github.com/KyleBanks/depth`
- `github.com/benbjohnson/clock`
- `github.com/davidlazar/go-crypto`
- `github.com/dgryski/go-rendezvous`
- `github.com/ghodss/yaml`
- `github.com/hyperledger-aries/aries-bbs-go`
- `github.com/hyperledger-labs/jsonld-vc-bbs-go`
- `github.com/jackpal/go-nat-pmp`
- `github.com/josharian/intern`
- `github.com/marten-seemann/tcp`
- `github.com/mikioh/tcpinfo`
- `github.com/mikioh/tcpopt`
- `github.com/minio/sha256-simd`
- `github.com/pbnjay/memory`
- `github.com/remyoudompheng/bigfft`
- `github.com/spaolacci/murmur3`
- `github.com/whyrusleeping/go-keyspace`

---

## SUMMARY

**Total unmaintained imports analyzed:** 35
- In go.mod: 18
- Direct unmaintained imports (this repo to blame): 1
- Indirect unmaintained imports grouped by 16 external blame point(s)
- Not in go.mod: 17

