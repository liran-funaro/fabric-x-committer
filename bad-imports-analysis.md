# Unmaintained Imports Report

---

## ⚠️ DIRECT UNMAINTAINED IMPORTS (1 found)

**🔴 THIS REPOSITORY IS TO BLAME**

These packages are directly imported in your code and should be removed.

- **📦 github.com/mitchellh/mapstructure**

  **Imported at:**

  - `loadgen/workload/distributions_test.go:16`
  - `cmd/config/config_decoder.go:19`

---

## 🎯 BLAME POINT: `cloud.google.com/go/longrunning`
Responsible for 3 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-logr/stdr**
    - Path: `cloud.google.com/go/longrunning` *(indirect)* -> `github.com/go-logr/stdr`

  - **📦 github.com/kr/pretty**
    - Path: `cloud.google.com/go/longrunning` *(indirect)* -> `go.opentelemetry.io/auto/sdk` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `cloud.google.com/go/longrunning` *(indirect)* -> `go.opentelemetry.io/otel` *(indirect)* -> `github.com/kr/text`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `cloud.google.com/go/longrunning`

- **📝 Direct dependencies imported in your code:**

  - `cloud.google.com/go/longrunning` (not found in code)

---

## 🎯 BLAME POINT: `github.com/IBM/idemix`
Responsible for 7 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/IBM/idemix` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/go-logr/stdr**
    - Path: `github.com/IBM/idemix` -> `google.golang.org/grpc` *(indirect)* -> `github.com/go-logr/stdr`

  - **📦 github.com/kilic/bls12-381**
    - Path: `github.com/IBM/idemix` *(indirect)* -> `github.com/kilic/bls12-381`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/IBM/idemix` *(indirect)* -> `github.com/consensys/gnark-crypto` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/IBM/idemix` -> `github.com/IBM/mathlib` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/IBM/idemix` *(indirect)* -> `github.com/pmezard/go-difflib`

  - **📦 github.com/sykesm/zap-logfmt**
    - Path: `github.com/IBM/idemix` -> `github.com/sykesm/zap-logfmt`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/IBM/idemix`

- **📝 Direct dependencies imported in your code:**

  - `github.com/IBM/idemix` (not found in code)

---

## 🎯 BLAME POINT: `github.com/IBM/idemix/bccsp/schemes/aries`
Responsible for 5 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/IBM/idemix/bccsp/schemes/aries` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/kilic/bls12-381**
    - Path: `github.com/IBM/idemix/bccsp/schemes/aries` *(indirect)* -> `github.com/kilic/bls12-381`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/IBM/idemix/bccsp/schemes/aries` *(indirect)* -> `github.com/consensys/gnark-crypto` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/IBM/idemix/bccsp/schemes/aries` -> `github.com/IBM/mathlib` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/IBM/idemix/bccsp/schemes/aries` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/IBM/idemix/bccsp/schemes/aries`

- **📝 Direct dependencies imported in your code:**

  - `github.com/IBM/idemix/bccsp/schemes/aries` (not found in code)

---

## 🎯 BLAME POINT: `github.com/IBM/idemix/bccsp/schemes/weak-bb`
Responsible for 5 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/IBM/idemix/bccsp/schemes/weak-bb` -> `github.com/IBM/mathlib` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/kilic/bls12-381**
    - Path: `github.com/IBM/idemix/bccsp/schemes/weak-bb` *(indirect)* -> `github.com/kilic/bls12-381`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/IBM/idemix/bccsp/schemes/weak-bb` *(indirect)* -> `github.com/consensys/gnark-crypto` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/IBM/idemix/bccsp/schemes/weak-bb` -> `github.com/IBM/mathlib` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/IBM/idemix/bccsp/schemes/weak-bb` -> `github.com/IBM/mathlib` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/IBM/idemix/bccsp/schemes/weak-bb`

- **📝 Direct dependencies imported in your code:**

  - `github.com/IBM/idemix/bccsp/schemes/weak-bb` (not found in code)

---

## 🎯 BLAME POINT: `github.com/IBM/idemix/bccsp/types`
Responsible for 5 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/IBM/idemix/bccsp/types` -> `github.com/IBM/mathlib` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/kilic/bls12-381**
    - Path: `github.com/IBM/idemix/bccsp/types` *(indirect)* -> `github.com/kilic/bls12-381`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/IBM/idemix/bccsp/types` *(indirect)* -> `github.com/consensys/gnark-crypto` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/IBM/idemix/bccsp/types` -> `github.com/IBM/mathlib` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/IBM/idemix/bccsp/types` -> `github.com/IBM/mathlib` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/IBM/idemix/bccsp/types`

- **📝 Direct dependencies imported in your code:**

  - `github.com/IBM/idemix/bccsp/types` (not found in code)

---

## 🎯 BLAME POINT: `github.com/IBM/mathlib`
Responsible for 5 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/IBM/mathlib` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/kilic/bls12-381**
    - Path: `github.com/IBM/mathlib` -> `github.com/kilic/bls12-381`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/IBM/mathlib` -> `github.com/consensys/gnark-crypto` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/IBM/mathlib` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/IBM/mathlib` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/IBM/mathlib`

- **📝 Direct dependencies imported in your code:**

  - `github.com/IBM/mathlib` (not found in code)

---

## 🎯 BLAME POINT: `github.com/Kunde21/markdownfmt/v3`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/Kunde21/markdownfmt/v3` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/Kunde21/markdownfmt/v3` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/Kunde21/markdownfmt/v3`

- **📝 Direct dependencies imported in your code:**

  - `github.com/Kunde21/markdownfmt/v3` (not found in code)

---

## 🎯 BLAME POINT: `github.com/bufbuild/protocompile`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/bufbuild/protocompile` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/bufbuild/protocompile` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/bufbuild/protocompile`

- **📝 Direct dependencies imported in your code:**

  - `github.com/bufbuild/protocompile` (not found in code)

---

## 🎯 BLAME POINT: `github.com/cockroachdb/errors`
Responsible for 6 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/cockroachdb/errors` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/go-logr/stdr**
    - Path: `github.com/cockroachdb/errors` -> `google.golang.org/grpc` *(indirect)* -> `github.com/go-logr/stdr`

  - **📦 github.com/gogo/protobuf**
    - Path: `github.com/cockroachdb/errors` -> `github.com/gogo/protobuf`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/cockroachdb/errors` -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/cockroachdb/errors` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/cockroachdb/errors` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/cockroachdb/errors`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/cockroachdb/errors`** is imported at:
    - `loadgen/client.go:13`
    - `utils/utils.go:20`
    - ... and 85 more location(s)

---

## 🎯 BLAME POINT: `github.com/consensys/gnark-crypto`
Responsible for 4 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/consensys/gnark-crypto` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/consensys/gnark-crypto` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/consensys/gnark-crypto` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/consensys/gnark-crypto` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/consensys/gnark-crypto`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/consensys/gnark-crypto`** is imported at:
    - `utils/signature/verify_bls.go:11`
    - `utils/signature/verify_schemes_test.go:20`
    - ... and 4 more location(s)

---

## 🎯 BLAME POINT: `github.com/containerd/errdefs/pkg`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-logr/stdr**
    - Path: `github.com/containerd/errdefs/pkg` -> `google.golang.org/grpc` *(indirect)* -> `github.com/go-logr/stdr`

  - **📦 github.com/gogo/protobuf**
    - Path: `github.com/containerd/errdefs/pkg` *(indirect)* -> `github.com/gogo/protobuf`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/containerd/errdefs/pkg`

- **📝 Direct dependencies imported in your code:**

  - `github.com/containerd/errdefs/pkg` (not found in code)

---

## 🎯 BLAME POINT: `github.com/fsouza/go-dockerclient`
Responsible for 1 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-logr/stdr**
    - Path: `github.com/fsouza/go-dockerclient` -> `github.com/moby/moby/client` *(indirect)* -> `github.com/go-logr/stdr`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/fsouza/go-dockerclient`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/fsouza/go-dockerclient`** is imported at:
    - `integration/runner/cluster_controllers_test.go:14`
    - `utils/testdb/container.go:22`
    - ... and 2 more location(s)

---

## 🎯 BLAME POINT: `github.com/gavv/httpexpect/v2`
Responsible for 6 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/gavv/httpexpect/v2` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/gavv/httpexpect/v2` *(indirect)* -> `github.com/sergi/go-diff` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/gavv/httpexpect/v2` *(indirect)* -> `github.com/pmezard/go-difflib`

  - **📦 github.com/xeipuuv/gojsonpointer**
    - Path: `github.com/gavv/httpexpect/v2` *(indirect)* -> `github.com/xeipuuv/gojsonpointer`

  - **📦 github.com/xeipuuv/gojsonreference**
    - Path: `github.com/gavv/httpexpect/v2` *(indirect)* -> `github.com/xeipuuv/gojsonreference`

  - **📦 github.com/xeipuuv/gojsonschema**
    - Path: `github.com/gavv/httpexpect/v2` -> `github.com/xeipuuv/gojsonschema`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/gavv/httpexpect/v2`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/gavv/httpexpect/v2`** is imported at:
    - `loadgen/client_test.go:19`

---

## 🎯 BLAME POINT: `github.com/getsentry/sentry-go`
Responsible for 4 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/getsentry/sentry-go` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/getsentry/sentry-go` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/getsentry/sentry-go` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/getsentry/sentry-go` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/getsentry/sentry-go`

- **📝 Direct dependencies imported in your code:**

  - `github.com/getsentry/sentry-go` (not found in code)

---

## 🎯 BLAME POINT: `github.com/go-playground/validator/v10`
Responsible for 3 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-playground/locales**
    - Path: `github.com/go-playground/validator/v10` -> `github.com/go-playground/locales`

  - **📦 github.com/go-playground/universal-translator**
    - Path: `github.com/go-playground/validator/v10` -> `github.com/go-playground/universal-translator`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/go-playground/validator/v10` -> `github.com/leodido/go-urn` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/go-playground/validator/v10`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/go-playground/validator/v10`** is imported at:
    - `cmd/config/app_config.go:18`

---

## 🎯 BLAME POINT: `github.com/go-task/slim-sprig/v3`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/go-task/slim-sprig/v3` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/go-task/slim-sprig/v3` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/go-task/slim-sprig/v3`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/go-task/slim-sprig/v3`** is imported at:
    - `cmd/config/create_config_file.go:20`

---

## 🎯 BLAME POINT: `github.com/googleapis/api-linter/v2`
Responsible for 3 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/googleapis/api-linter/v2` -> `github.com/bufbuild/protocompile` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/go-logr/stdr**
    - Path: `github.com/googleapis/api-linter/v2` -> `cloud.google.com/go/longrunning` *(indirect)* -> `github.com/go-logr/stdr`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/googleapis/api-linter/v2` -> `github.com/bufbuild/protocompile` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/googleapis/api-linter/v2`

- **📝 Direct dependencies imported in your code:**

  - `github.com/googleapis/api-linter/v2` (not found in code)

---

## 🎯 BLAME POINT: `github.com/grpc-ecosystem/grpc-gateway/v2`
Responsible for 3 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-logr/stdr**
    - Path: `github.com/grpc-ecosystem/grpc-gateway/v2` -> `google.golang.org/grpc` *(indirect)* -> `github.com/go-logr/stdr`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/grpc-ecosystem/grpc-gateway/v2` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/grpc-ecosystem/grpc-gateway/v2` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/grpc-ecosystem/grpc-gateway/v2`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/grpc-ecosystem/grpc-gateway/v2`** is imported at:
    - `loadgen/client.go:14`
    - `api/servicepb/loadgen.pb.gw.go:17`
    - ... and 1 more location(s)

---

## 🎯 BLAME POINT: `github.com/hyperledger-labs/SmartBFT`
Responsible for 3 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/hyperledger-labs/SmartBFT` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/kr/text**
    - Path: `github.com/hyperledger-labs/SmartBFT` -> `go.uber.org/zap` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/hyperledger-labs/SmartBFT` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/hyperledger-labs/SmartBFT`

- **📝 Direct dependencies imported in your code:**

  - `github.com/hyperledger-labs/SmartBFT` (not found in code)

---

## 🎯 BLAME POINT: `github.com/hyperledger/aries-bbs-go`
Responsible for 5 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/hyperledger/aries-bbs-go` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/kilic/bls12-381**
    - Path: `github.com/hyperledger/aries-bbs-go` *(indirect)* -> `github.com/kilic/bls12-381`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/hyperledger/aries-bbs-go` *(indirect)* -> `github.com/consensys/gnark-crypto` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/hyperledger/aries-bbs-go` -> `github.com/IBM/mathlib` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/hyperledger/aries-bbs-go` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/hyperledger/aries-bbs-go`

- **📝 Direct dependencies imported in your code:**

  - `github.com/hyperledger/aries-bbs-go` (not found in code)

---

## 🎯 BLAME POINT: `github.com/hyperledger/fabric-lib-go`
Responsible for 9 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/beorn7/perks**
    - Path: `github.com/hyperledger/fabric-lib-go` *(indirect)* -> `github.com/beorn7/perks`

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/hyperledger/fabric-lib-go` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/go-logr/stdr**
    - Path: `github.com/hyperledger/fabric-lib-go` -> `google.golang.org/grpc` *(indirect)* -> `github.com/go-logr/stdr`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/hyperledger/fabric-lib-go` -> `github.com/prometheus/client_golang` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/hyperledger/fabric-lib-go` *(indirect)* -> `github.com/spf13/cast` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/mitchellh/mapstructure**
    - Path: `github.com/hyperledger/fabric-lib-go` -> `github.com/mitchellh/mapstructure`

  - **📦 github.com/munnerz/goautoneg**
    - Path: `github.com/hyperledger/fabric-lib-go` *(indirect)* -> `github.com/munnerz/goautoneg`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/hyperledger/fabric-lib-go` *(indirect)* -> `github.com/pmezard/go-difflib`

  - **📦 github.com/sykesm/zap-logfmt**
    - Path: `github.com/hyperledger/fabric-lib-go` -> `github.com/sykesm/zap-logfmt`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/hyperledger/fabric-lib-go`
  - `github.com/hyperledger/fabric-x-committer` -> `github.com/hyperledger/fabric-x-common` -> `github.com/hyperledger/fabric-lib-go`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/hyperledger/fabric-lib-go`** is imported at:
    - `loadgen/client.go:15`
    - `mock/common.go:9`
    - ... and 41 more location(s)
  - **✓ `github.com/hyperledger/fabric-x-common`** is imported at:
    - `loadgen/client_test.go:20`
    - `loadgen/client_test.go:21`
    - ... and 266 more location(s)

---

## 🎯 BLAME POINT: `github.com/hyperledger/fabric-protos-go-apiv2`
Responsible for 1 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-logr/stdr**
    - Path: `github.com/hyperledger/fabric-protos-go-apiv2` -> `google.golang.org/grpc` *(indirect)* -> `github.com/go-logr/stdr`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/hyperledger/fabric-protos-go-apiv2`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/hyperledger/fabric-protos-go-apiv2`** is imported at:
    - `mock/orderer.go:21`
    - `mock/orderer.go:22`
    - ... and 48 more location(s)

---

## 🎯 BLAME POINT: `github.com/hyperledger/fabric-x-common`
Responsible for 10 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/Knetic/govaluate**
    - Path: `github.com/hyperledger/fabric-x-common` -> `github.com/Knetic/govaluate`

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/hyperledger/fabric-x-common` -> `github.com/davecgh/go-spew`

  - **📦 github.com/go-logr/stdr**
    - Path: `github.com/hyperledger/fabric-x-common` *(indirect)* -> `cloud.google.com/go/longrunning` *(indirect)* -> `github.com/go-logr/stdr`

  - **📦 github.com/gogo/protobuf**
    - Path: `github.com/hyperledger/fabric-x-common` *(indirect)* -> `github.com/gogo/protobuf`

  - **📦 github.com/kilic/bls12-381**
    - Path: `github.com/hyperledger/fabric-x-common` *(indirect)* -> `github.com/kilic/bls12-381`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/hyperledger/fabric-x-common` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/hyperledger/fabric-x-common` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/mitchellh/mapstructure**
    - Path: `github.com/hyperledger/fabric-x-common` -> `github.com/mitchellh/mapstructure`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/hyperledger/fabric-x-common` *(indirect)* -> `github.com/pmezard/go-difflib`

  - **📦 github.com/sykesm/zap-logfmt**
    - Path: `github.com/hyperledger/fabric-x-common` *(indirect)* -> `github.com/sykesm/zap-logfmt`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/hyperledger/fabric-x-common`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/hyperledger/fabric-x-common`** is imported at:
    - `loadgen/client_test.go:20`
    - `loadgen/client_test.go:21`
    - ... and 266 more location(s)

---

## 🎯 BLAME POINT: `github.com/jackc/pgx/v5`
Responsible for 5 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/jackc/pgx/v5` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/jackc/pgpassfile**
    - Path: `github.com/jackc/pgx/v5` -> `github.com/jackc/pgpassfile`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/jackc/pgx/v5` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/jackc/pgx/v5` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/jackc/pgx/v5` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/jackc/pgx/v5`

- **📝 Direct dependencies imported in your code:**

  - `github.com/jackc/pgx/v5` (not found in code)

---

## 🎯 BLAME POINT: `github.com/jackc/puddle/v2`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/jackc/puddle/v2` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/jackc/puddle/v2` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/jackc/puddle/v2`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/jackc/puddle/v2`** is imported at:
    - `utils/retry/executor.go:14`

---

## 🎯 BLAME POINT: `github.com/leodido/go-urn`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/leodido/go-urn` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/leodido/go-urn` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/leodido/go-urn`

- **📝 Direct dependencies imported in your code:**

  - `github.com/leodido/go-urn` (not found in code)

---

## 🎯 BLAME POINT: `github.com/moby/moby/client`
Responsible for 4 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-logr/stdr**
    - Path: `github.com/moby/moby/client` *(indirect)* -> `github.com/go-logr/stdr`

  - **📦 github.com/gogo/protobuf**
    - Path: `github.com/moby/moby/client` -> `github.com/containerd/errdefs/pkg` *(indirect)* -> `github.com/gogo/protobuf`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/moby/moby/client` *(indirect)* -> `go.opentelemetry.io/auto/sdk` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/moby/moby/client` *(indirect)* -> `go.opentelemetry.io/otel` *(indirect)* -> `github.com/kr/text`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/moby/moby/client`

- **📝 Direct dependencies imported in your code:**

  - `github.com/moby/moby/client` (not found in code)

---

## 🎯 BLAME POINT: `github.com/pkg/diff`
Responsible for 1 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/kr/pretty**
    - Path: `github.com/pkg/diff` *(indirect)* -> `github.com/sergi/go-diff` *(indirect)* -> `github.com/kr/pretty`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/pkg/diff`

- **📝 Direct dependencies imported in your code:**

  - `github.com/pkg/diff` (not found in code)

---

## 🎯 BLAME POINT: `github.com/prometheus/client_golang`
Responsible for 4 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/beorn7/perks**
    - Path: `github.com/prometheus/client_golang` -> `github.com/beorn7/perks`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/prometheus/client_golang` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/prometheus/client_golang` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`

  - **📦 github.com/munnerz/goautoneg**
    - Path: `github.com/prometheus/client_golang` *(indirect)* -> `github.com/munnerz/goautoneg`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/prometheus/client_golang`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/prometheus/client_golang`** is imported at:
    - `loadgen/metrics/metrics.go:13`
    - `utils/monitoring/provider_test.go:17`
    - ... and 14 more location(s)

---

## 🎯 BLAME POINT: `github.com/prometheus/common`
Responsible for 5 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/beorn7/perks**
    - Path: `github.com/prometheus/common` *(indirect)* -> `github.com/beorn7/perks`

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/prometheus/common` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/prometheus/common` *(indirect)* -> `github.com/prometheus/client_golang` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/munnerz/goautoneg**
    - Path: `github.com/prometheus/common` -> `github.com/munnerz/goautoneg`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/prometheus/common` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/prometheus/common`

- **📝 Direct dependencies imported in your code:**

  - `github.com/prometheus/common` (not found in code)

---

## 🎯 BLAME POINT: `github.com/rogpeppe/go-internal`
Responsible for 1 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/kr/pretty**
    - Path: `github.com/rogpeppe/go-internal` *(indirect)* -> `gopkg.in/errgo.v2` *(indirect)* -> `github.com/kr/pretty`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/rogpeppe/go-internal`

- **📝 Direct dependencies imported in your code:**

  - `github.com/rogpeppe/go-internal` (not found in code)

---

## 🎯 BLAME POINT: `github.com/sanity-io/litter`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/sanity-io/litter` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/sanity-io/litter` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/sanity-io/litter`

- **📝 Direct dependencies imported in your code:**

  - `github.com/sanity-io/litter` (not found in code)

---

## 🎯 BLAME POINT: `github.com/sergi/go-diff`
Responsible for 3 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/sergi/go-diff` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/sergi/go-diff` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/sergi/go-diff` *(indirect)* -> `github.com/kr/pretty` -> `github.com/kr/text`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/sergi/go-diff`

- **📝 Direct dependencies imported in your code:**

  - `github.com/sergi/go-diff` (not found in code)

---

## 🎯 BLAME POINT: `github.com/sirupsen/logrus`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/sirupsen/logrus` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/sirupsen/logrus` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/sirupsen/logrus`

- **📝 Direct dependencies imported in your code:**

  - `github.com/sirupsen/logrus` (not found in code)

---

## 🎯 BLAME POINT: `github.com/spf13/cast`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/kr/pretty**
    - Path: `github.com/spf13/cast` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/spf13/cast` *(indirect)* -> `github.com/kr/text`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/spf13/cast`

- **📝 Direct dependencies imported in your code:**

  - `github.com/spf13/cast` (not found in code)

---

## 🎯 BLAME POINT: `github.com/spf13/viper`
Responsible for 4 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/spf13/viper` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/spf13/viper` -> `github.com/spf13/cast` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `github.com/spf13/viper` -> `github.com/spf13/cast` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/spf13/viper` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/spf13/viper`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/spf13/viper`** is imported at:
    - `cmd/config/env_vars.go:15`
    - `cmd/config/config_decoder.go:20`
    - ... and 3 more location(s)

---

## 🎯 BLAME POINT: `github.com/stretchr/testify`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/stretchr/testify` -> `github.com/davecgh/go-spew`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/stretchr/testify` -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/stretchr/testify`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/stretchr/testify`** is imported at:
    - `loadgen/client_test.go:22`
    - `loadgen/client_test.go:23`
    - ... and 152 more location(s)

---

## 🎯 BLAME POINT: `github.com/subosito/gotenv`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/subosito/gotenv` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/subosito/gotenv` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `github.com/subosito/gotenv`

- **📝 Direct dependencies imported in your code:**

  - `github.com/subosito/gotenv` (not found in code)

---

## 🎯 BLAME POINT: `github.com/yugabyte/pgx/v5`
Responsible for 4 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `github.com/yugabyte/pgx/v5` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/jackc/pgpassfile**
    - Path: `github.com/yugabyte/pgx/v5` -> `github.com/jackc/pgpassfile`

  - **📦 github.com/kr/pretty**
    - Path: `github.com/yugabyte/pgx/v5` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `github.com/yugabyte/pgx/v5` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `github.com/yugabyte/pgx/v5`

- **📝 Direct dependencies imported in your code:**

  - **✓ `github.com/yugabyte/pgx/v5`** is imported at:
    - `utils/dbconn/database_connection_test.go:16`
    - `utils/dbconn/database_connection.go:16`
    - ... and 11 more location(s)

---

## 🎯 BLAME POINT: `go.opentelemetry.io/auto/sdk`
Responsible for 5 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `go.opentelemetry.io/auto/sdk` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/go-logr/stdr**
    - Path: `go.opentelemetry.io/auto/sdk` -> `go.opentelemetry.io/otel` -> `github.com/go-logr/stdr`

  - **📦 github.com/kr/pretty**
    - Path: `go.opentelemetry.io/auto/sdk` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `go.opentelemetry.io/auto/sdk` -> `go.opentelemetry.io/otel` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `go.opentelemetry.io/auto/sdk` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `go.opentelemetry.io/auto/sdk`

- **📝 Direct dependencies imported in your code:**

  - `go.opentelemetry.io/auto/sdk` (not found in code)

---

## 🎯 BLAME POINT: `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`
Responsible for 5 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/go-logr/stdr**
    - Path: `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` *(indirect)* -> `github.com/go-logr/stdr`

  - **📦 github.com/kr/pretty**
    - Path: `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` *(indirect)* -> `go.opentelemetry.io/auto/sdk` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` -> `go.opentelemetry.io/otel` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`

- **📝 Direct dependencies imported in your code:**

  - `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` (not found in code)

---

## 🎯 BLAME POINT: `go.opentelemetry.io/otel`
Responsible for 5 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `go.opentelemetry.io/otel` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/go-logr/stdr**
    - Path: `go.opentelemetry.io/otel` -> `github.com/go-logr/stdr`

  - **📦 github.com/kr/pretty**
    - Path: `go.opentelemetry.io/otel` -> `go.opentelemetry.io/auto/sdk` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `go.opentelemetry.io/otel` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `go.opentelemetry.io/otel` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `go.opentelemetry.io/otel`

- **📝 Direct dependencies imported in your code:**

  - `go.opentelemetry.io/otel` (not found in code)

---

## 🎯 BLAME POINT: `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`
Responsible for 5 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/go-logr/stdr**
    - Path: `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` *(indirect)* -> `github.com/go-logr/stdr`

  - **📦 github.com/kr/pretty**
    - Path: `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` *(indirect)* -> `github.com/grpc-ecosystem/grpc-gateway/v2` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` -> `go.opentelemetry.io/otel` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`

- **📝 Direct dependencies imported in your code:**

  - `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` (not found in code)

---

## 🎯 BLAME POINT: `go.opentelemetry.io/otel/metric`
Responsible for 5 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `go.opentelemetry.io/otel/metric` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/go-logr/stdr**
    - Path: `go.opentelemetry.io/otel/metric` *(indirect)* -> `github.com/go-logr/stdr`

  - **📦 github.com/kr/pretty**
    - Path: `go.opentelemetry.io/otel/metric` *(indirect)* -> `go.opentelemetry.io/auto/sdk` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `go.opentelemetry.io/otel/metric` -> `go.opentelemetry.io/otel` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `go.opentelemetry.io/otel/metric` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `go.opentelemetry.io/otel/metric`

- **📝 Direct dependencies imported in your code:**

  - `go.opentelemetry.io/otel/metric` (not found in code)

---

## 🎯 BLAME POINT: `go.opentelemetry.io/otel/trace`
Responsible for 4 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `go.opentelemetry.io/otel/trace` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/go-logr/stdr**
    - Path: `go.opentelemetry.io/otel/trace` -> `go.opentelemetry.io/otel` -> `github.com/go-logr/stdr`

  - **📦 github.com/kr/text**
    - Path: `go.opentelemetry.io/otel/trace` -> `go.opentelemetry.io/otel` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `go.opentelemetry.io/otel/trace` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `go.opentelemetry.io/otel/trace`

- **📝 Direct dependencies imported in your code:**

  - `go.opentelemetry.io/otel/trace` (not found in code)

---

## 🎯 BLAME POINT: `go.uber.org/mock`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `go.uber.org/mock` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `go.uber.org/mock` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `go.uber.org/mock`

- **📝 Direct dependencies imported in your code:**

  - **✓ `go.uber.org/mock`** is imported at:
    - `utils/deliver/delivery_test.go:19`
    - `utils/deliver/streamer_mock_test.go:18`

---

## 🎯 BLAME POINT: `go.uber.org/multierr`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `go.uber.org/multierr` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `go.uber.org/multierr` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(indirect)* -> `go.uber.org/multierr`

- **📝 Direct dependencies imported in your code:**

  - `go.uber.org/multierr` (not found in code)

---

## 🎯 BLAME POINT: `go.uber.org/zap`
Responsible for 3 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/davecgh/go-spew**
    - Path: `go.uber.org/zap` *(indirect)* -> `github.com/davecgh/go-spew`

  - **📦 github.com/kr/text**
    - Path: `go.uber.org/zap` *(indirect)* -> `github.com/kr/text`

  - **📦 github.com/pmezard/go-difflib**
    - Path: `go.uber.org/zap` *(indirect)* -> `github.com/pmezard/go-difflib`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `go.uber.org/zap`

- **📝 Direct dependencies imported in your code:**

  - **✓ `go.uber.org/zap`** is imported at:
    - `utils/retry/executor.go:16`
    - `service/vc/validator.go:15`
    - ... and 1 more location(s)

---

## 🎯 BLAME POINT: `google.golang.org/grpc`
Responsible for 3 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/go-logr/stdr**
    - Path: `google.golang.org/grpc` *(indirect)* -> `github.com/go-logr/stdr`

  - **📦 github.com/kr/pretty**
    - Path: `google.golang.org/grpc` *(indirect)* -> `go.opentelemetry.io/auto/sdk` *(indirect)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `google.golang.org/grpc` -> `go.opentelemetry.io/otel` *(indirect)* -> `github.com/kr/text`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` -> `google.golang.org/grpc`

- **📝 Direct dependencies imported in your code:**

  - **✓ `google.golang.org/grpc`** is imported at:
    - `loadgen/client.go:17`
    - `loadgen/client.go:18`
    - ... and 98 more location(s)

---

## 🎯 BLAME POINT: `mvdan.cc/gofumpt`
Responsible for 2 unmaintained import(s)

- **⚠️  Unmaintained imports:**

  - **📦 github.com/kr/pretty**
    - Path: `mvdan.cc/gofumpt` *(toolchain)* -> `github.com/kr/pretty`

  - **📦 github.com/kr/text**
    - Path: `mvdan.cc/gofumpt` *(toolchain)* -> `github.com/kr/text`

- **📍 Paths from your code to this blame point:**

  - `github.com/hyperledger/fabric-x-committer` *(toolchain)* -> `mvdan.cc/gofumpt`

- **📝 Direct dependencies imported in your code:**

  - `mvdan.cc/gofumpt` (not found in code)

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
- In go.mod: 18 (1 direct, 17 indirect)
- Direct unmaintained imports (this repo to blame): 1
- Indirect unmaintained imports grouped by 50 external blame point(s)
- Not in go.mod: 17

