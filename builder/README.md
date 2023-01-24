# Docker Registry 

## Artifactory

To simplify our SC prototype development, we use artifactory as our IBM internal docker registry.
The repo is available at https://eu.artifactory.swg-devops.com/artifactory/res-decentralized-trust-team-sc-docker-local/ and requires permission to use it.

### Setting up your account

First, we need to add you to the permission group.
Please contact @marcus via IBM Slack or [bur@zurich.ibm.com](bur@zurich.ibm.com).

Next, use your w3id to login at https://eu.artifactory.swg-devops.com/artifactory/res-decentralized-trust-team-sc-docker-local/.
On the top-right, click on your w3id and open ["Edit profile"](https://eu.artifactory.swg-devops.com/ui/admin/artifactory/user_profile).
Click on "Generate an Identity Token", give it a name and keep the token string somewhere at a save place. :P
You will need the token string later, to login with docker.


### Docker Login

```bash
docker login docker-eu.artifactory.swg-devops.com
// please use your w3id as user and the token as pasword
```

### Work with the Docker Registry

In this example, we illustrate how to pull and push the `sc_runner` docker image from our artifactory.

#### Pull
```bash
docker pull docker-eu.artifactory.swg-devops.com/res-decentralized-trust-team-sc-docker-local/sc_runner:latest
docker tag docker-eu.artifactory.swg-devops.com/res-decentralized-trust-team-sc-docker-local/sc_runner:latest sc_runner:latest
```

#### Push
```bash
docker tag sc_runner:latest docker-eu.artifactory.swg-devops.com/res-decentralized-trust-team-sc-docker-local/sc_runner:latest
docker push docker-eu.artifactory.swg-devops.com/res-decentralized-trust-team-sc-docker-local/sc_runner:latest
```