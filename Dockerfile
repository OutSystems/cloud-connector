# Dockerfile
ARG BASE_REG=edencore.azurecr.io/
FROM ${BASE_REG}cg_fips/go:1.22 as build
WORKDIR /app
COPY . .
RUN go mod download
RUN go build -o outsystemscc
FROM ${BASE_REG}cg_fips/chainguard_base-fips:latest
COPY --from=build /app/outsystemscc /app/
ENTRYPOINT ["/app/outsystemscc"]