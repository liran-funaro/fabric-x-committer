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
blockgen --help

blockgen -p testdata/profile1.yaml -o out/blocks
```

## Workload Submitter (client)

Reads pre-generated blocks from file and submits them to the coordinator as fast as possible.

TODO

### Run and Build

```bash
just build-client

./workload
```
