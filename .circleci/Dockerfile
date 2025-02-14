FROM circleci/golang:1.17

RUN go get -u github.com/kisielk/errcheck

RUN go get -u github.com/google/wire/cmd/wire

RUN go get -u golang.org/x/tools/cmd/goimports

RUN sudo apt-get install default-jdk

USER root

RUN curl -sL https://deb.nodesource.com/setup_16.x | bash -

RUN apt-get install -y nodejs

RUN mv /usr/bin/docker-compose /usr/bin/docker-compose-v1
ARG DOCKER_COMPOSE_V2_VERSION=v2.2.2
RUN curl -fL "https://github.com/docker/compose/releases/download/${DOCKER_COMPOSE_V2_VERSION}/docker-compose-$(uname -s | tr '[A-Z]' '[a-z]')-$(uname -m)" -o /usr/local/bin/docker-compose \
  && chmod a+x /usr/local/bin/docker-compose \
  && docker-compose version

USER circleci

RUN sudo npm install -g firebase-tools

RUN firebase setup:emulators:firestore

USER root

RUN curl --silent --show-error --location --fail --retry 3 --output /tmp/kustomize.tar.gz  https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv3.9.2/kustomize_v3.9.2_linux_amd64.tar.gz \
  && tar -xz -C /tmp -f /tmp/kustomize.tar.gz \
  && mv /tmp/kustomize /usr/bin/kustomize

RUN curl --silent --show-error --location --fail --retry 3 --output /tmp/helm.tar.gz  https://get.helm.sh/helm-v3.5.0-linux-amd64.tar.gz \
  && tar -xz -C /tmp -f /tmp/helm.tar.gz \
  && mv /tmp/linux-amd64/helm /usr/bin/helm3

USER circleci

RUN curl --silent --show-error --location --fail --retry 3 --output /tmp/google-cloud-sdk.tar.gz https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/google-cloud-sdk-326.0.0-linux-x86_64.tar.gz \
  && tar -xz -C /tmp -f /tmp/google-cloud-sdk.tar.gz

RUN /tmp/google-cloud-sdk/install.sh

RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.43.0

RUN curl -o- -L https://yarnpkg.com/install.sh | bash -s -- --version 1.22.10

ENV PATH="${PATH}:/tmp/google-cloud-sdk/bin"
ENV PATH="${PATH}:/home/circleci/.yarn/bin"
