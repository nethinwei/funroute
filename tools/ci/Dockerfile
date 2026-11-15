# The machine GitHub Actions runs make ci on (.github/workflows/ci.yml): Linux,
# with Go at go.mod's version and Node at web/.nvmrc's, which make ci-linux
# passes in. It runs make ci here, on linux/amd64, so what differs by OS or
# architecture shows before a push. Node comes from its official image rather
# than a package mirror. GO and NODE have no defaults on purpose: a default
# would be a third place the versions are written, and would go stale
# unnoticed; built without them, the build fails at once (docker warns of
# InvalidDefaultArgInFrom for the same reason).
ARG GO
ARG NODE
FROM node:${NODE}-bookworm AS node
FROM golang:${GO}-bookworm
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx
WORKDIR /src
