# Workload Generator Client

## Workload Generator (blockgen)

Allows generating blocks according to a given profile.
A sample profile is given in [testdata/profile1.yaml](testdata/profile1.yaml).
Persists the generated data on disk.
The outfile contains the used profile and the generated blocks as serialized protobufs.
In addition to the block file, the tool write the corresponding signature verification key into a `.pem` file.

### Build and Run

```bash
just build-blockgen

# usage see
./blockgen generate --help

./blockgen generate -p testdata/profile1.yaml -o out/blocks

./blockgen load --in out/blocks
./blockgen pump --in out/blocks --host localhost --port 500
```

## Workload Submitter (client)

Reads pre-generated blocks from file and submits them to the coordinator as fast as possible.

### Pump

```bash
# usage see
./blockgen pump --help

# reads blocks from disk and pumps to coordinator
./blockgen pump --in out/blocks --host localhost --port 5002

# just read blocks from disk
./blockgen load --in out/blocks
```
