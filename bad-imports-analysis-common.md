# Unmaintained Imports Report

---

## ⚠️ DIRECT UNMAINTAINED IMPORTS (1 found)

**🔴 THIS REPOSITORY IS TO BLAME**

These packages are directly imported in your code and should be removed.

- **📦 github.com/davecgh/go-spew**

  **Imported at:**

  - `common/ledger/blkstorage/blockfile_mgr.go:16`
  - `common/ledger/blkstorage/reset_test.go:14`
  - `common/ledger/blkstorage/blockfile_helper.go:15`

---

## 🎯 BLAME POINT: `cloud.google.com/go/longrunning`
Responsible for 1 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-logr/stdr**
    - Path: `cloud.google.com/go/longrunning` -> `github.com/go-logr/stdr`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-common` -> `cloud.google.com/go/longrunning`

- **📝 Direct dependencies imported in your code:**

  - `cloud.google.com/go/longrunning` (not found in code)

---

## 🎯 BLAME POINT: `github.com/IBM/idemix`
Responsible for 1 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/sykesm/zap-logfmt**
    - Path: `github.com/IBM/idemix` -> `github.com/sykesm/zap-logfmt`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-common` -> `github.com/IBM/idemix`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/IBM/idemix`** is imported at:
    - `protoutil/blockutils.go:19`
    - `msp/factory.go:10`
    - ... and 3 more location(s)

---

## 🎯 BLAME POINT: `github.com/IBM/mathlib`
Responsible for 1 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/kilic/bls12-381**
    - Path: `github.com/IBM/mathlib` -> `github.com/kilic/bls12-381`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-common` -> `github.com/IBM/idemix` -> `github.com/IBM/mathlib`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/IBM/idemix` -> `github.com/IBM/idemix/bccsp/types` -> `github.com/IBM/mathlib`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/IBM/idemix` -> `github.com/hyperledger/aries-bbs-go` -> `github.com/IBM/mathlib`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/IBM/idemix` -> `github.com/IBM/idemix/bccsp/schemes/aries` -> `github.com/IBM/mathlib`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/IBM/idemix` -> `github.com/IBM/idemix/bccsp/schemes/weak-bb` -> `github.com/IBM/mathlib`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/IBM/idemix`** is imported at:
    - `protoutil/blockutils.go:19`
    - `msp/factory.go:10`
    - ... and 3 more location(s)

---

## 🎯 BLAME POINT: `github.com/cockroachdb/errors`
Responsible for 3 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/gogo/protobuf**
    - Path: `github.com/cockroachdb/errors` -> `github.com/gogo/protobuf`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/cockroachdb/errors` -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/cockroachdb/errors` -> `github.com/kr/pretty` -> `github.com/kr/text`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-common` -> `github.com/cockroachdb/errors`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/cockroachdb/errors`** is imported at:
    - `protoutil/txutils.go:15`
    - `protoutil/unmarshalers.go:10`
    - ... and 25 more location(s)

---

## 🎯 BLAME POINT: `github.com/getsentry/sentry-go`
Responsible for 3 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-playground/locales**
    - Path: `github.com/getsentry/sentry-go` -> `github.com/go-playground/locales`

  - **📦 github.com/go-playground/universal-translator**
    - Path: `github.com/getsentry/sentry-go` -> `github.com/go-playground/universal-translator`

  - **📦 github.com/josharian/intern**
    - Path: `github.com/getsentry/sentry-go` -> `github.com/josharian/intern`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-common` -> `github.com/getsentry/sentry-go`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/cockroachdb/errors` -> `github.com/getsentry/sentry-go`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/cockroachdb/errors`** is imported at:
    - `protoutil/txutils.go:15`
    - `protoutil/unmarshalers.go:10`
    - ... and 25 more location(s)
  - `github.com/getsentry/sentry-go` (not found in code)

---

## 🎯 BLAME POINT: `github.com/googleapis/api-linter/v2`
Responsible for 1 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-logr/stdr**
    - Path: `github.com/googleapis/api-linter/v2` -> `cloud.google.com/go/longrunning` -> `github.com/go-logr/stdr`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-common` -> `github.com/googleapis/api-linter/v2`

- **📝 Direct dependencies imported in your code:**

  - `github.com/googleapis/api-linter/v2` (not found in code)

---

## 🎯 BLAME POINT: `github.com/grpc-ecosystem/go-grpc-middleware`
Responsible for 1 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/gogo/protobuf**
    - Path: `github.com/grpc-ecosystem/go-grpc-middleware` -> `github.com/gogo/protobuf`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-common` -> `github.com/grpc-ecosystem/go-grpc-middleware`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/grpc-ecosystem/go-grpc-middleware`** is imported at:
    - `tools/pkg/comm/server.go:18`

---

## 🎯 BLAME POINT: `github.com/hyperledger/fabric-lib-go`
Responsible for 3 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/beorn7/perks**
    - Path: `github.com/hyperledger/fabric-lib-go` -> `github.com/beorn7/perks`

  - **📦 github.com/munnerz/goautoneg**
    - Path: `github.com/hyperledger/fabric-lib-go` -> `github.com/munnerz/goautoneg`

  - **📦 github.com/sykesm/zap-logfmt**
    - Path: `github.com/hyperledger/fabric-lib-go` -> `github.com/sykesm/zap-logfmt`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-common` -> `github.com/hyperledger/fabric-lib-go`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/hyperledger/fabric-lib-go`** is imported at:
    - `protoutil/proputils_test.go:17`
    - `msp/factory_test.go:14`
    - ... and 143 more location(s)

---

## 🎯 BLAME POINT: `github.com/stretchr/testify`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/stretchr/testify` -> `github.com/davecgh/go-spew`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/stretchr/testify` -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-common` -> `github.com/stretchr/testify`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/grpc-ecosystem/go-grpc-middleware` -> `github.com/stretchr/testify`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/alecthomas/kingpin/v2` -> `github.com/stretchr/testify`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/spf13/viper` -> `github.com/stretchr/testify`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/IBM/idemix` -> `github.com/stretchr/testify`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/hyperledger-labs/SmartBFT` -> `github.com/stretchr/testify`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/hyperledger/fabric-lib-go` -> `github.com/stretchr/testify`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/cockroachdb/errors` -> `github.com/stretchr/testify`
  - `github.com/hyperledger/fabric-x-common` -> `go.uber.org/zap` -> `github.com/stretchr/testify`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/spf13/viper` -> `github.com/subosito/gotenv` -> `github.com/stretchr/testify`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/IBM/idemix`** is imported at:
    - `protoutil/blockutils.go:19`
    - `msp/factory.go:10`
    - ... and 3 more location(s)
  - **✓ `github.com/alecthomas/kingpin/v2`** is imported at:
    - `cmd/common/cli.go:15`
    - `cmd/configtxlator/main.go:18`
    - ... and 1 more location(s)
  - **✓ `github.com/cockroachdb/errors`** is imported at:
    - `protoutil/txutils.go:15`
    - `protoutil/unmarshalers.go:10`
    - ... and 25 more location(s)
  - **✓ `github.com/grpc-ecosystem/go-grpc-middleware`** is imported at:
    - `tools/pkg/comm/server.go:18`
  - **✓ `github.com/hyperledger-labs/SmartBFT`** is imported at:
    - `tools/configtxgen/config.go:16`
  - **✓ `github.com/hyperledger/fabric-lib-go`** is imported at:
    - `protoutil/proputils_test.go:17`
    - `msp/factory_test.go:14`
    - ... and 143 more location(s)
  - **✓ `github.com/spf13/viper`** is imported at:
    - `core/config/config.go:15`
    - `core/config/config_test.go:14`
    - ... and 2 more location(s)
  - **✓ `github.com/stretchr/testify`** is imported at:
    - `protoutil/blockutils_test.go:17`
    - `protoutil/commonutils_test.go:16`
    - ... and 160 more location(s)
  - **✓ `go.uber.org/zap`** is imported at:
    - `msp/identities.go:23`
    - `common/policies/policy.go:17`
    - ... and 6 more location(s)

---

## 🎯 BLAME POINT: `github.com/vektra/mockery/v2`
Responsible for 1 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/mitchellh/mapstructure**
    - Path: `github.com/vektra/mockery/v2` -> `github.com/mitchellh/mapstructure`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-common` -> `github.com/vektra/mockery/v2`

- **📝 Direct dependencies imported in your code:**

  - `github.com/vektra/mockery/v2` (not found in code)

---

## 🎯 BLAME POINT: `google.golang.org/grpc`
Responsible for 1 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-logr/stdr**
    - Path: `google.golang.org/grpc` -> `github.com/go-logr/stdr`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-common` -> `google.golang.org/grpc`
  - `github.com/hyperledger/fabric-x-common` -> `google.golang.org/genproto/googleapis/api` -> `google.golang.org/grpc`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/grpc-ecosystem/go-grpc-middleware` -> `google.golang.org/grpc`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/cockroachdb/errors` -> `google.golang.org/grpc`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/hyperledger/fabric-protos-go-apiv2` -> `google.golang.org/grpc`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/googleapis/api-linter/v2` -> `google.golang.org/grpc`
  - `github.com/hyperledger/fabric-x-common` -> `google.golang.org/grpc/cmd/protoc-gen-go-grpc` -> `google.golang.org/grpc`
  - `github.com/hyperledger/fabric-x-common` -> `cloud.google.com/go/longrunning` -> `google.golang.org/grpc`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/IBM/idemix` -> `google.golang.org/grpc`
  - `github.com/hyperledger/fabric-x-common` -> `github.com/hyperledger/fabric-lib-go` -> `google.golang.org/grpc`

- **📝 Direct dependencies imported in your code:**

  - `cloud.google.com/go/longrunning` (not found in code)
  - **✓ `github.com/IBM/idemix`** is imported at:
    - `protoutil/blockutils.go:19`
    - `msp/factory.go:10`
    - ... and 3 more location(s)
  - **✓ `github.com/cockroachdb/errors`** is imported at:
    - `protoutil/txutils.go:15`
    - `protoutil/unmarshalers.go:10`
    - ... and 25 more location(s)
  - `github.com/googleapis/api-linter/v2` (not found in code)
  - **✓ `github.com/grpc-ecosystem/go-grpc-middleware`** is imported at:
    - `tools/pkg/comm/server.go:18`
  - **✓ `github.com/hyperledger/fabric-lib-go`** is imported at:
    - `protoutil/proputils_test.go:17`
    - `msp/factory_test.go:14`
    - ... and 143 more location(s)
  - **✓ `github.com/hyperledger/fabric-protos-go-apiv2`** is imported at:
    - `protolator/variably_opaque_test.go:13`
    - `protoutil/blockutils_test.go:16`
    - ... and 319 more location(s)
  - `google.golang.org/genproto/googleapis/api` (not found in code)
  - **✓ `google.golang.org/grpc`** is imported at:
    - `protoutil/txutils_test.go:26`
    - `protoutil/txutils_test.go:27`
    - ... and 71 more location(s)
  - `google.golang.org/grpc/cmd/protoc-gen-go-grpc` (not found in code)

---

## PACKAGES WITHOUT CLEAR BLAME POINT (1)

**📦 Package: `github.com/Knetic/govaluate`**
⚠️  Could not determine dependency chain

---

## NOT IN GO.MOD (20 found)

- `github.com/KyleBanks/depth`
- `github.com/benbjohnson/clock`
- `github.com/davidlazar/go-crypto`
- `github.com/dgryski/go-rendezvous`
- `github.com/ghodss/yaml`
- `github.com/hyperledger-aries/aries-bbs-go`
- `github.com/hyperledger-labs/jsonld-vc-bbs-go`
- `github.com/jackc/pgpassfile`
- `github.com/jackpal/go-nat-pmp`
- `github.com/marten-seemann/tcp`
- `github.com/mikioh/tcpinfo`
- `github.com/mikioh/tcpopt`
- `github.com/minio/sha256-simd`
- `github.com/pbnjay/memory`
- `github.com/remyoudompheng/bigfft`
- `github.com/spaolacci/murmur3`
- `github.com/whyrusleeping/go-keyspace`
- `github.com/xeipuuv/gojsonpointer`
- `github.com/xeipuuv/gojsonreference`
- `github.com/xeipuuv/gojsonschema`

---

## SUMMARY

**Total unmaintained imports analyzed:** 35
- In go.mod: 15 (2 direct, 13 indirect)
- Direct unmaintained imports (this repo to blame): 1
- Indirect unmaintained imports grouped by 11 external blame point(s)
- Not in go.mod: 20

