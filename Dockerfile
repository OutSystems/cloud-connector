# Dockerfile
FROM gcr.io/distroless/static:nonroot
COPY outsystemscc /app/
ENTRYPOINT ["/app/outsystemscc"]