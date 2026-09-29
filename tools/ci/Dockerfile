# The machine GitHub Actions runs make ci on (.github/workflows/ci.yml):
# Linux, Go 1.26, Node 22. make ci-linux runs it here, on linux/amd64, so
# what differs by OS or architecture shows before a push. Node comes from its
# official image rather than a package mirror.
FROM golang:1.26-bookworm
COPY --from=node:22-bookworm /usr/local/bin/node /usr/local/bin/node
COPY --from=node:22-bookworm /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx
WORKDIR /src
