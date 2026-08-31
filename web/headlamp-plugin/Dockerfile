# Bakes this plugin's built dist/ into an image used as a Headlamp
# initContainer (hack/dev-headlamp-values.yaml), so it survives a Headlamp
# pod restart - unlike hack/build-headlamp-plugin.sh's kubectl cp into the
# chart's pluginsDir emptyDir, which is wiped every time that pod restarts
# and has to be re-run by hand. build-headlamp-plugin.sh stays for fast
# dev-loop iteration (no image build/reload/helm-upgrade round trip); this
# image is what hack/deploy-headlamp-plugin.sh builds for the "this should
# actually survive" path.
FROM node:22-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

# Not a runnable server - just a filesystem the initContainer's own `cp`
# command copies out of, into the shared plugins volume. Headlamp's own
# compatibility check (frontend/src/plugin/index.js) reads
# devDependencies['@kinvolk/headlamp-plugin'] from a package.json it expects
# to find *next to* main.js (verified live earlier this session - a plugin
# with only main.js and no package.json is reported "not compatible"), so
# package.json is copied alongside dist/ here the same way
# build-headlamp-plugin.sh already does by hand.
FROM alpine:3.20
COPY --from=builder /app/dist /plugin/authentik-bench
COPY --from=builder /app/package.json /plugin/authentik-bench/package.json
CMD ["sh", "-c", "cp -r /plugin/. /headlamp/plugins/"]
