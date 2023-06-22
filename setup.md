# Prerequisites

### Golang 1.20
#### Mac:
```shell
brew install go@1.20
```

#### Ubuntu:
```shell
apt-get update
wget https://go.dev/dl/go1.20.3.linux-amd64.tar.gz
tar xvf ./go1.20.3.linux-amd64.tar.gz
rm ./go1.20.3.linux-amd64.tar.gz
mv ./go/ /usr/local/
```


Add the following lines in `~/.profile`:
```shell
export GOPATH=$HOME/go
export PATH=$PATH:/usr/local/go/bin:$GOPATH/bin
```

Test the version:
```shell
go version
```

### Docker client
#### Mac:
```shell
brew install docker
```

#### Ubuntu:
Follow instructions [here](https://docs.docker.com/engine/install/ubuntu/)

To grant access to a non-root user:
```shell
apt install acl
setfacl --modify user:cbdcdemo:rw /var/run/docker.sock
```


# Quickstart

## Build
You can build locally or via a docker.

Local build:
```shell
make build
```

Docker build:
```shell
make build-docker
```

Build linux binaries for remote machines:
```shell
make build os=linux arch=amd64 output_dir=./bin-linux
```

# Run local binaries
```shell
bin/sigservice --configs config/config-sigservice.yaml
bin/shardsservice --configs config/config-shardsservice.yaml
bin/coordinator --configs config/config-coordinator.yaml
bin/blockgen stream --configs config/config-blockgen.yaml
```


