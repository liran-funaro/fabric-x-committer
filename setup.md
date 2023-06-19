# Prerequisites

### Just
#### Mac
```shell
brew install just
```

#### Ubuntu:
```shell
curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh &#124; bash -s -- --to /usr/local/just
```

Add the following line to `~/.profile`:
```shell
export PATH=$PATH:/usr/local/just
```

Test the version:
```shell
just —-version
```

[Further details](https://github.com/casey/just#installation)

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

Build and run local binaries
```shell
just build-local
bin/sigservice --configs config/config-sigservice.yaml
bin/shardsservice --configs config/config-shardsservice.yaml
bin/coordinator --configs config/config-coordinator.yaml
bin/blockgen stream --configs config/config-blockgen.yaml
```

Build linux binaries for remote machines
```shell
just build-linux
```
