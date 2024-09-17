# Dockerfile
ARG BASE_REG=edencore.azurecr.io/

# FIPS and non-FIPS build
FROM ${BASE_REG}cg_fips/go:1.22 as build
WORKDIR /app
COPY . .
RUN go mod download
RUN go build -o outsystemscc
# TODO: Import the FIPS module when it is required only
# - main.go: import _ "crypto/tls/fipsonly"
RUN go build -tags=requirefips -o outsystemscc-fips

# Package the final image
FROM ${BASE_REG}cg_fips/chainguard_base-fips:latest
COPY --from=build /app/outsystemscc /app/
COPY --from=build /app/outsystemscc-fips /app/
ENTRYPOINT ["/app/outsystemscc-fips"]